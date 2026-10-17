//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/convert"
	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
)

// waitForQueue waits until nothing is converting or waiting to be converted.
func (s *server) waitForQueue(timeout time.Duration) convert.Status {
	s.t.Helper()
	var st convert.Status
	eventually(s.t, timeout, func() error {
		var err error
		st, err = s.App.Converter.Status(context.Background())
		if err != nil {
			return err
		}
		if len(st.Active)+st.Queued > 0 {
			return fmt.Errorf("%d converting, %d queued", len(st.Active), st.Queued)
		}
		return nil
	})
	return st
}

// mediaFiles lists every file in the media folder, hidden ones included.
func (s *server) mediaFiles() []string {
	s.t.Helper()
	var out []string
	filepath.WalkDir(s.Media, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			rel, _ := filepath.Rel(s.Media, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	return out
}

type probedStream struct {
	CodecType string            `json:"codec_type"`
	CodecName string            `json:"codec_name"`
	Channels  int               `json:"channels"`
	Tags      map[string]string `json:"tags"`
}

func probeStreams(t *testing.T, file string) []probedStream {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-print_format", "json", "-show_streams", file).Output()
	if err != nil {
		t.Fatalf("ffprobe %s: %v", file, err)
	}
	var res struct{ Streams []probedStream }
	if err := json.Unmarshal(out, &res); err != nil {
		t.Fatal(err)
	}
	return res.Streams
}

// Files that are not browser-ready MP4s are converted in the background as
// soon as the scanner finds them, without anyone opening them, and the
// originals are deleted. Everything attached to the media item survives.
func TestConvertReplacesOriginals(t *testing.T) {
	s := startServer(t, serverOpts{media: []string{movieMP4, movieAVI, multiTrack, episode1}, convert: true})
	s.waitForMedia(4)
	ctx := context.Background()
	ids := map[string]int64{}
	for _, p := range []string{movieMP4, movieAVI, multiTrack, episode1} {
		ids[p] = s.mediaID(p)
	}
	viewer := s.register("viewer")
	s.App.Store.SaveProgress(ctx, 1, ids[movieAVI], 6, false)

	s.waitForQueue(2 * time.Minute)

	wantFiles := []string{
		"Movies/Multi Language (2018).eng.srt", // the built-in subtitle, extracted
		"Movies/Multi Language (2018).mp4",
		"Movies/Old Format Movie (1999).mp4",
		"Movies/The Test Movie (2020).mp4", // already an MP4: untouched
		"Test Show (2019)/Season 1/Test.Show.S01E01.Pilot.mp4",
	}
	if got := s.mediaFiles(); strings.Join(got, "\n") != strings.Join(wantFiles, "\n") {
		t.Fatalf("media folder after converting:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(wantFiles, "\n"))
	}

	// Same media items (same IDs), now pointing at MP4s.
	for orig, mp4 := range map[string]string{
		movieAVI:   "Movies/Old Format Movie (1999).mp4",
		multiTrack: "Movies/Multi Language (2018).mp4",
		episode1:   "Test Show (2019)/Season 1/Test.Show.S01E01.Pilot.mp4",
		movieMP4:   movieMP4,
	} {
		m, err := s.App.Store.MediaByID(ctx, ids[orig])
		if err != nil {
			t.Fatal(err)
		}
		if m.Path != mp4 || m.VideoCodec != "h264" || (m.AudioCodec != "aac" && m.AudioCodec != "mp3") {
			t.Errorf("%s became %+v", orig, m)
		}
	}
	if pos, _ := s.App.Store.Progress(ctx, 1, ids[movieAVI]); pos != 6 {
		t.Errorf("progress after conversion = %v, want 6", pos)
	}

	// The multi-language file keeps both audio tracks, French still 5.1, and no subtitles inside.
	streams := probeStreams(t, filepath.Join(s.Media, "Movies", "Multi Language (2018).mp4"))
	var audio []string
	for _, st := range streams {
		switch st.CodecType {
		case "audio":
			audio = append(audio, fmt.Sprintf("%s/%s/%d", st.Tags["language"], st.CodecName, st.Channels))
		case "subtitle":
			t.Error("subtitles must stay out of the MP4")
		}
	}
	if strings.Join(audio, " ") != "fre/aac/6 eng/aac/2" {
		t.Errorf("audio tracks = %v, want French 5.1 and English stereo AAC", audio)
	}

	// The extracted subtitle is imported for the same media item, so corrections work on it.
	eventually(t, 15*time.Second, func() error {
		subs, err := s.App.Store.Subtitles(ctx, ids[multiTrack])
		if err != nil || len(subs) != 1 || subs[0].Language != "eng" {
			return fmt.Errorf("subtitles = %+v, %v", subs, err)
		}
		return nil
	})

	// The converted files now play directly.
	s.publishAll()
	_, page := viewer.get("/watch/" + itoa(ids[movieAVI]))
	if !strings.Contains(page, `data-hls="false"`) {
		t.Error("a converted file should play directly, without HLS")
	}

	// A new file dropped in later is picked up and converted without being opened.
	copyMedia(t, s.Media, episode2)
	s.waitForMedia(5)
	s.waitForQueue(time.Minute)
	if _, err := os.Stat(filepath.Join(s.Media, "Test Show (2019)", "Season 1", "Test.Show.S01E02.The.Second.One.mp4")); err != nil {
		t.Errorf("a file added later was not converted: %v", err)
	}
}

// A failed conversion leaves the original alone, is reported in the control
// panel, and can be retried.
func TestConvertFailureKeepsOriginal(t *testing.T) {
	broken := filepath.Join(t.TempDir(), "ffmpeg-fails-conversions")
	ffmpeg, _ := exec.LookPath("ffmpeg")
	os.WriteFile(broken, []byte("#!/bin/sh\ncase \"$*\" in *-progress*) echo 'Conversion failed!' >&2; exit 1;; esac\nexec "+ffmpeg+" \"$@\"\n"), 0o755)

	s := startServer(t, serverOpts{media: []string{movieAVI}, convert: true, ffmpeg: broken})
	s.waitForMedia(1)
	id := s.mediaID(movieAVI)
	admin := s.register("admin")
	ctx := context.Background()

	eventually(t, 30*time.Second, func() error {
		f, err := s.App.Store.ConversionFailures(ctx)
		if err != nil || len(f) != 1 {
			return fmt.Errorf("failures = %+v, %v", f, err)
		}
		return nil
	})
	if got := s.mediaFiles(); len(got) != 1 || got[0] != movieAVI {
		t.Errorf("media folder after a failed conversion: %v (the original must stay, with no leftovers)", got)
	}
	_, panel := admin.get("/admin")
	if !strings.Contains(panel, "Could not convert") || !strings.Contains(panel, "Conversion failed!") {
		t.Error("the control panel does not report the failure and its reason")
	}
	if resp, _ := admin.post("/admin/media/"+itoa(id)+"/retry-conversion", url.Values{}); resp.StatusCode != 303 {
		t.Fatalf("retry: %d", resp.StatusCode)
	}
	m, _ := s.App.Store.MediaByID(ctx, id)
	if m.Path != movieAVI {
		t.Errorf("path after failure = %q", m.Path)
	}
	// The retry runs (and fails again with this ffmpeg) rather than staying stuck.
	eventually(t, 30*time.Second, func() error {
		f, _ := s.App.Store.ConversionFailures(ctx)
		if len(f) != 1 {
			return fmt.Errorf("retry has not finished")
		}
		return nil
	})
}

// With a read-only media folder nothing is touched and the control panel says why.
func TestConvertReadOnlyMediaFolder(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	s := startServer(t, serverOpts{media: []string{movieAVI}})
	s.waitForMedia(1)
	os.Chmod(filepath.Join(s.Media, "Movies"), 0o555)
	os.Chmod(s.Media, 0o555)
	t.Cleanup(func() { os.Chmod(s.Media, 0o755); os.Chmod(filepath.Join(s.Media, "Movies"), 0o755) })
	c := &convert.Converter{Store: s.App.Store, Tools: ffmpeg.Tools{FFmpeg: "ffmpeg", FFprobe: "ffprobe"}, MediaDir: s.Media, Max: 1}
	runCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c.Run(runCtx) // returns at once when the folder is not writable
	st, _ := c.Status(context.Background())
	if !strings.Contains(st.Disabled, "read-only") {
		t.Errorf("status = %+v, want it disabled because the folder is read-only", st)
	}
	if got := s.mediaFiles(); len(got) != 1 {
		t.Errorf("files changed: %v", got)
	}
}

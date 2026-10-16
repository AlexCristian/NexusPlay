//go:build integration

package integration

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// The scanner reacts to files appearing and disappearing without waiting for
// its periodic rescan (the test server's interval is an hour).
func TestScannerFollowsTheMediaFolder(t *testing.T) {
	s := startServer(t, serverOpts{media: []string{movieMP4, movieSRT}})
	s.waitForMedia(1)

	subs, err := s.App.Store.Subtitles(context.Background(), s.mediaID(movieMP4))
	if err != nil || len(subs) != 1 || subs[0].Language != "en" {
		t.Fatalf("sidecar subtitles = %+v, %v", subs, err)
	}

	copyMedia(t, s.Media, episode1)
	s.waitForMedia(2)
	pending, _ := s.App.Store.PendingMedia(context.Background())
	for _, m := range pending {
		if m.Path == episode1 && (m.Title != "Pilot" || m.Season != 1 || m.Episode != 1 || m.VideoCodec != "h264") {
			t.Errorf("new episode = %+v", m)
		}
	}

	if err := os.Remove(filepath.Join(s.Media, filepath.FromSlash(episode1))); err != nil {
		t.Fatal(err)
	}
	s.waitForMedia(1)
}

// A file browsers cannot play is transcoded to HLS on demand, and the output
// really is H.264 + AAC.
func TestHLSTranscode(t *testing.T) {
	s := startServer(t, serverOpts{media: []string{movieAVI}})
	s.waitForMedia(1)
	s.publishAll()
	viewer := s.register("viewer")
	id := s.mediaID(movieAVI)

	_, page := viewer.get("/watch/" + itoa(id))
	if !strings.Contains(page, `data-hls="true"`) || !strings.Contains(page, "/hls/index.m3u8") {
		t.Fatal("watch page should use HLS for an AVI file")
	}
	resp, playlist := viewer.get("/media/" + itoa(id) + "/hls/index.m3u8")
	if resp.StatusCode != 200 || !strings.Contains(playlist, "#EXTINF") {
		t.Fatalf("playlist: %d\n%s", resp.StatusCode, playlist)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/vnd.apple.mpegurl" {
		t.Errorf("playlist content type = %q", ct)
	}
	var segment string
	for _, line := range strings.Split(playlist, "\n") {
		if strings.HasSuffix(line, ".ts") {
			segment = line
			break
		}
	}
	resp, body := viewer.get("/media/" + itoa(id) + "/hls/" + segment)
	if resp.StatusCode != 200 || len(body) < 1000 || resp.Header.Get("Content-Type") != "video/mp2t" {
		t.Fatalf("segment %s: %d, %d bytes, %q", segment, resp.StatusCode, len(body), resp.Header.Get("Content-Type"))
	}
	seg := filepath.Join(t.TempDir(), "seg.ts")
	os.WriteFile(seg, []byte(body), 0o644)
	out, err := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_name", "-of", "csv=p=0", seg).Output()
	if err != nil {
		t.Fatal(err)
	}
	// MPEG-TS lists each stream twice (under its program and on its own).
	codecs := strings.Fields(string(out))
	slices.Sort(codecs)
	codecs = slices.Compact(codecs)
	if strings.Join(codecs, ",") != "aac,h264" {
		t.Errorf("segment codecs = %v, want h264,aac", codecs)
	}

	// Requests outside the HLS file pattern are refused.
	if resp, _ := viewer.get("/media/" + itoa(id) + "/hls/..%2f..%2fnexusplay.db"); resp.StatusCode != 404 {
		t.Errorf("path traversal in HLS route: %d", resp.StatusCode)
	}
}

// With the transcode limit reached, a second video gets a clear 503 instead
// of starting another ffmpeg.
func TestTranscodeLimit(t *testing.T) {
	// A slow ffmpeg keeps the first transcode busy while the second request arrives.
	slow := filepath.Join(t.TempDir(), "slow-ffmpeg")
	ffmpeg, _ := exec.LookPath("ffmpeg")
	os.WriteFile(slow, []byte("#!/bin/sh\nsleep 4\nexec "+ffmpeg+" \"$@\"\n"), 0o755)

	s := startServer(t, serverOpts{media: []string{movieAVI, episode1}, maxTranscodes: 1, ffmpeg: slow})
	s.waitForMedia(2)
	s.publishAll()
	viewer := s.register("viewer")

	var wg sync.WaitGroup
	var first *http.Response
	wg.Add(1)
	go func() {
		defer wg.Done()
		first, _ = viewer.get("/media/" + itoa(s.mediaID(movieAVI)) + "/hls/index.m3u8")
	}()
	time.Sleep(500 * time.Millisecond)
	second, body := viewer.get("/media/" + itoa(s.mediaID(episode1)) + "/hls/index.m3u8")
	if second.StatusCode != http.StatusServiceUnavailable || second.Header.Get("Retry-After") == "" || !strings.Contains(body, "busy") {
		t.Errorf("second transcode: %d %q", second.StatusCode, body)
	}
	wg.Wait()
	if first.StatusCode != 200 {
		t.Errorf("first transcode: %d", first.StatusCode)
	}
}

// Progress saved by the player shows up under "Previously watching", and
// finishing a video marks it watched.
func TestProgressAndWatched(t *testing.T) {
	s := startServer(t, serverOpts{media: []string{episode1, episode2}})
	s.waitForMedia(2)
	s.publishAll()
	u := s.register("viewer")
	ep1, ep2 := s.mediaID(episode1), s.mediaID(episode2)

	if resp, _ := u.post("/me/progress", url.Values{"media_id": {itoa(ep1)}, "position": {"7.5"}}); resp.StatusCode != http.StatusNoContent {
		t.Fatalf("save progress: %d", resp.StatusCode)
	}
	_, list := u.get("/me/resume")
	if !strings.Contains(list, "Test Show · S1E1 · Pilot") {
		t.Errorf("resume list:\n%s", list)
	}
	_, page := u.get("/watch/" + itoa(ep1))
	if !strings.Contains(page, `data-resume="7.5"`) {
		t.Error("watch page does not resume at the saved position")
	}

	// Saving a position at the very end counts as finished.
	u.post("/me/progress", url.Values{"media_id": {itoa(ep2)}, "position": {"11.9"}})
	coll, _ := s.App.Store.CollectionByName(context.Background(), "Test Show")
	_, dialog := u.get("/collections/" + itoa(coll.ID))
	if strings.Count(dialog, `title="Watched"`) != 1 {
		t.Errorf("expected exactly one watched episode in the collection dialog")
	}
	_, list = u.get("/me/resume")
	if strings.Contains(list, "The Second One") {
		t.Error("a finished episode is still listed as in progress")
	}
}

// Publishing an episode notifies subscribers, through the real admin form.
func TestPublishNotifiesSubscribers(t *testing.T) {
	s := startServer(t, serverOpts{media: []string{episode1, episode2}})
	s.waitForMedia(2)
	admin := s.register("admin")
	fan := s.register("fan")
	s.publishAll(episode2)
	coll, _ := s.App.Store.CollectionByName(context.Background(), "Test Show")

	if _, ribbon := fan.post("/collections/"+itoa(coll.ID)+"/subscribe", url.Values{}); !strings.Contains(ribbon, "Subscribed") {
		t.Fatalf("subscribe: %s", ribbon)
	}

	// The admin reviews and publishes episode 2 with the multipart form.
	id := s.mediaID(episode2)
	_, form := admin.get("/admin/media/" + itoa(id))
	if !strings.Contains(form, `value="Test Show"`) || !strings.Contains(form, `value="The Second One"`) {
		t.Fatalf("review form is not prefilled from the file name:\n%s", form)
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range map[string]string{
		"csrf_token": admin.csrf, "title": "The Second One", "collection": "Test Show",
		"season": "1", "episode": "2", "publish": "1",
	} {
		mw.WriteField(k, v)
	}
	mw.Close()
	resp, _ := admin.do("POST", "/admin/media/"+itoa(id), &buf, mw.FormDataContentType())
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("publish: %d", resp.StatusCode)
	}

	_, notes := fan.get("/me/notifications")
	if !strings.Contains(notes, "The Second One") {
		t.Fatalf("subscriber was not notified:\n%s", notes)
	}
	_, adminNotes := admin.get("/me/notifications")
	if strings.Contains(adminNotes, "The Second One") {
		t.Error("a user who did not subscribe was notified")
	}
	fan.get("/watch/" + itoa(id))
	if _, notes := fan.get("/me/notifications"); strings.Contains(notes, "The Second One") {
		t.Error("watching the episode should clear its notification")
	}
}

// Uploading a subtitle through the admin form, then reading it back as WebVTT.
func TestSubtitleUpload(t *testing.T) {
	s := startServer(t, serverOpts{media: []string{episode1}})
	s.waitForMedia(1)
	s.publishAll()
	admin := s.register("admin")
	id := s.mediaID(episode1)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("csrf_token", admin.csrf)
	mw.WriteField("language", "ro")
	fw, _ := mw.CreateFormFile("file", "pilot.srt")
	io.WriteString(fw, "1\n00:00:02,000 --> 00:00:03,500\nBună ziua, ce faceți?\n")
	mw.Close()
	resp, _ := admin.do("POST", "/admin/media/"+itoa(id)+"/subtitles", &buf, mw.FormDataContentType())
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("upload: %d", resp.StatusCode)
	}
	subs, _ := s.App.Store.Subtitles(context.Background(), id)
	if len(subs) != 1 || subs[0].Label != "Română" {
		t.Fatalf("subtitles = %+v", subs)
	}
	_, page := admin.get("/watch/" + itoa(id))
	if !strings.Contains(page, `srclang="ro"`) {
		t.Error("watch page has no track for the uploaded subtitle")
	}
	resp, vtt := admin.get("/subtitles/" + itoa(subs[0].ID))
	want := "WEBVTT\n\n00:00:02.000 --> 00:00:03.500\nBună ziua, ce faceți?\n"
	if resp.Header.Get("Content-Type") != "text/vtt; charset=utf-8" || !strings.HasPrefix(vtt, want) {
		t.Errorf("vtt = %q", vtt)
	}
}

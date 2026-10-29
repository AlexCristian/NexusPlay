package library

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// fakeProber answers every probe with H.264 video and AAC audio. If gate is
// set, each probe blocks until it is closed.
type fakeProber struct {
	gate    chan struct{}
	probing chan struct{} // receives a value when a probe starts
	mu      sync.Mutex
	probed  []string
}

func (f *fakeProber) Probe(ctx context.Context, file string) (ffmpeg.Probe, error) {
	f.mu.Lock()
	f.probed = append(f.probed, filepath.Base(file))
	f.mu.Unlock()
	if f.probing != nil {
		f.probing <- struct{}{}
	}
	if f.gate != nil {
		select {
		case <-f.gate:
		case <-ctx.Done():
			return ffmpeg.Probe{}, ctx.Err()
		}
	}
	return ffmpeg.Probe{Duration: 60, Container: "mov,mp4", Streams: []ffmpeg.Stream{
		{Index: 0, Type: "video", Codec: "h264", PixFmt: "yuv420p"},
		{Index: 1, Type: "audio", Codec: "aac"},
	}}, nil
}

func newUnitScanner(t *testing.T, p Prober) (*Scanner, *store.Store, string) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(context.Background(), filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	media := filepath.Join(dir, "media")
	os.MkdirAll(media, 0o755)
	sc := NewScanner(Deps{Store: st, Prober: p, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, Config{MediaDirectory: media})
	return sc, st, media
}

// writeSettled creates a file dated an hour ago, past the settle time.
func writeSettled(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(path, old, old)
}

// The whole scan, with no ffprobe installed.
func TestScanWithFakeProber(t *testing.T) {
	sc, st, media := newUnitScanner(t, &fakeProber{})
	ctx := context.Background()
	writeSettled(t, filepath.Join(media, "Movies", "Alien (1979).mp4"), "video")
	writeSettled(t, filepath.Join(media, "Movies", "Alien (1979).en.srt"), "1\n00:00:01,000 --> 00:00:02,000\nHello\n")
	writeSettled(t, filepath.Join(media, "Cosmos", "Season 1", "Cosmos.S01E02.mkv"), "video")
	os.WriteFile(filepath.Join(media, "Movies", "New.mp4"), []byte("still copying"), 0o644)

	res, err := sc.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 2 || res.Subtitles != 1 || res.Waiting != 1 || res.Removed != 0 {
		t.Errorf("result = %+v, want 2 added, 1 subtitle, 1 waiting", res)
	}
	pending, _ := st.PendingMedia(ctx)
	if len(pending) != 2 || pending[0].PixFmt != "yuv420p" {
		t.Errorf("pending = %+v", pending)
	}

	os.Remove(filepath.Join(media, "Movies", "Alien (1979).mp4"))
	if res, _ := sc.Scan(ctx); res.Removed != 1 || res.Added != 0 {
		t.Errorf("after deleting a file: %+v, want 1 removed", res)
	}
}

// A trigger sent before Run starts is kept, not dropped.
func TestTriggerBeforeRunIsKept(t *testing.T) {
	sc, _, _ := newUnitScanner(t, &fakeProber{})
	sc.Trigger()
	select {
	case <-sc.trigger:
	default:
		t.Fatal("the trigger was dropped")
	}
}

// The converter's lock (Exclusive) is free while the scanner probes files,
// which can take minutes on a large first scan.
func TestScanLockReleasedWhileProbing(t *testing.T) {
	p := &fakeProber{gate: make(chan struct{}), probing: make(chan struct{}, 1)}
	sc, _, media := newUnitScanner(t, p)
	writeSettled(t, filepath.Join(media, "slow.mp4"), "video")

	done := make(chan Result, 1)
	go func() {
		res, _ := sc.Scan(context.Background())
		done <- res
	}()
	<-p.probing // the scan is now inside ffprobe

	entered := make(chan struct{})
	go sc.Exclusive(func() error { close(entered); return nil })
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Exclusive was blocked while the scan was probing")
	}
	close(p.gate)
	if res := <-done; res.Added != 1 {
		t.Errorf("result = %+v", res)
	}
}

// A file that disappears between the walk and the database update (for
// example renamed by the converter) is not inserted, and a row whose file
// reappears is not deleted.
func TestReconcileRechecksTheDisk(t *testing.T) {
	p := &fakeProber{gate: make(chan struct{}), probing: make(chan struct{}, 1)}
	sc, st, media := newUnitScanner(t, p)
	ctx := context.Background()
	writeSettled(t, filepath.Join(media, "Movie.mkv"), "video")
	writeSettled(t, filepath.Join(media, "Other.mp4"), "video")
	id, _, _ := st.InsertScannedMedia(ctx, store.ScannedMedia{Path: "Other.mp4", Title: "Other"})

	done := make(chan Result, 1)
	go func() {
		res, _ := sc.Scan(ctx)
		done <- res
	}()
	<-p.probing
	// While Movie.mkv is being probed, the converter swaps it for an MP4
	// that the walk never saw, and updates the row.
	sc.Exclusive(func() error {
		os.Rename(filepath.Join(media, "Movie.mkv"), filepath.Join(media, "Movie.mp4"))
		_, _, err := st.InsertScannedMedia(ctx, store.ScannedMedia{Path: "Movie.mp4", Title: "Movie"})
		return err
	})
	close(p.gate)
	res := <-done
	if res.Added != 0 || res.Removed != 0 {
		t.Errorf("result = %+v; want nothing added (the .mkv is gone) and nothing removed (the .mp4 exists)", res)
	}
	paths, _ := st.MediaPaths(ctx)
	if _, ok := paths["Movie.mp4"]; !ok || paths["Other.mp4"] != id || len(paths) != 2 {
		t.Errorf("library = %v", paths)
	}
}

func TestSidecarLanguage(t *testing.T) {
	for _, tt := range []struct {
		video, sub, lang string
		ok               bool
	}{
		{"Movies/Alien.mp4", "Movies/Alien.srt", "und", true},
		{"Movies/Alien.mp4", "Movies/Alien.en.srt", "en", true},
		{"Movies/Alien.mp4", "Movies/Alien.forced.EN.vtt", "en", true},
		{"Movies/Alien.mp4", "Movies/Aliens.en.srt", "", false},
		{"Movies/Alien.mp4", "Other/Alien.en.srt", "", false},
	} {
		lang, ok := SidecarLanguage(tt.video, tt.sub)
		if lang != tt.lang || ok != tt.ok {
			t.Errorf("SidecarLanguage(%q, %q) = %q, %v; want %q, %v", tt.video, tt.sub, lang, ok, tt.lang, tt.ok)
		}
	}
}

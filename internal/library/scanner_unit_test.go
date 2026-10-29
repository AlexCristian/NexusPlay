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
	mutex   sync.Mutex
	probed  []string
}

func (fakeProber *fakeProber) Probe(ctx context.Context, file string) (ffmpeg.Probe, error) {
	fakeProber.mutex.Lock()
	fakeProber.probed = append(fakeProber.probed, filepath.Base(file))
	fakeProber.mutex.Unlock()
	if fakeProber.probing != nil {
		fakeProber.probing <- struct{}{}
	}
	if fakeProber.gate != nil {
		select {
		case <-fakeProber.gate:
		case <-ctx.Done():
			return ffmpeg.Probe{}, ctx.Err()
		}
	}
	return ffmpeg.Probe{Duration: 60, Container: "mov,mp4", Streams: []ffmpeg.Stream{
		{Index: 0, Type: "video", Codec: "h264", PixelFormat: "yuv420p"},
		{Index: 1, Type: "audio", Codec: "aac"},
	}}, nil
}

func newUnitScanner(t *testing.T, prober Prober) (*Scanner, *store.Store, string) {
	t.Helper()
	directory := t.TempDir()
	database, err := store.Open(context.Background(), filepath.Join(directory, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	media := filepath.Join(directory, "media")
	os.MkdirAll(media, 0o755)
	scanner := NewScanner(Deps{Store: database, Prober: prober, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, Config{MediaDirectory: media})
	return scanner, database, media
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
	scanner, store, media := newUnitScanner(t, &fakeProber{})
	ctx := context.Background()
	writeSettled(t, filepath.Join(media, "Movies", "Alien (1979).mp4"), "video")
	writeSettled(t, filepath.Join(media, "Movies", "Alien (1979).en.srt"), "1\n00:00:01,000 --> 00:00:02,000\nHello\n")
	writeSettled(t, filepath.Join(media, "Cosmos", "Season 1", "Cosmos.S01E02.mkv"), "video")
	os.WriteFile(filepath.Join(media, "Movies", "New.mp4"), []byte("still copying"), 0o644)

	result, err := scanner.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 2 || result.Subtitles != 1 || result.Waiting != 1 || result.Removed != 0 {
		t.Errorf("result = %+v, want 2 added, 1 subtitle, 1 waiting", result)
	}
	pending, _ := store.PendingMedia(ctx)
	if len(pending) != 2 || pending[0].PixelFormat != "yuv420p" {
		t.Errorf("pending = %+v", pending)
	}

	os.Remove(filepath.Join(media, "Movies", "Alien (1979).mp4"))
	if result, _ := scanner.Scan(ctx); result.Removed != 1 || result.Added != 0 {
		t.Errorf("after deleting a file: %+v, want 1 removed", result)
	}
}

// A trigger sent before Run starts is kept, not dropped.
func TestTriggerBeforeRunIsKept(t *testing.T) {
	scanner, _, _ := newUnitScanner(t, &fakeProber{})
	scanner.Trigger()
	select {
	case <-scanner.trigger:
	default:
		t.Fatal("the trigger was dropped")
	}
}

// The converter's lock (Exclusive) is free while the scanner probes files,
// which can take minutes on a large first scan.
func TestScanLockReleasedWhileProbing(t *testing.T) {
	prober := &fakeProber{gate: make(chan struct{}), probing: make(chan struct{}, 1)}
	scanner, _, media := newUnitScanner(t, prober)
	writeSettled(t, filepath.Join(media, "slow.mp4"), "video")

	done := make(chan Result, 1)
	go func() {
		result, _ := scanner.Scan(context.Background())
		done <- result
	}()
	<-prober.probing // the scan is now inside ffprobe

	entered := make(chan struct{})
	go scanner.Exclusive(func() error { close(entered); return nil })
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("Exclusive was blocked while the scan was probing")
	}
	close(prober.gate)
	if result := <-done; result.Added != 1 {
		t.Errorf("result = %+v", result)
	}
}

// A file that disappears between the walk and the database update (for
// example renamed by the converter) is not inserted, and a row whose file
// reappears is not deleted.
func TestReconcileRechecksTheDisk(t *testing.T) {
	prober := &fakeProber{gate: make(chan struct{}), probing: make(chan struct{}, 1)}
	scanner, database, media := newUnitScanner(t, prober)
	ctx := context.Background()
	writeSettled(t, filepath.Join(media, "Movie.mkv"), "video")
	writeSettled(t, filepath.Join(media, "Other.mp4"), "video")
	id, _, _ := database.InsertScannedMedia(ctx, store.ScannedMedia{Path: "Other.mp4", Title: "Other"})

	done := make(chan Result, 1)
	go func() {
		result, _ := scanner.Scan(ctx)
		done <- result
	}()
	<-prober.probing
	// While Movie.mkv is being probed, the converter swaps it for an MP4
	// that the walk never saw, and updates the row.
	scanner.Exclusive(func() error {
		os.Rename(filepath.Join(media, "Movie.mkv"), filepath.Join(media, "Movie.mp4"))
		_, _, err := database.InsertScannedMedia(ctx, store.ScannedMedia{Path: "Movie.mp4", Title: "Movie"})
		return err
	})
	close(prober.gate)
	result := <-done
	if result.Added != 0 || result.Removed != 0 {
		t.Errorf("result = %+v; want nothing added (the .mkv is gone) and nothing removed (the .mp4 exists)", result)
	}
	paths, _ := database.MediaPaths(ctx)
	if _, found := paths["Movie.mp4"]; !found || paths["Other.mp4"] != id || len(paths) != 2 {
		t.Errorf("library = %v", paths)
	}
}

func TestSidecarLanguage(t *testing.T) {
	for _, testCase := range []struct {
		video, subtitle, language string
		isSidecar                 bool
	}{
		{"Movies/Alien.mp4", "Movies/Alien.srt", "und", true},
		{"Movies/Alien.mp4", "Movies/Alien.en.srt", "en", true},
		{"Movies/Alien.mp4", "Movies/Alien.forced.EN.vtt", "en", true},
		{"Movies/Alien.mp4", "Movies/Aliens.en.srt", "", false},
		{"Movies/Alien.mp4", "Other/Alien.en.srt", "", false},
	} {
		language, isSidecar := SidecarLanguage(testCase.video, testCase.subtitle)
		if language != testCase.language || isSidecar != testCase.isSidecar {
			t.Errorf("SidecarLanguage(%q, %q) = %q, %v; want %q, %v", testCase.video, testCase.subtitle, language, isSidecar, testCase.language, testCase.isSidecar)
		}
	}
}

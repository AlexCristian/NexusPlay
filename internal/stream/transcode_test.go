package stream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/media"
)

func TestFFmpegArguments(t *testing.T) {
	remux := ffmpegArguments("in.mkv", "/out", media.Format{VideoCodec: "h264", AudioCodec: "aac", PixFmt: "yuv420p"})
	if !slices.Contains(remux, "copy") || slices.Contains(remux, "libx264") {
		t.Errorf("h264/aac should be copied, got %v", remux)
	}
	if tenBit := ffmpegArguments("in.mkv", "/out", media.Format{VideoCodec: "h264", AudioCodec: "aac", PixFmt: "yuv420p10le"}); !slices.Contains(tenBit, "libx264") {
		t.Errorf("10-bit h264 must be re-encoded for browsers, got %v", tenBit)
	}
	encode := ffmpegArguments("in.avi", "/out", media.Format{VideoCodec: "mpeg4", AudioCodec: "mp3"})
	if !slices.Contains(encode, "libx264") || !slices.Contains(encode, "aac") {
		t.Errorf("mpeg4/mp3 should be encoded, got %v", encode)
	}
	if encode[len(encode)-1] != "/out/index.m3u8" {
		t.Errorf("playlist should be the last argument, got %v", encode)
	}
}

// fakeStarter pretends to be ffmpeg: it writes a playlist with one segment
// into the output folder, then runs until released or cancelled.
type fakeStarter struct {
	starts   atomic.Int32
	failWith error         // returned by Wait when released
	release  chan struct{} // closed to let every process finish
}

func newFakeStarter() *fakeStarter { return &fakeStarter{release: make(chan struct{})} }

func (fakeStarter *fakeStarter) Start(ctx context.Context, arguments []string) (Process, error) {
	fakeStarter.starts.Add(1)
	playlist := arguments[len(arguments)-1]
	directory := filepath.Dir(playlist)
	os.WriteFile(filepath.Join(directory, "seg00000.ts"), []byte("segment"), 0o644)
	os.WriteFile(playlist, []byte("#EXTM3U\n#EXTINF:6,\nseg00000.ts\n"), 0o644)
	return &fakeProcess{ctx: ctx, fakeStarter: fakeStarter}, nil
}

type fakeProcess struct {
	ctx         context.Context
	fakeStarter *fakeStarter
}

func (fakeProcess *fakeProcess) Wait() error {
	select {
	case <-fakeProcess.ctx.Done():
		return fakeProcess.ctx.Err()
	case <-fakeProcess.fakeStarter.release:
		return fakeProcess.fakeStarter.failWith
	}
}

func newTestManager(t *testing.T, fakeStarter *fakeStarter, max int) *Manager {
	t.Helper()
	manager, err := NewManager(fakeStarter, Config{Directory: filepath.Join(t.TempDir(), "transcode"), Max: max},
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

var h264 = media.Format{Path: "a.mkv", VideoCodec: "h264", AudioCodec: "aac", PixFmt: "yuv420p"}

func TestPlaylistReusesARunningTranscode(t *testing.T) {
	fakeStarter := newFakeStarter()
	manager := newTestManager(t, fakeStarter, 2)
	var waitGroup sync.WaitGroup
	for i := 0; i < 5; i++ {
		waitGroup.Go(func() {
			if _, err := manager.Playlist(context.Background(), 1, "a.mkv", h264); err != nil {
				t.Error(err)
			}
		})
	}
	waitGroup.Wait()
	if starts := fakeStarter.starts.Load(); starts != 1 {
		t.Errorf("%d ffmpeg processes for one file, want 1", starts)
	}
	file, err := manager.Segment(context.Background(), 1, "seg00000.ts")
	if err != nil || filepath.Base(file) != "seg00000.ts" {
		t.Errorf("Segment = %q, %v", file, err)
	}
	for _, bad := range []string{"../../x", "index.m3u8", "seg1.ts", ".ts"} {
		if _, err := manager.Segment(context.Background(), 1, bad); !errors.Is(err, ErrNoSegment) {
			t.Errorf("Segment(%q) = %v, want ErrNoSegment", bad, err)
		}
	}
}

func TestTouchUnknownJob(t *testing.T) {
	manager := newTestManager(t, newFakeStarter(), 1)
	if directory, found := manager.Touch(42); found || directory != "" {
		t.Errorf("Touch on no transcode = %q, %v", directory, found)
	}
	if _, err := manager.Segment(context.Background(), 42, "seg00000.ts"); !errors.Is(err, ErrNoSegment) {
		t.Errorf("Segment on no transcode = %v", err)
	}
}

func TestStartWhenFullIsBusy(t *testing.T) {
	fakeStarter := newFakeStarter()
	manager := newTestManager(t, fakeStarter, 1)
	if _, err := manager.Playlist(context.Background(), 1, "a.mkv", h264); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Playlist(context.Background(), 2, "b.mkv", h264); !errors.Is(err, ErrBusy) {
		t.Errorf("second file at the limit: %v, want ErrBusy", err)
	}
	close(fakeStarter.release) // the first one finishes: finished jobs do not count
	time.Sleep(20 * time.Millisecond)
	if _, err := manager.Playlist(context.Background(), 2, "b.mkv", h264); err != nil {
		t.Errorf("after the first finished: %v", err)
	}
}

// A failed transcode is retried by the next request, in a fresh folder.
func TestFailedTranscodeIsRetried(t *testing.T) {
	fakeStarter := newFakeStarter()
	fakeStarter.failWith = errors.New("ffmpeg: exit status 1: broken file")
	close(fakeStarter.release)
	manager := newTestManager(t, fakeStarter, 1)
	manager.Playlist(context.Background(), 1, "a.mkv", h264) // fails or races to the playlist; either is fine
	time.Sleep(20 * time.Millisecond)
	manager.Playlist(context.Background(), 1, "a.mkv", h264)
	if starts := fakeStarter.starts.Load(); starts != 2 {
		t.Errorf("%d starts, want a retry after the failure", starts)
	}
}

// Evicting an old transcode must not delete the folder of a newer one for
// the same media (each job has its own folder).
func TestEvictionKeepsANewerJobsFolder(t *testing.T) {
	fakeStarter := newFakeStarter()
	manager := newTestManager(t, fakeStarter, 2)
	first, _ := manager.Playlist(context.Background(), 1, "a.mkv", h264)
	manager.sweep(time.Now().Add(time.Hour)) // idle: evicted, removal in progress
	second, err := manager.Playlist(context.Background(), 1, "a.mkv", h264)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(first) == filepath.Dir(second) {
		t.Fatal("a restarted transcode reused the old folder")
	}
	time.Sleep(50 * time.Millisecond) // let the old removal finish
	if _, err := os.Stat(second); err != nil {
		t.Errorf("the newer transcode's playlist was deleted: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(first)); !os.IsNotExist(err) {
		t.Errorf("the evicted folder is still there: %v", err)
	}
}

// Shutdown stops every ffmpeg, waits for it and removes all output.
func TestRunStopsAndCleansUpOnShutdown(t *testing.T) {
	fakeStarter := newFakeStarter()
	manager := newTestManager(t, fakeStarter, 2)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- manager.Run(ctx) }()
	manager.Playlist(context.Background(), 1, "a.mkv", h264)
	manager.Playlist(context.Background(), 2, "b.mkv", h264)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("Run = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after cancel")
	}
	entries, _ := os.ReadDir(manager.config.Directory)
	if len(entries) != 0 {
		t.Errorf("output left after shutdown: %v", entries)
	}
}

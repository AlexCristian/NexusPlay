package app

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/config"
)

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

// testConfig uses "true" for ffmpeg and ffprobe: present on every system and
// never actually asked to convert anything in these tests.
func testConfig(t *testing.T) config.Config {
	t.Helper()
	dir := t.TempDir()
	c := config.Defaults()
	c.MediaDir = filepath.Join(dir, "media")
	c.DataDir = filepath.Join(dir, "data")
	c.FFmpegPath, c.FFprobePath = "true", "true"
	os.MkdirAll(c.MediaDir, 0o755)
	return c
}

func TestNewFailsWithoutFFmpeg(t *testing.T) {
	c := testConfig(t)
	c.FFmpegPath = filepath.Join(t.TempDir(), "no-ffmpeg-here")
	_, err := New(context.Background(), c, quiet)
	if err == nil || !strings.Contains(err.Error(), "NEXUSPLAY_FFMPEG") {
		t.Errorf("err = %v, want it to explain how to point at ffmpeg", err)
	}
}

func TestNewFailsWithoutMediaFolder(t *testing.T) {
	c := testConfig(t)
	c.MediaDir = filepath.Join(t.TempDir(), "missing")
	if _, err := New(context.Background(), c, quiet); err == nil || !strings.Contains(err.Error(), "media folder") {
		t.Errorf("err = %v", err)
	}
}

func serve(t *testing.T, a *App) (url string, stop func() error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Serve(ctx, ln) }()
	return "http://" + ln.Addr().String(), func() error {
		cancel()
		select {
		case err := <-done:
			return err
		case <-time.After(10 * time.Second):
			t.Fatal("Serve did not return")
			return nil
		}
	}
}

// stubbornWorker ignores cancellation for a while, like a file swap.
type stubbornWorker struct {
	hold    time.Duration
	stopped atomic.Bool
}

func (w *stubbornWorker) Run(ctx context.Context) error {
	<-ctx.Done()
	time.Sleep(w.hold)
	w.stopped.Store(true)
	return nil
}

func TestServeClosesStoreAfterWorkers(t *testing.T) {
	a, err := New(context.Background(), testConfig(t), quiet)
	if err != nil {
		t.Fatal(err)
	}
	slow := &stubbornWorker{hold: 200 * time.Millisecond}
	a.workers = append(a.workers, slow)
	url, stop := serve(t, a)
	resp, err := http.Get(url + "/healthz")
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", resp, err)
	}
	resp.Body.Close()
	if err := stop(); err != nil {
		t.Errorf("Serve = %v", err)
	}
	if !slow.stopped.Load() {
		t.Error("Serve returned before a worker finished")
	}
	if err := a.Store.Ping(context.Background()); err == nil {
		t.Error("the database is still open after a clean shutdown")
	}
}

// A worker that does not stop in time leaves the database open rather than
// closing it underneath that worker.
func TestServeShutdownTimeoutLeavesStoreOpen(t *testing.T) {
	a, err := New(context.Background(), testConfig(t), quiet)
	if err != nil {
		t.Fatal(err)
	}
	a.workers = append(a.workers, &stubbornWorker{hold: 2 * time.Second})
	a.shutdownTimeout = 100 * time.Millisecond
	_, stop := serve(t, a)
	stop()
	if err := a.Store.Ping(context.Background()); err != nil {
		t.Errorf("the database was closed while a worker was still running: %v", err)
	}
	t.Cleanup(func() { time.Sleep(2 * time.Second); a.Close() })
}

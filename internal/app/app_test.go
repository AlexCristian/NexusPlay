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
	directory := t.TempDir()
	configuration := config.Defaults()
	configuration.MediaDirectory = filepath.Join(directory, "media")
	configuration.DataDirectory = filepath.Join(directory, "data")
	configuration.FFmpegPath, configuration.FFprobePath = "true", "true"
	os.MkdirAll(configuration.MediaDirectory, 0o755)
	return configuration
}

func TestNewFailsWithoutFFmpeg(t *testing.T) {
	config := testConfig(t)
	config.FFmpegPath = filepath.Join(t.TempDir(), "no-ffmpeg-here")
	_, err := New(context.Background(), config, quiet)
	if err == nil || !strings.Contains(err.Error(), "NEXUSPLAY_FFMPEG") {
		t.Errorf("err = %v, want it to explain how to point at ffmpeg", err)
	}
}

func TestNewFailsWithoutMediaFolder(t *testing.T) {
	config := testConfig(t)
	config.MediaDirectory = filepath.Join(t.TempDir(), "missing")
	if _, err := New(context.Background(), config, quiet); err == nil || !strings.Contains(err.Error(), "media folder") {
		t.Errorf("err = %v", err)
	}
}

func serve(t *testing.T, app *App) (url string, stop func() error) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- app.Serve(ctx, listener) }()
	return "http://" + listener.Addr().String(), func() error {
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

func (stubbornWorker *stubbornWorker) Run(ctx context.Context) error {
	<-ctx.Done()
	time.Sleep(stubbornWorker.hold)
	stubbornWorker.stopped.Store(true)
	return nil
}

func TestServeClosesStoreAfterWorkers(t *testing.T) {
	app, err := New(context.Background(), testConfig(t), quiet)
	if err != nil {
		t.Fatal(err)
	}
	slow := &stubbornWorker{hold: 200 * time.Millisecond}
	app.workers = append(app.workers, slow)
	url, stop := serve(t, app)
	response, err := http.Get(url + "/healthz")
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("healthz: %v %v", response, err)
	}
	response.Body.Close()
	if err := stop(); err != nil {
		t.Errorf("Serve = %v", err)
	}
	if !slow.stopped.Load() {
		t.Error("Serve returned before a worker finished")
	}
	if err := app.Store.Ping(context.Background()); err == nil {
		t.Error("the database is still open after a clean shutdown")
	}
}

// A worker that does not stop in time leaves the database open rather than
// closing it underneath that worker.
func TestServeShutdownTimeoutLeavesStoreOpen(t *testing.T) {
	app, err := New(context.Background(), testConfig(t), quiet)
	if err != nil {
		t.Fatal(err)
	}
	app.workers = append(app.workers, &stubbornWorker{hold: 2 * time.Second})
	app.shutdownTimeout = 100 * time.Millisecond
	_, stop := serve(t, app)
	stop()
	if err := app.Store.Ping(context.Background()); err != nil {
		t.Errorf("the database was closed while a worker was still running: %v", err)
	}
	t.Cleanup(func() { time.Sleep(2 * time.Second); app.Close() })
}

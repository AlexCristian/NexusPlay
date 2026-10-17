// Package app is the composition root: the only place that knows every
// concrete type. It checks the environment, builds each component with its
// dependencies, and runs the HTTP server and the background workers. The
// command and the integration tests both start the server through it.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/auth"
	"github.com/AlexCristian/NexusPlay/internal/config"
	"github.com/AlexCristian/NexusPlay/internal/convert"
	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/library"
	"github.com/AlexCristian/NexusPlay/internal/metadata"
	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/stream"
	"github.com/AlexCristian/NexusPlay/internal/web"
)

// The real implementations of the interfaces each package declares. If one
// stops matching, the build fails here, where the wiring lives.
var (
	_ library.Store     = (*store.Store)(nil)
	_ library.Prober    = ffmpeg.Tools{}
	_ library.Metadata  = (*metadata.Client)(nil)
	_ convert.Store     = (*store.Store)(nil)
	_ convert.Tools     = ffmpeg.Tools{}
	_ convert.Library   = (*library.Scanner)(nil)
	_ auth.UserStore    = (*store.Store)(nil)
	_ auth.SessionStore = (*store.Store)(nil)
	_ web.Conversions   = (*convert.Converter)(nil)
	_ web.Conversions   = convert.Disabled{}
)

const (
	// sessionLifetime is how long a login lasts.
	sessionLifetime = 30 * 24 * time.Hour
	// shutdownTimeout bounds how long Serve waits for background work after
	// it is asked to stop. A file swap ignores cancellation and finishes.
	shutdownTimeout = 30 * time.Second
)

// worker is a background component that runs until its context ends.
type worker interface {
	Run(ctx context.Context) error
}

// App is a configured server with its background workers.
type App struct {
	Cfg        config.Config
	Log        *slog.Logger
	Store      *store.Store
	Scanner    *library.Scanner
	Transcodes *stream.Manager
	Converter  web.Conversions // convert.Disabled when NEXUSPLAY_CONVERT is off
	Handler    http.Handler

	workers         []worker
	shutdownTimeout time.Duration
}

// New checks the environment, opens the database and builds every component.
// Nothing runs until Serve.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	if err := ensureDirectories(cfg); err != nil {
		return nil, err
	}
	if err := checkBinaries(cfg); err != nil {
		return nil, err
	}
	st, err := store.Open(ctx, cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	a, err := build(cfg, st, log)
	if err != nil {
		st.Close()
		return nil, err
	}
	return a, nil
}

// ensureDirectories creates the data folders and checks the media folder exists.
func ensureDirectories(cfg config.Config) error {
	for _, dir := range []string{cfg.DataDir, cfg.PosterDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if _, err := os.Stat(cfg.MediaDir); err != nil {
		return fmt.Errorf("media folder: %w", err)
	}
	return nil
}

// checkBinaries makes sure ffmpeg and ffprobe can be run.
func checkBinaries(cfg config.Config) error {
	for _, bin := range []string{cfg.FFmpegPath, cfg.FFprobePath} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%s not found: install ffmpeg or set NEXUSPLAY_FFMPEG / NEXUSPLAY_FFPROBE", bin)
		}
	}
	return nil
}

// build wires the components together. Every dependency edge in the program
// is visible here.
func build(cfg config.Config, st *store.Store, log *slog.Logger) (*App, error) {
	tools := ffmpeg.Tools{FFmpeg: cfg.FFmpegPath, FFprobe: cfg.FFprobePath}
	meta := metadata.New(metadata.Config{APIKey: cfg.TMDBAPIKey, PosterDir: cfg.PosterDir()}, log.With("component", "tmdb"))
	scanner := library.NewScanner(
		library.Deps{Store: st, Prober: tools, Meta: meta, Log: log.With("component", "scanner")},
		library.Config{MediaDir: cfg.MediaDir, Interval: cfg.ScanInterval},
	)
	transcodes, err := stream.NewManager(stream.FFmpegStarter(tools),
		stream.Config{Dir: cfg.TranscodeDir(), Max: cfg.MaxTranscodes}, log.With("component", "transcode"))
	if err != nil {
		return nil, fmt.Errorf("transcode folder: %w", err)
	}
	accounts, err := auth.NewAccounts(st, log.With("component", "auth"))
	if err != nil {
		return nil, err
	}
	sessions := auth.NewSessions(st, sessionLifetime, log.With("component", "auth"))
	workers := []worker{scanner, transcodes, sessions}

	var conversions web.Conversions = convert.Disabled{}
	if cfg.Convert {
		conv := convert.New(
			convert.Deps{Store: st, Tools: tools, Library: scanner, Log: log.With("component", "convert")},
			convert.Config{MediaDir: cfg.MediaDir, Max: cfg.MaxConversions},
		)
		scanner.OnAdded(conv.Wake)
		conversions = conv
		workers = append(workers, conv)
	}

	srv := &web.Server{Cfg: cfg, Store: st, Scanner: scanner, Meta: meta, Transcodes: transcodes,
		Converter: conversions, Accounts: accounts, Sessions: sessions, Log: log}
	return &App{
		Cfg: cfg, Log: log, Store: st, Scanner: scanner, Transcodes: transcodes,
		Converter: conversions, Handler: srv.Handler(),
		workers: workers, shutdownTimeout: shutdownTimeout,
	}, nil
}

// Serve runs the HTTP server on ln and every background worker until ctx
// ends (or the server fails), then shuts down in order:
//
//  1. stop accepting requests and let those in flight finish (up to 10 s);
//  2. stop the workers and wait for them, up to the shutdown timeout;
//  3. close the database.
//
// If a worker is still running when the timeout expires, the database is
// left open and Serve returns anyway: exiting with SQLite's write-ahead log
// is safe, closing the database under a file swap is not.
func (a *App) Serve(ctx context.Context, ln net.Listener) error {
	workCtx, stopWorkers := context.WithCancel(context.Background())
	defer stopWorkers()
	var workers sync.WaitGroup
	for _, w := range a.workers {
		workers.Go(func() {
			if err := w.Run(workCtx); err != nil {
				a.Log.Error("background worker stopped", "worker", fmt.Sprintf("%T", w), "err", err)
			}
		})
	}

	srv := &http.Server{Handler: a.Handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	var err error
	select {
	case <-ctx.Done():
		a.Log.Info("shutting down")
	case err = <-served:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}

	httpCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if shErr := srv.Shutdown(httpCtx); shErr != nil {
		a.Log.Warn("requests still running at shutdown", "err", shErr)
	}
	cancel()

	stopWorkers()
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
		return errors.Join(err, a.Store.Close())
	case <-time.After(a.shutdownTimeout):
		a.Log.Error("background work did not stop in time; leaving the database open")
		return err
	}
}

// Close releases the database. Use it only when Serve was never called.
func (a *App) Close() error { return a.Store.Close() }

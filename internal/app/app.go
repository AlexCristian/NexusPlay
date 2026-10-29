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
	"github.com/AlexCristian/NexusPlay/internal/posters"
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
	_ posters.Store     = (*store.Store)(nil)
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
	Config     config.Config
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
func New(ctx context.Context, config config.Config, log *slog.Logger) (*App, error) {
	if err := ensureDirectories(config); err != nil {
		return nil, err
	}
	if err := checkBinaries(config); err != nil {
		return nil, err
	}
	database, err := store.Open(ctx, config.DatabasePath())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	application, err := build(config, database, log)
	if err != nil {
		database.Close()
		return nil, err
	}
	return application, nil
}

// ensureDirectories creates the data folders and checks the media folder exists.
func ensureDirectories(config config.Config) error {
	for _, directory := range []string{config.DataDirectory, config.PosterDirectory()} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", directory, err)
		}
	}
	if _, err := os.Stat(config.MediaDirectory); err != nil {
		return fmt.Errorf("media folder: %w", err)
	}
	return nil
}

// checkBinaries makes sure ffmpeg and ffprobe can be run.
func checkBinaries(config config.Config) error {
	for _, bin := range []string{config.FFmpegPath, config.FFprobePath} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%s not found: install ffmpeg or set NEXUSPLAY_FFMPEG / NEXUSPLAY_FFPROBE", bin)
		}
	}
	return nil
}

// build wires the components together. Every dependency edge in the program
// is visible here.
func build(config config.Config, database *store.Store, log *slog.Logger) (*App, error) {
	tools := ffmpeg.Tools{FFmpeg: config.FFmpegPath, FFprobe: config.FFprobePath}
	meta := metadata.New(metadata.Config{APIKey: config.TMDBAPIKey, PosterDirectory: config.PosterDirectory()}, log.With("component", "tmdb"))
	scanner := library.NewScanner(
		library.Deps{Store: database, Prober: tools, Meta: meta, Log: log.With("component", "scanner")},
		library.Config{MediaDirectory: config.MediaDirectory, Interval: config.ScanInterval},
	)
	transcodes, err := stream.NewManager(stream.FFmpegStarter(tools),
		stream.Config{Directory: config.TranscodeDirectory(), Max: config.MaxTranscodes}, log.With("component", "transcode"))
	if err != nil {
		return nil, fmt.Errorf("transcode folder: %w", err)
	}
	accounts, err := auth.NewAccounts(database, config.MaxAccounts, log.With("component", "auth"))
	if err != nil {
		return nil, err
	}
	sessions := auth.NewSessions(database, sessionLifetime, log.With("component", "auth"))
	sweeper := posters.NewSweeper(database, posters.Config{Directory: config.PosterDirectory()}, log.With("component", "posters"))
	workers := []worker{scanner, transcodes, sessions, sweeper}

	var conversions web.Conversions = convert.Disabled{}
	if config.Convert {
		conv := convert.New(
			convert.Deps{Store: database, Tools: tools, Library: scanner, Log: log.With("component", "convert")},
			convert.Config{MediaDirectory: config.MediaDirectory, Max: config.MaxConversions},
		)
		scanner.OnAdded(conv.Wake)
		conversions = conv
		workers = append(workers, conv)
	}

	srv := &web.Server{Config: config, Store: database, Scanner: scanner, Meta: meta, Transcodes: transcodes,
		Converter: conversions, Accounts: accounts, Sessions: sessions, Log: log}
	return &App{
		Config: config, Log: log, Store: database, Scanner: scanner, Transcodes: transcodes,
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
func (app *App) Serve(ctx context.Context, listener net.Listener) error {
	workCtx, stopWorkers := context.WithCancel(context.Background())
	defer stopWorkers()
	var workers sync.WaitGroup
	for _, worker := range app.workers {
		workers.Go(func() {
			if err := worker.Run(workCtx); err != nil {
				app.Log.Error("background worker stopped", "worker", fmt.Sprintf("%T", worker), "err", err)
			}
		})
	}

	srv := &http.Server{Handler: app.Handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	served := make(chan error, 1)
	go func() { served <- srv.Serve(listener) }()
	var err error
	select {
	case <-ctx.Done():
		app.Log.Info("shutting down")
	case err = <-served:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}

	httpCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if shErr := srv.Shutdown(httpCtx); shErr != nil {
		app.Log.Warn("requests still running at shutdown", "err", shErr)
	}
	cancel()

	stopWorkers()
	done := make(chan struct{})
	go func() { workers.Wait(); close(done) }()
	select {
	case <-done:
		return errors.Join(err, app.Store.Close())
	case <-time.After(app.shutdownTimeout):
		app.Log.Error("background work did not stop in time; leaving the database open")
		return err
	}
}

// Close releases the database. Use it only when Serve was never called.
func (app *App) Close() error { return app.Store.Close() }

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
	// [sessionLifetime] is how long a sign-in lasts: 30 days.
	sessionLifetime = 30 * 24 * time.Hour
	// [shutdownTimeout] is how long [App.Serve] waits for background work after
	// it is asked to stop. A file swap ignores cancellation and finishes.
	shutdownTimeout = 30 * time.Second
)

// [worker] is a background component that runs until its context ends: the
// scanner, transcode manager, session clean-up, poster sweeper and, when
// conversion is on, the converter.
type worker interface {
	// [Run] does the component's background work until [ctx] ends.
	//
	// Parameters:
	//   - [ctx]: stops the work when cancelled.
	//
	// Returns:
	//   - [error]: [nil] once stopped, or why it stopped early.
	Run(ctx context.Context) error
}

// [App] is a configured server with its background workers. Create one with
// [New], then call [App.Serve] once, or [App.Close] if it will not be
// served.
type App struct {
	// [Config] holds the settings it was built with.
	Config config.Config
	// [Log] records the server's and workers' messages.
	Log *slog.Logger
	// [Store] is the open database.
	Store *store.Store
	// [Scanner] is the library scanner; tests trigger scans through it.
	Scanner *library.Scanner
	// [Transcodes] runs the HLS transcodes.
	Transcodes *stream.Manager
	// [Converter] is the MP4 converter, or [convert.Disabled] when
	// NEXUSPLAY_CONVERT is off.
	Converter web.Conversions
	// [Handler] is the complete HTTP handler, which tests can call directly.
	Handler http.Handler

	// [workers] are the background components [App.Serve] runs.
	workers []worker
	// [shutdownTimeout] is how long [App.Serve] waits for the workers to stop;
	// tests shorten it.
	shutdownTimeout time.Duration
}

// [New] checks the environment, opens the database (applying migrations) and
// builds every component. Nothing runs until [App.Serve].
//
// Parameters:
//   - [ctx]: cancels opening the database.
//   - [config]: the validated settings.
//   - [log]: where every component logs.
//
// Returns:
//   - [*App]: the application, or [nil] if [error] is not [nil].
//   - [error]: [nil] on success, or why it could not start: a data folder
//     that cannot be created, a missing media folder, ffmpeg or ffprobe not
//     found, a database that cannot be opened, or a component that cannot
//     be built.
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

// [ensureDirectories] creates the data and poster folders if needed, and
// checks that the media folder exists.
//
// Parameters:
//   - [config]: the settings naming the folders.
//
// Returns:
//   - [error]: [nil] if the folders are ready, or an error naming the one
//     that is not.
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

// [checkBinaries] makes sure ffmpeg and ffprobe can be found, on the PATH or
// at the configured paths.
//
// Parameters:
//   - [config]: the settings naming the programs.
//
// Returns:
//   - [error]: [nil] if both are found, or an error naming the first one
//     that is not and how to fix it.
func checkBinaries(config config.Config) error {
	for _, program := range []string{config.FFmpegPath, config.FFprobePath} {
		if _, err := exec.LookPath(program); err != nil {
			return fmt.Errorf("%s not found: install ffmpeg or set NEXUSPLAY_FFMPEG / NEXUSPLAY_FFPROBE", program)
		}
	}
	return nil
}

// [build] wires the components together. Every dependency edge in the
// program is visible here.
//
// Parameters:
//   - [config]: the validated settings.
//   - [database]: the open database.
//   - [log]: where every component logs; each gets its own "component"
//     attribute.
//
// Returns:
//   - [*App]: the application, or [nil] if [error] is not [nil].
//   - [error]: [nil] on success; an error if the transcode folder cannot be
//     prepared or the system's random source is broken.
func build(config config.Config, database *store.Store, log *slog.Logger) (*App, error) {
	tools := ffmpeg.Tools{FFmpeg: config.FFmpegPath, FFprobe: config.FFprobePath}
	tmdbClient := metadata.New(metadata.Config{APIKey: config.TMDBAPIKey, PosterDirectory: config.PosterDirectory()}, log.With("component", "tmdb"))
	scanner := library.NewScanner(
		library.Dependencies{Store: database, Prober: tools, Metadata: tmdbClient, Log: log.With("component", "scanner")},
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
		converter := convert.New(
			convert.Dependencies{Store: database, Tools: tools, Library: scanner, Log: log.With("component", "convert")},
			convert.Config{MediaDirectory: config.MediaDirectory, Max: config.MaxConversions},
		)
		scanner.OnAdded(converter.Wake)
		conversions = converter
		workers = append(workers, converter)
	}

	webServer := &web.Server{Config: config, Store: database, Scanner: scanner, Metadata: tmdbClient, Transcodes: transcodes,
		Converter: conversions, Accounts: accounts, Sessions: sessions, Log: log}
	return &App{
		Config: config, Log: log, Store: database, Scanner: scanner, Transcodes: transcodes,
		Converter: conversions, Handler: webServer.Handler(),
		workers: workers, shutdownTimeout: shutdownTimeout,
	}, nil
}

// [Serve] runs the HTTP server on a listener and every background worker
// until [ctx] ends (or the server fails), then shuts down in order:
//
//  1. stop accepting requests and let those in flight finish, for up to 10
//     seconds;
//  2. stop the workers and wait for them, up to the shutdown timeout;
//  3. close the database.
//
// If a worker is still running when the timeout expires, the database is
// left open and [Serve] returns anyway: exiting with SQLite's write-ahead
// log is safe, closing the database under a file swap is not. A worker that
// stops with an error is logged. Call [Serve] once.
//
// Parameters:
//   - [ctx]: starts the shutdown when cancelled.
//   - [listener]: where to accept HTTP connections; it is closed on
//     shutdown.
//
// Returns:
//   - [error]: [nil] after a clean shutdown; otherwise the server's error
//     (if it failed before [ctx] ended) joined with the error from closing
//     the database.
func (app *App) Serve(ctx context.Context, listener net.Listener) error {
	workContext, stopWorkers := context.WithCancel(context.Background())
	defer stopWorkers()
	var workers sync.WaitGroup
	for _, worker := range app.workers {
		workers.Go(func() {
			if err := worker.Run(workContext); err != nil {
				app.Log.Error("background worker stopped", "worker", fmt.Sprintf("%T", worker), "err", err)
			}
		})
	}

	httpServer := &http.Server{Handler: app.Handler, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	served := make(chan error, 1)
	go func() { served <- httpServer.Serve(listener) }()
	var err error
	select {
	case <-ctx.Done():
		app.Log.Info("shutting down")
	case err = <-served:
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
	}

	httpContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	if shutdownErr := httpServer.Shutdown(httpContext); shutdownErr != nil {
		app.Log.Warn("requests still running at shutdown", "err", shutdownErr)
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

// [Close] releases the database. Use it only when [App.Serve] was never
// called, since [App.Serve] closes it itself.
//
// Returns:
//   - [error]: [nil] on success, or the error from closing the database.
func (app *App) Close() error { return app.Store.Close() }

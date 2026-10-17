// Package app wires NexusPlay's parts together. The command and the
// integration tests both start the server through it.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
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

// sessionLifetime is how long a login lasts.
const sessionLifetime = 30 * 24 * time.Hour

// App is a configured server with its background workers.
type App struct {
	Cfg        config.Config
	Log        *slog.Logger
	Store      *store.Store
	Scanner    *library.Scanner
	Transcodes *stream.Manager
	Converter  web.Conversions    // convert.Disabled when NEXUSPLAY_CONVERT is off
	runner     *convert.Converter // nil when conversion is off
	sessions   *auth.Sessions
	Handler    http.Handler
}

// New checks the environment, opens the database and builds the HTTP handler.
func New(ctx context.Context, cfg config.Config, log *slog.Logger) (*App, error) {
	for _, dir := range []string{cfg.DataDir, cfg.PosterDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if _, err := os.Stat(cfg.MediaDir); err != nil {
		return nil, fmt.Errorf("media folder: %w", err)
	}
	for _, bin := range []string{cfg.FFmpegPath, cfg.FFprobePath} {
		if _, err := exec.LookPath(bin); err != nil {
			return nil, fmt.Errorf("%s not found: install ffmpeg or set NEXUSPLAY_FFMPEG / NEXUSPLAY_FFPROBE", bin)
		}
	}

	st, err := store.Open(ctx, cfg.DBPath())
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	tools := ffmpeg.Tools{FFmpeg: cfg.FFmpegPath, FFprobe: cfg.FFprobePath}
	meta := metadata.New(metadata.Config{APIKey: cfg.TMDBAPIKey, PosterDir: cfg.PosterDir()}, log.With("component", "tmdb"))
	scanner := library.NewScanner(
		library.Deps{Store: st, Prober: tools, Meta: meta, Log: log.With("component", "scanner")},
		library.Config{MediaDir: cfg.MediaDir, Interval: cfg.ScanInterval},
	)
	transcodes, err := stream.NewManager(stream.FFmpegStarter(tools),
		stream.Config{Dir: cfg.TranscodeDir(), Max: cfg.MaxTranscodes}, log.With("component", "transcode"))
	if err != nil {
		st.Close()
		return nil, fmt.Errorf("transcode folder: %w", err)
	}
	var conversions web.Conversions = convert.Disabled{}
	var runner *convert.Converter
	if cfg.Convert {
		runner = convert.New(
			convert.Deps{Store: st, Tools: tools, Library: scanner, Log: log.With("component", "convert")},
			convert.Config{MediaDir: cfg.MediaDir, Max: cfg.MaxConversions},
		)
		scanner.OnAdded(runner.Wake)
		conversions = runner
	}
	accounts, err := auth.NewAccounts(st, log.With("component", "auth"))
	if err != nil {
		st.Close()
		return nil, err
	}
	sessions := auth.NewSessions(st, sessionLifetime, log.With("component", "auth"))
	srv := &web.Server{Cfg: cfg, Store: st, Scanner: scanner, Meta: meta, Transcodes: transcodes,
		Converter: conversions, Accounts: accounts, Sessions: sessions, Log: log}
	return &App{Cfg: cfg, Log: log, Store: st, Scanner: scanner, Transcodes: transcodes,
		Converter: conversions, runner: runner, sessions: sessions, Handler: srv.Handler()}, nil
}

// Start runs the scanner, the transcode janitor, the converter and session
// cleanup until ctx ends.
func (a *App) Start(ctx context.Context) {
	go a.Scanner.Run(ctx)
	go a.Transcodes.Run(ctx)
	if a.runner != nil {
		go a.runner.Run(ctx)
	}
	go a.sessions.Run(ctx)

}

// Close releases the database.
func (a *App) Close() error { return a.Store.Close() }

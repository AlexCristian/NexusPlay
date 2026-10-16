// Command nexusplay runs the NexusPlay media server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/config"
	"github.com/AlexCristian/NexusPlay/internal/library"
	"github.com/AlexCristian/NexusPlay/internal/metadata"
	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/stream"
	"github.com/AlexCristian/NexusPlay/internal/web"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	level := slog.LevelInfo
	if os.Getenv("NEXUSPLAY_DEBUG") != "" {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	for _, dir := range []string{cfg.DataDir, cfg.PosterDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	if _, err := os.Stat(cfg.MediaDir); err != nil {
		return fmt.Errorf("media folder: %w", err)
	}
	for _, bin := range []string{cfg.FFmpegPath, cfg.FFprobePath} {
		if _, err := exec.LookPath(bin); err != nil {
			return fmt.Errorf("%s not found: install ffmpeg or set NEXUSPLAY_FFMPEG / NEXUSPLAY_FFPROBE", bin)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(ctx, cfg.DBPath())
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer st.Close()

	meta := metadata.New(cfg.TMDBAPIKey, cfg.PosterDir())
	scanner := &library.Scanner{
		Store: st, Prober: library.Prober{Path: cfg.FFprobePath}, Meta: meta,
		MediaDir: cfg.MediaDir, Interval: cfg.ScanInterval, Log: log.With("component", "scanner"),
	}
	transcodes, err := stream.NewManager(cfg.FFmpegPath, cfg.TranscodeDir(), cfg.MaxTranscodes, log.With("component", "transcode"))
	if err != nil {
		return fmt.Errorf("transcode folder: %w", err)
	}

	srv := &web.Server{Cfg: cfg, Store: st, Scanner: scanner, Meta: meta, Transcodes: transcodes, Log: log}
	httpServer := &http.Server{
		Addr:              cfg.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	go scanner.Run(ctx)
	go transcodes.Janitor(ctx)
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := st.DeleteExpiredSessions(ctx); err != nil {
					log.Warn("session cleanup", "err", err)
				}
			}
		}
	}()

	errc := make(chan error, 1)
	go func() {
		log.Info("NexusPlay listening", "addr", cfg.Addr, "media", cfg.MediaDir, "data", cfg.DataDir, "tmdb", meta.Enabled())
		errc <- httpServer.ListenAndServe()
	}()
	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		log.Info("shutting down")
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

// healthcheck lets a distroless or minimal container check itself without curl.
func healthcheck() int {
	addr := os.Getenv("NEXUSPLAY_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	c := &http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get("http://" + addr + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, resp.Status)
		return 1
	}
	return 0
}

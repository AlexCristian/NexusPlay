// Command nexusplay runs the NexusPlay media server. It reads the
// configuration from NEXUSPLAY_* environment variables (see package config),
// then hands everything else to package app.
//
// "nexusplay healthcheck" asks a running server's /healthz and exits 0 if it
// answers; the container image uses it because it has no curl.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/app"
	"github.com/AlexCristian/NexusPlay/internal/config"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	configuration, err := config.Load()
	if err != nil {
		return err
	}
	level := slog.LevelInfo
	if configuration.Debug {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	application, err := app.New(ctx, configuration, log)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", configuration.Addr)
	if err != nil {
		application.Close()
		return err
	}
	log.Info("NexusPlay listening", "addr", listener.Addr().String(), "media", configuration.MediaDirectory, "data", configuration.DataDirectory, "tmdb", configuration.TMDBAPIKey != "")
	return application.Serve(ctx, listener)
}

// healthcheck lets a minimal container check itself without curl.
func healthcheck() int {
	addr := os.Getenv("NEXUSPLAY_ADDR")
	if addr == "" {
		addr = config.Defaults().Addr
	}
	if addr[0] == ':' {
		addr = "127.0.0.1" + addr
	}
	httpClient := &http.Client{Timeout: 3 * time.Second}
	response, err := httpClient.Get("http://" + addr + "/healthz")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	response.Body.Close()
	if response.StatusCode != http.StatusOK {
		fmt.Fprintln(os.Stderr, response.Status)
		return 1
	}
	return 0
}

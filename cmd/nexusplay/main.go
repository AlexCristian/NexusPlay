// Command [nexusplay] runs the NexusPlay media server. It reads the
// configuration from NEXUSPLAY_* environment variables (see package config),
// then hands everything else to package app.
//
// "nexusplay healthcheck" asks a running server's /healthz and exits 0 if it
// answers 200 OK; the container image uses it because it has no curl.
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

// [main] runs the health check when called as "nexusplay healthcheck", and
// the server otherwise. It exits with status 1 if the server fails to start
// or stops with an error, after logging why.
func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		os.Exit(healthcheck())
	}
	if err := run(); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

// [run] loads the configuration, sets up logging (debug level when
// NEXUSPLAY_DEBUG is on), and serves until the process receives SIGINT or
// SIGTERM, then shuts down cleanly.
//
// Returns:
//   - [error]: [nil] after a clean shutdown; otherwise a configuration error,
//     an error from building the application, an error listening on
//     NEXUSPLAY_ADDR, or the server's error.
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
	listener, err := net.Listen("tcp", configuration.Address)
	if err != nil {
		application.Close()
		return err
	}
	log.Info("NexusPlay listening", "addr", listener.Addr().String(), "media", configuration.MediaDirectory, "data", configuration.DataDirectory, "tmdb", configuration.TMDBAPIKey != "")
	return application.Serve(ctx, listener)
}

// [healthcheck] lets a minimal container check itself without curl: it asks
// the server at NEXUSPLAY_ADDR (or the default address) for /healthz,
// waiting at most 3 seconds. An address without a host, such as ":8080",
// is reached on 127.0.0.1. Problems are printed to stderr.
//
// Returns:
//   - [int]: the exit status: 0 if the server answered 200, 1 otherwise.
func healthcheck() int {
	address := os.Getenv("NEXUSPLAY_ADDR")
	if address == "" {
		address = config.Defaults().Address
	}
	if address[0] == ':' {
		address = "127.0.0.1" + address
	}
	httpClient := &http.Client{Timeout: 3 * time.Second}
	response, err := httpClient.Get("http://" + address + "/healthz")
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

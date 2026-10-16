// Package config loads NexusPlay's settings from environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Config holds every runtime setting. Nothing is compiled in: each field has
// a default and can be overridden with the NEXUSPLAY_* variable named beside it.
type Config struct {
	Addr          string        // NEXUSPLAY_ADDR
	MediaDir      string        // NEXUSPLAY_MEDIA_DIR
	DataDir       string        // NEXUSPLAY_DATA_DIR
	ScanInterval  time.Duration // NEXUSPLAY_SCAN_INTERVAL
	TMDBAPIKey    string        // NEXUSPLAY_TMDB_API_KEY
	VotesRequired int           // NEXUSPLAY_VOTES_REQUIRED
	MaxTranscodes int           // NEXUSPLAY_MAX_TRANSCODES
	RequireLogin  bool          // NEXUSPLAY_REQUIRE_LOGIN
	SecureCookies bool          // NEXUSPLAY_SECURE_COOKIES
	FFmpegPath    string        // NEXUSPLAY_FFMPEG
	FFprobePath   string        // NEXUSPLAY_FFPROBE
}

// Load reads the configuration from the environment.
func Load() (Config, error) {
	c := Config{
		Addr:          env("NEXUSPLAY_ADDR", ":8080"),
		MediaDir:      env("NEXUSPLAY_MEDIA_DIR", "./media"),
		DataDir:       env("NEXUSPLAY_DATA_DIR", "./data"),
		TMDBAPIKey:    env("NEXUSPLAY_TMDB_API_KEY", ""),
		FFmpegPath:    env("NEXUSPLAY_FFMPEG", "ffmpeg"),
		FFprobePath:   env("NEXUSPLAY_FFPROBE", "ffprobe"),
		ScanInterval:  5 * time.Minute,
		VotesRequired: 4,
		MaxTranscodes: 2,
	}
	var err error
	if v := os.Getenv("NEXUSPLAY_SCAN_INTERVAL"); v != "" {
		if c.ScanInterval, err = time.ParseDuration(v); err != nil {
			return c, fmt.Errorf("NEXUSPLAY_SCAN_INTERVAL: %w", err)
		}
	}
	if c.VotesRequired, err = envInt("NEXUSPLAY_VOTES_REQUIRED", c.VotesRequired); err != nil {
		return c, err
	}
	if c.MaxTranscodes, err = envInt("NEXUSPLAY_MAX_TRANSCODES", c.MaxTranscodes); err != nil {
		return c, err
	}
	if c.RequireLogin, err = envBool("NEXUSPLAY_REQUIRE_LOGIN", false); err != nil {
		return c, err
	}
	if c.SecureCookies, err = envBool("NEXUSPLAY_SECURE_COOKIES", false); err != nil {
		return c, err
	}
	if c.VotesRequired < 1 {
		return c, fmt.Errorf("NEXUSPLAY_VOTES_REQUIRED must be at least 1")
	}
	if c.MaxTranscodes < 1 {
		return c, fmt.Errorf("NEXUSPLAY_MAX_TRANSCODES must be at least 1")
	}
	if c.MediaDir, err = filepath.Abs(c.MediaDir); err != nil {
		return c, err
	}
	if c.DataDir, err = filepath.Abs(c.DataDir); err != nil {
		return c, err
	}
	return c, nil
}

// PosterDir is where downloaded and uploaded posters are stored.
func (c Config) PosterDir() string { return filepath.Join(c.DataDir, "posters") }

// TranscodeDir is the scratch space for HLS output.
func (c Config) TranscodeDir() string { return filepath.Join(c.DataDir, "transcode") }

// DBPath is the SQLite database file.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "nexusplay.db") }

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) (int, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return n, nil
}

func envBool(key string, def bool) (bool, error) {
	v := os.Getenv(key)
	if v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}

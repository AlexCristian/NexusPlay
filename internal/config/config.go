// Package config loads NexusPlay's settings from environment variables. It
// owns the variable names and defaults; nothing else reads the environment.
package config

import (
	"errors"
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
	MediaDir      string        // NEXUSPLAY_MEDIA_DIR (made absolute)
	DataDir       string        // NEXUSPLAY_DATA_DIR (made absolute)
	ScanInterval  time.Duration // NEXUSPLAY_SCAN_INTERVAL
	TMDBAPIKey    string        // NEXUSPLAY_TMDB_API_KEY
	VotesRequired int           // NEXUSPLAY_VOTES_REQUIRED
	MaxTranscodes int           // NEXUSPLAY_MAX_TRANSCODES
	RequireLogin  bool          // NEXUSPLAY_REQUIRE_LOGIN
	SecureCookies bool          // NEXUSPLAY_SECURE_COOKIES
	FFmpegPath    string        // NEXUSPLAY_FFMPEG
	FFprobePath   string        // NEXUSPLAY_FFPROBE

	// Convert turns every file that is not a browser-ready MP4 into one in
	// the background, then deletes the original.
	Convert        bool // NEXUSPLAY_CONVERT
	MaxConversions int  // NEXUSPLAY_MAX_CONVERSIONS
}

// Defaults returns the configuration used when no variable is set.
func Defaults() Config {
	return Config{
		Addr:           ":8080",
		MediaDir:       "./media",
		DataDir:        "./data",
		ScanInterval:   5 * time.Minute,
		VotesRequired:  4,
		MaxTranscodes:  2,
		FFmpegPath:     "ffmpeg",
		FFprobePath:    "ffprobe",
		Convert:        true,
		MaxConversions: 1,
	}
}

// Load reads the configuration from the process environment.
func Load() (Config, error) { return LoadFrom(os.Getenv) }

// LoadFrom reads the configuration from getenv (os.Getenv, or a map in
// tests), applies defaults, and validates it. It reports every bad variable
// at once rather than stopping at the first.
func LoadFrom(getenv func(string) string) (Config, error) {
	c := Defaults()
	var errs []error
	str := func(key string, dst *string) {
		if v := getenv(key); v != "" {
			*dst = v
		}
	}
	num := func(key string, dst *int) {
		if v := getenv(key); v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %q is not a whole number", key, v))
				return
			}
			*dst = n
		}
	}
	flag := func(key string, dst *bool) {
		if v := getenv(key); v != "" {
			b, err := strconv.ParseBool(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %q is not true or false", key, v))
				return
			}
			*dst = b
		}
	}
	duration := func(key string, dst *time.Duration) {
		if v := getenv(key); v != "" {
			d, err := time.ParseDuration(v)
			if err != nil {
				errs = append(errs, fmt.Errorf("%s: %q is not a duration such as 5m or 1h", key, v))
				return
			}
			*dst = d
		}
	}

	str("NEXUSPLAY_ADDR", &c.Addr)
	str("NEXUSPLAY_MEDIA_DIR", &c.MediaDir)
	str("NEXUSPLAY_DATA_DIR", &c.DataDir)
	str("NEXUSPLAY_TMDB_API_KEY", &c.TMDBAPIKey)
	str("NEXUSPLAY_FFMPEG", &c.FFmpegPath)
	str("NEXUSPLAY_FFPROBE", &c.FFprobePath)
	duration("NEXUSPLAY_SCAN_INTERVAL", &c.ScanInterval)
	num("NEXUSPLAY_VOTES_REQUIRED", &c.VotesRequired)
	num("NEXUSPLAY_MAX_TRANSCODES", &c.MaxTranscodes)
	num("NEXUSPLAY_MAX_CONVERSIONS", &c.MaxConversions)
	flag("NEXUSPLAY_CONVERT", &c.Convert)
	flag("NEXUSPLAY_REQUIRE_LOGIN", &c.RequireLogin)
	flag("NEXUSPLAY_SECURE_COOKIES", &c.SecureCookies)

	errs = append(errs, c.Validate())
	if err := errors.Join(errs...); err != nil {
		return c, err
	}
	var err error
	if c.MediaDir, err = filepath.Abs(c.MediaDir); err != nil {
		return c, err
	}
	if c.DataDir, err = filepath.Abs(c.DataDir); err != nil {
		return c, err
	}
	return c, nil
}

// Validate reports every setting that is out of range.
func (c Config) Validate() error {
	var errs []error
	atLeastOne := func(key string, n int) {
		if n < 1 {
			errs = append(errs, fmt.Errorf("%s must be at least 1, not %d", key, n))
		}
	}
	atLeastOne("NEXUSPLAY_VOTES_REQUIRED", c.VotesRequired)
	atLeastOne("NEXUSPLAY_MAX_TRANSCODES", c.MaxTranscodes)
	atLeastOne("NEXUSPLAY_MAX_CONVERSIONS", c.MaxConversions)
	// Changes are picked up within seconds by the file watcher; the interval
	// is only the safety net, and walking a big library too often wastes disk I/O.
	if c.ScanInterval < 10*time.Second {
		errs = append(errs, fmt.Errorf("NEXUSPLAY_SCAN_INTERVAL must be at least 10s, not %s", c.ScanInterval))
	}
	if c.Addr == "" {
		errs = append(errs, errors.New("NEXUSPLAY_ADDR must not be empty"))
	}
	return errors.Join(errs...)
}

// PosterDir is where downloaded and uploaded posters are stored.
func (c Config) PosterDir() string { return filepath.Join(c.DataDir, "posters") }

// TranscodeDir is the scratch space for HLS output.
func (c Config) TranscodeDir() string { return filepath.Join(c.DataDir, "transcode") }

// DBPath is the SQLite database file.
func (c Config) DBPath() string { return filepath.Join(c.DataDir, "nexusplay.db") }

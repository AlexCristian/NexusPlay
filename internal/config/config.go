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
	MaxAccounts   int           // NEXUSPLAY_MAX_ACCOUNTS: 0 for no limit
	SecureCookies bool          // NEXUSPLAY_SECURE_COOKIES
	FFmpegPath    string        // NEXUSPLAY_FFMPEG
	FFprobePath   string        // NEXUSPLAY_FFPROBE
	Debug         bool          // NEXUSPLAY_DEBUG: log every request

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
		MaxAccounts:    10,
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
	for _, v := range c.variables() {
		if s := getenv(v.name); s != "" {
			if err := v.set(s); err != nil {
				errs = append(errs, fmt.Errorf("%s: %q %v", v.name, s, err))
			}
		}
	}
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

// variable is one NEXUSPLAY_* setting: its name, and how to parse a value
// into its field.
type variable struct {
	name string
	set  func(string) error
}

// variables lists every setting read from the environment.
func (c *Config) variables() []variable {
	return []variable{
		{"NEXUSPLAY_ADDR", text(&c.Addr)},
		{"NEXUSPLAY_MEDIA_DIR", text(&c.MediaDir)},
		{"NEXUSPLAY_DATA_DIR", text(&c.DataDir)},
		{"NEXUSPLAY_TMDB_API_KEY", text(&c.TMDBAPIKey)},
		{"NEXUSPLAY_FFMPEG", text(&c.FFmpegPath)},
		{"NEXUSPLAY_FFPROBE", text(&c.FFprobePath)},
		{"NEXUSPLAY_SCAN_INTERVAL", duration(&c.ScanInterval)},
		{"NEXUSPLAY_VOTES_REQUIRED", number(&c.VotesRequired)},
		{"NEXUSPLAY_MAX_TRANSCODES", number(&c.MaxTranscodes)},
		{"NEXUSPLAY_MAX_CONVERSIONS", number(&c.MaxConversions)},
		{"NEXUSPLAY_MAX_ACCOUNTS", number(&c.MaxAccounts)},
		{"NEXUSPLAY_CONVERT", flag(&c.Convert)},
		{"NEXUSPLAY_REQUIRE_LOGIN", flag(&c.RequireLogin)},
		{"NEXUSPLAY_SECURE_COOKIES", flag(&c.SecureCookies)},
		{"NEXUSPLAY_DEBUG", flag(&c.Debug)},
	}
}

func text(dst *string) func(string) error {
	return func(v string) error { *dst = v; return nil }
}

func number(dst *int) func(string) error {
	return func(v string) error {
		n, err := strconv.Atoi(v)
		if err != nil {
			return errors.New("is not a whole number")
		}
		*dst = n
		return nil
	}
}

func flag(dst *bool) func(string) error {
	return func(v string) error {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return errors.New("is not true or false")
		}
		*dst = b
		return nil
	}
}

func duration(dst *time.Duration) func(string) error {
	return func(v string) error {
		d, err := time.ParseDuration(v)
		if err != nil {
			return errors.New("is not a duration such as 5m or 1h")
		}
		*dst = d
		return nil
	}
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
	if c.MaxAccounts < 0 {
		errs = append(errs, fmt.Errorf("NEXUSPLAY_MAX_ACCOUNTS must be 0 (no limit) or more, not %d", c.MaxAccounts))
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

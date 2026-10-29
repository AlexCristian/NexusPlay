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
	Address        string        // NEXUSPLAY_ADDR
	MediaDirectory string        // NEXUSPLAY_MEDIA_DIR (made absolute)
	DataDirectory  string        // NEXUSPLAY_DATA_DIR (made absolute)
	ScanInterval   time.Duration // NEXUSPLAY_SCAN_INTERVAL
	TMDBAPIKey     string        // NEXUSPLAY_TMDB_API_KEY
	VotesRequired  int           // NEXUSPLAY_VOTES_REQUIRED
	MaxTranscodes  int           // NEXUSPLAY_MAX_TRANSCODES
	RequireLogin   bool          // NEXUSPLAY_REQUIRE_LOGIN
	MaxAccounts    int           // NEXUSPLAY_MAX_ACCOUNTS: 0 for no limit
	SecureCookies  bool          // NEXUSPLAY_SECURE_COOKIES
	FFmpegPath     string        // NEXUSPLAY_FFMPEG
	FFprobePath    string        // NEXUSPLAY_FFPROBE
	Debug          bool          // NEXUSPLAY_DEBUG: log every request

	// Convert turns every file that is not a browser-ready MP4 into one in
	// the background, then deletes the original.
	Convert        bool // NEXUSPLAY_CONVERT
	MaxConversions int  // NEXUSPLAY_MAX_CONVERSIONS
}

// Defaults returns the configuration used when no variable is set.
func Defaults() Config {
	return Config{
		Address:        ":8080",
		MediaDirectory: "./media",
		DataDirectory:  "./data",
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
	config := Defaults()
	var errs []error
	for _, variable := range config.variables() {
		if value := getenv(variable.name); value != "" {
			if err := variable.set(value); err != nil {
				errs = append(errs, fmt.Errorf("%s: %q %v", variable.name, value, err))
			}
		}
	}
	errs = append(errs, config.Validate())
	if err := errors.Join(errs...); err != nil {
		return config, err
	}
	var err error
	if config.MediaDirectory, err = filepath.Abs(config.MediaDirectory); err != nil {
		return config, err
	}
	if config.DataDirectory, err = filepath.Abs(config.DataDirectory); err != nil {
		return config, err
	}
	return config, nil
}

// variable is one NEXUSPLAY_* setting: its name, and how to parse a value
// into its field.
type variable struct {
	name string
	set  func(string) error
}

// variables lists every setting read from the environment.
func (config *Config) variables() []variable {
	return []variable{
		{"NEXUSPLAY_ADDR", text(&config.Address)},
		{"NEXUSPLAY_MEDIA_DIR", text(&config.MediaDirectory)},
		{"NEXUSPLAY_DATA_DIR", text(&config.DataDirectory)},
		{"NEXUSPLAY_TMDB_API_KEY", text(&config.TMDBAPIKey)},
		{"NEXUSPLAY_FFMPEG", text(&config.FFmpegPath)},
		{"NEXUSPLAY_FFPROBE", text(&config.FFprobePath)},
		{"NEXUSPLAY_SCAN_INTERVAL", duration(&config.ScanInterval)},
		{"NEXUSPLAY_VOTES_REQUIRED", number(&config.VotesRequired)},
		{"NEXUSPLAY_MAX_TRANSCODES", number(&config.MaxTranscodes)},
		{"NEXUSPLAY_MAX_CONVERSIONS", number(&config.MaxConversions)},
		{"NEXUSPLAY_MAX_ACCOUNTS", number(&config.MaxAccounts)},
		{"NEXUSPLAY_CONVERT", flag(&config.Convert)},
		{"NEXUSPLAY_REQUIRE_LOGIN", flag(&config.RequireLogin)},
		{"NEXUSPLAY_SECURE_COOKIES", flag(&config.SecureCookies)},
		{"NEXUSPLAY_DEBUG", flag(&config.Debug)},
	}
}

func text(destination *string) func(string) error {
	return func(value string) error { *destination = value; return nil }
}

func number(destination *int) func(string) error {
	return func(value string) error {
		number, err := strconv.Atoi(value)
		if err != nil {
			return errors.New("is not a whole number")
		}
		*destination = number
		return nil
	}
}

func flag(destination *bool) func(string) error {
	return func(value string) error {
		enabled, err := strconv.ParseBool(value)
		if err != nil {
			return errors.New("is not true or false")
		}
		*destination = enabled
		return nil
	}
}

func duration(destination *time.Duration) func(string) error {
	return func(value string) error {
		duration, err := time.ParseDuration(value)
		if err != nil {
			return errors.New("is not a duration such as 5m or 1h")
		}
		*destination = duration
		return nil
	}
}

// Validate reports every setting that is out of range.
func (config Config) Validate() error {
	var errs []error
	atLeastOne := func(key string, count int) {
		if count < 1 {
			errs = append(errs, fmt.Errorf("%s must be at least 1, not %d", key, count))
		}
	}
	atLeastOne("NEXUSPLAY_VOTES_REQUIRED", config.VotesRequired)
	atLeastOne("NEXUSPLAY_MAX_TRANSCODES", config.MaxTranscodes)
	atLeastOne("NEXUSPLAY_MAX_CONVERSIONS", config.MaxConversions)
	// Changes are picked up within seconds by the file watcher; the interval
	// is only the safety net, and walking a big library too often wastes disk I/O.
	if config.ScanInterval < 10*time.Second {
		errs = append(errs, fmt.Errorf("NEXUSPLAY_SCAN_INTERVAL must be at least 10s, not %s", config.ScanInterval))
	}
	if config.MaxAccounts < 0 {
		errs = append(errs, fmt.Errorf("NEXUSPLAY_MAX_ACCOUNTS must be 0 (no limit) or more, not %d", config.MaxAccounts))
	}
	if config.Address == "" {
		errs = append(errs, errors.New("NEXUSPLAY_ADDR must not be empty"))
	}
	return errors.Join(errs...)
}

// PosterDirectory is where downloaded and uploaded posters are stored.
func (config Config) PosterDirectory() string { return filepath.Join(config.DataDirectory, "posters") }

// TranscodeDirectory is the scratch space for HLS output.
func (config Config) TranscodeDirectory() string {
	return filepath.Join(config.DataDirectory, "transcode")
}

// DatabasePath is the SQLite database file.
func (config Config) DatabasePath() string {
	return filepath.Join(config.DataDirectory, "nexusplay.db")
}

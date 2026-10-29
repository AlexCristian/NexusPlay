package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// [Config] holds every runtime setting. Nothing is compiled in: each field
// has a default from [Defaults] and can be overridden with the NEXUSPLAY_*
// variable named in its comment. Build one with [Load] or [LoadFrom], which
// also validate it.
type Config struct {
	// [Address] is the host and port the web server listens on, such as ":8080"
	// (NEXUSPLAY_ADDR).
	Address string
	// [MediaDirectory] is the folder of videos the library is built from
	// (NEXUSPLAY_MEDIA_DIR). [LoadFrom] makes it absolute.
	MediaDirectory string
	// [DataDirectory] is the folder NexusPlay writes to: the database, posters
	// and transcodes (NEXUSPLAY_DATA_DIR). [LoadFrom] makes it absolute.
	DataDirectory string
	// [ScanInterval] is how often the media folder is rescanned in full, at
	// least 10 seconds (NEXUSPLAY_SCAN_INTERVAL, such as "5m"). The file watcher
	// catches most changes sooner; this is the safety net.
	ScanInterval time.Duration
	// [TMDBAPIKey] is the TMDB v3 API key or v4 read access token used to look up
	// titles and posters, or "" to turn lookups off (NEXUSPLAY_TMDB_API_KEY).
	TMDBAPIKey string
	// [VotesRequired] is how many viewers must vote for a subtitle correction
	// before it replaces the line, at least 1 (NEXUSPLAY_VOTES_REQUIRED).
	VotesRequired int
	// [MaxTranscodes] is how many on-the-fly HLS transcodes may run at once, at
	// least 1 (NEXUSPLAY_MAX_TRANSCODES).
	MaxTranscodes int
	// [RequireLogin] makes every page need a signed-in account; when [false],
	// browsing and watching are public (NEXUSPLAY_REQUIRE_LOGIN).
	RequireLogin bool
	// [MaxAccounts] is how many accounts may be registered, or 0 for no limit
	// (NEXUSPLAY_MAX_ACCOUNTS).
	MaxAccounts int
	// [SecureCookies] marks session cookies Secure, so browsers send them only
	// over HTTPS; turn it on behind an HTTPS proxy (NEXUSPLAY_SECURE_COOKIES).
	SecureCookies bool
	// [FFmpegPath] is the ffmpeg program to run: a name looked up on the PATH,
	// or a file path (NEXUSPLAY_FFMPEG).
	FFmpegPath string
	// [FFprobePath] is the ffprobe program to run: a name looked up on the PATH,
	// or a file path (NEXUSPLAY_FFPROBE).
	FFprobePath string
	// [Debug] turns on debug-level logging, which includes a line for every
	// HTTP request (NEXUSPLAY_DEBUG).
	Debug bool

	// [Convert] turns every file that is not a browser-ready MP4 into one in the
	// background, then deletes the original (NEXUSPLAY_CONVERT).
	Convert bool
	// [MaxConversions] is how many background conversions may run at once, at
	// least 1 (NEXUSPLAY_MAX_CONVERSIONS).
	MaxConversions int
}

// [Defaults] gives the settings used when no NEXUSPLAY_* variable is set.
//
// Returns:
//   - [Config]: listening on ":8080", with media in "./media" and data in
//     "./data" (both relative, not yet made absolute), a 5 minute scan
//     interval, 4 votes per correction, 2 transcodes and 1 conversion at a
//     time, 10 accounts, conversion on, TMDB lookups off, and ffmpeg and
//     ffprobe looked up on the PATH.
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

// [Load] reads the settings from the process's environment variables. It is
// [LoadFrom] with [os.Getenv].
//
// Returns:
//   - [Config]: the settings, valid and with absolute folders if [error] is
//     [nil].
//   - [error]: [nil] on success, or every problem found, joined: values
//     that cannot be parsed and settings out of range.
func Load() (Config, error) { return LoadFrom(os.Getenv) }

// [LoadFrom] reads the settings from a source of environment variables,
// applies [Defaults] for those not set, validates the result with
// [Config.Validate], and makes [Config.MediaDirectory] and
// [Config.DataDirectory] absolute. It reports every bad variable at once
// rather than stopping at the first. A variable set to "" counts as not set.
//
// Parameters:
//   - [getenv]: looks up one variable by name and returns its value, or ""
//     if it is not set: [os.Getenv], or a map lookup in tests.
//
// Returns:
//   - [Config]: the settings. When [error] is not [nil] it holds whatever
//     was parsed and must not be used.
//   - [error]: [nil] on success; otherwise every problem joined with
//     [errors.Join], each naming its variable, such as
//     "NEXUSPLAY_MAX_ACCOUNTS: \"x\" is not a whole number"; or the error
//     from [filepath.Abs] if a folder cannot be made absolute.
func LoadFrom(getenv func(string) string) (Config, error) {
	config := Defaults()
	var problems []error
	for _, variable := range config.variables() {
		if value := getenv(variable.name); value != "" {
			if err := variable.set(value); err != nil {
				problems = append(problems, fmt.Errorf("%s: %q %v", variable.name, value, err))
			}
		}
	}
	problems = append(problems, config.Validate())
	if err := errors.Join(problems...); err != nil {
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

// [variable] is one NEXUSPLAY_* setting: its name, and how to parse a value
// into its [Config] field.
type variable struct {
	// [name] is the environment variable's name, such as "NEXUSPLAY_ADDR".
	name string
	// [set] parses a value and stores it in the setting's field, or returns an
	// error that completes the sentence "NEXUSPLAY_X: \"value\" …", such as
	// "is not a whole number".
	set func(string) error
}

// [variables] lists every setting read from the environment, bound to this
// configuration's fields.
//
// Returns:
//   - [[]variable]: one entry per NEXUSPLAY_* variable, each of whose
//     [variable.set] writes into [config].
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

// [text] makes a parser for a string setting, which accepts any value.
//
// Parameters:
//   - [destination]: the field to store the value in.
//
// Returns:
//   - [func(string) error]: a parser that stores its value in
//     [destination] and always returns [nil].
func text(destination *string) func(string) error {
	return func(value string) error { *destination = value; return nil }
}

// [number] makes a parser for a whole-number setting.
//
// Parameters:
//   - [destination]: the field to store the number in.
//
// Returns:
//   - [func(string) error]: a parser that stores the number in
//     [destination], or leaves it unchanged and returns "is not a whole
//     number" if the value is not one. Ranges are checked later by
//     [Config.Validate].
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

// [flag] makes a parser for an on/off setting.
//
// Parameters:
//   - [destination]: the field to store the setting in.
//
// Returns:
//   - [func(string) error]: a parser that accepts what [strconv.ParseBool]
//     accepts ("1", "true", "TRUE", "0", "false" and so on) and stores it in
//     [destination], or leaves it unchanged and returns "is not true or
//     false".
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

// [duration] makes a parser for a length-of-time setting.
//
// Parameters:
//   - [destination]: the field to store the duration in.
//
// Returns:
//   - [func(string) error]: a parser that accepts what [time.ParseDuration]
//     accepts ("90s", "5m", "1h") and stores it in [destination], or leaves
//     it unchanged and returns "is not a duration such as 5m or 1h".
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

// [Validate] checks that every setting is in range: at least 1 for
// [Config.VotesRequired], [Config.MaxTranscodes] and [Config.MaxConversions],
// at least 10 seconds for [Config.ScanInterval], not negative for
// [Config.MaxAccounts], and a [Config.Address] that is not "".
//
// Returns:
//   - [error]: [nil] if every setting is in range, or one error per problem
//     joined with [errors.Join], each naming its variable.
func (config Config) Validate() error {
	var problems []error
	atLeastOne := func(key string, count int) {
		if count < 1 {
			problems = append(problems, fmt.Errorf("%s must be at least 1, not %d", key, count))
		}
	}
	atLeastOne("NEXUSPLAY_VOTES_REQUIRED", config.VotesRequired)
	atLeastOne("NEXUSPLAY_MAX_TRANSCODES", config.MaxTranscodes)
	atLeastOne("NEXUSPLAY_MAX_CONVERSIONS", config.MaxConversions)
	// Changes are picked up within seconds by the file watcher; the interval
	// is only the safety net, and walking a big library too often wastes disk I/O.
	if config.ScanInterval < 10*time.Second {
		problems = append(problems, fmt.Errorf("NEXUSPLAY_SCAN_INTERVAL must be at least 10s, not %s", config.ScanInterval))
	}
	if config.MaxAccounts < 0 {
		problems = append(problems, fmt.Errorf("NEXUSPLAY_MAX_ACCOUNTS must be 0 (no limit) or more, not %d", config.MaxAccounts))
	}
	if config.Address == "" {
		problems = append(problems, errors.New("NEXUSPLAY_ADDR must not be empty"))
	}
	return errors.Join(problems...)
}

// [PosterDirectory] gives the folder where downloaded and uploaded posters
// are stored.
//
// Returns:
//   - [string]: "posters" inside [Config.DataDirectory].
func (config Config) PosterDirectory() string { return filepath.Join(config.DataDirectory, "posters") }

// [TranscodeDirectory] gives the scratch folder for on-the-fly HLS output.
//
// Returns:
//   - [string]: "transcode" inside [Config.DataDirectory].
func (config Config) TranscodeDirectory() string {
	return filepath.Join(config.DataDirectory, "transcode")
}

// [DatabasePath] gives the SQLite database file.
//
// Returns:
//   - [string]: "nexusplay.db" inside [Config.DataDirectory].
func (config Config) DatabasePath() string {
	return filepath.Join(config.DataDirectory, "nexusplay.db")
}

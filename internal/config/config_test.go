package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lookup(environment map[string]string) func(string) string {
	return func(key string) string { return environment[key] }
}

func TestDefaults(t *testing.T) {
	config, err := LoadFrom(lookup(nil))
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != ":8080" || !config.Convert || config.MaxConversions != 1 || config.MaxTranscodes != 2 ||
		config.VotesRequired != 4 || config.ScanInterval != 5*time.Minute || config.RequireLogin || config.MaxAccounts != 10 {
		t.Errorf("defaults = %+v", config)
	}
	if !filepath.IsAbs(config.MediaDirectory) || !filepath.IsAbs(config.DataDirectory) {
		t.Errorf("folders must be absolute: %q %q", config.MediaDirectory, config.DataDirectory)
	}
	if config.DatabasePath() != filepath.Join(config.DataDirectory, "nexusplay.db") {
		t.Errorf("DBPath = %q", config.DatabasePath())
	}
}

func TestLoadFrom(t *testing.T) {
	config, err := LoadFrom(lookup(map[string]string{
		"NEXUSPLAY_ADDR":            "127.0.0.1:9000",
		"NEXUSPLAY_MEDIA_DIR":       "/srv/media",
		"NEXUSPLAY_SCAN_INTERVAL":   "1h",
		"NEXUSPLAY_MAX_CONVERSIONS": "3",
		"NEXUSPLAY_CONVERT":         "false",
		"NEXUSPLAY_REQUIRE_LOGIN":   "1",
		"NEXUSPLAY_TMDB_API_KEY":    "abc",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if config.Address != "127.0.0.1:9000" || config.MediaDirectory != "/srv/media" || config.ScanInterval != time.Hour ||
		config.MaxConversions != 3 || config.Convert || !config.RequireLogin || config.TMDBAPIKey != "abc" {
		t.Errorf("config = %+v", config)
	}
}

// Every bad variable is reported at once, by name.
func TestLoadFromReportsEveryError(t *testing.T) {
	_, err := LoadFrom(lookup(map[string]string{
		"NEXUSPLAY_MAX_TRANSCODES": "two",
		"NEXUSPLAY_CONVERT":        "maybe",
		"NEXUSPLAY_SCAN_INTERVAL":  "soon",
		"NEXUSPLAY_VOTES_REQUIRED": "0",
	}))
	if err == nil {
		t.Fatal("bad settings were accepted")
	}
	for _, key := range []string{"NEXUSPLAY_MAX_TRANSCODES", "NEXUSPLAY_CONVERT", "NEXUSPLAY_SCAN_INTERVAL", "NEXUSPLAY_VOTES_REQUIRED"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s:\n%v", key, err)
		}
	}
}

func TestValidate(t *testing.T) {
	config := Defaults()
	if err := config.Validate(); err != nil {
		t.Errorf("defaults are invalid: %v", err)
	}
	for name, mutate := range map[string]func(*Config){
		"no transcodes":         func(config *Config) { config.MaxTranscodes = 0 },
		"negative conversions":  func(config *Config) { config.MaxConversions = -1 },
		"scan every second":     func(config *Config) { config.ScanInterval = time.Second },
		"negative scan":         func(config *Config) { config.ScanInterval = -time.Minute },
		"no address":            func(config *Config) { config.Address = "" },
		"zero votes to approve": func(config *Config) { config.VotesRequired = 0 },
		"negative accounts":     func(config *Config) { config.MaxAccounts = -1 },
	} {
		config := Defaults()
		mutate(&config)
		if err := config.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

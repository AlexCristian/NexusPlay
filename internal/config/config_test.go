package config

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lookup(env map[string]string) func(string) string {
	return func(k string) string { return env[k] }
}

func TestDefaults(t *testing.T) {
	c, err := LoadFrom(lookup(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || !c.Convert || c.MaxConversions != 1 || c.MaxTranscodes != 2 ||
		c.VotesRequired != 4 || c.ScanInterval != 5*time.Minute || c.RequireLogin {
		t.Errorf("defaults = %+v", c)
	}
	if !filepath.IsAbs(c.MediaDir) || !filepath.IsAbs(c.DataDir) {
		t.Errorf("folders must be absolute: %q %q", c.MediaDir, c.DataDir)
	}
	if c.DBPath() != filepath.Join(c.DataDir, "nexusplay.db") {
		t.Errorf("DBPath = %q", c.DBPath())
	}
}

func TestLoadFrom(t *testing.T) {
	c, err := LoadFrom(lookup(map[string]string{
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
	if c.Addr != "127.0.0.1:9000" || c.MediaDir != "/srv/media" || c.ScanInterval != time.Hour ||
		c.MaxConversions != 3 || c.Convert || !c.RequireLogin || c.TMDBAPIKey != "abc" {
		t.Errorf("config = %+v", c)
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
	ok := Defaults()
	if err := ok.Validate(); err != nil {
		t.Errorf("defaults are invalid: %v", err)
	}
	for name, mutate := range map[string]func(*Config){
		"no transcodes":         func(c *Config) { c.MaxTranscodes = 0 },
		"negative conversions":  func(c *Config) { c.MaxConversions = -1 },
		"scan every second":     func(c *Config) { c.ScanInterval = time.Second },
		"negative scan":         func(c *Config) { c.ScanInterval = -time.Minute },
		"no address":            func(c *Config) { c.Addr = "" },
		"zero votes to approve": func(c *Config) { c.VotesRequired = 0 },
	} {
		c := Defaults()
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

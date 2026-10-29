// Package posters removes poster images nothing uses any more. Posters are
// written by TMDB lookups (metadata) and by uploads in the control panel
// (web), and several rows can share one file, so neither writer deletes a
// file when it replaces one. The Sweeper deletes files that no media or
// collection refers to, once they are old enough that a row may not simply
// be about to refer to them.
package posters

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// Store lists the poster files in use; *store.Store implements it.
type Store interface {
	PosterNames(ctx context.Context) (map[string]bool, error)
}

// Config sets where posters live and how careful the sweep is.
type Config struct {
	Directory string
	MinAge    time.Duration // a file younger than this is kept (default 1 h)
	Interval  time.Duration // time between sweeps (default 1 h)
}

// Sweeper deletes unused poster files.
type Sweeper struct {
	store  Store
	config Config
	log    *slog.Logger
	now    func() time.Time
}

// NewSweeper returns a sweeper; nothing runs until Run.
func NewSweeper(database Store, config Config, log *slog.Logger) *Sweeper {
	if config.MinAge <= 0 {
		config.MinAge = time.Hour
	}
	if config.Interval <= 0 {
		config.Interval = time.Hour
	}
	return &Sweeper{store: database, config: config, log: log, now: time.Now}
}

// Run sweeps at once and then every Interval until ctx ends.
func (sweeper *Sweeper) Run(ctx context.Context) error {
	ticker := time.NewTicker(sweeper.config.Interval)
	defer ticker.Stop()
	for {
		if deleted, err := sweeper.Sweep(ctx); err != nil && ctx.Err() == nil {
			sweeper.log.Warn("removing unused posters", "err", err)
		} else if deleted > 0 {
			sweeper.log.Info("removed unused posters", "count", deleted)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

// Sweep deletes every file in the poster folder that no row refers to and
// that is at least MinAge old, including leftovers of interrupted
// downloads. It returns how many files it removed.
//
// The age check is what makes this safe without a lock: a poster is
// written to disk just before the row that uses it is saved, and an hour
// is far longer than that gap.
func (sweeper *Sweeper) Sweep(ctx context.Context) (int, error) {
	inUse, err := sweeper.store.PosterNames(ctx)
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(sweeper.config.Directory)
	if err != nil {
		return 0, err
	}
	cutoff := sweeper.now().Add(-sweeper.config.MinAge)
	removed := 0
	for _, entry := range entries {
		if !entry.Type().IsRegular() || inUse[entry.Name()] {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(sweeper.config.Directory, entry.Name())); err != nil {
			sweeper.log.Warn("could not remove an unused poster", "file", entry.Name(), "err", err)
			continue
		}
		removed++
	}
	return removed, nil
}

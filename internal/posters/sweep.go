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
	Dir      string
	MinAge   time.Duration // a file younger than this is kept (default 1 h)
	Interval time.Duration // time between sweeps (default 1 h)
}

// Sweeper deletes unused poster files.
type Sweeper struct {
	store Store
	cfg   Config
	log   *slog.Logger
	now   func() time.Time
}

// NewSweeper returns a sweeper; nothing runs until Run.
func NewSweeper(st Store, cfg Config, log *slog.Logger) *Sweeper {
	if cfg.MinAge <= 0 {
		cfg.MinAge = time.Hour
	}
	if cfg.Interval <= 0 {
		cfg.Interval = time.Hour
	}
	return &Sweeper{store: st, cfg: cfg, log: log, now: time.Now}
}

// Run sweeps at once and then every Interval until ctx ends.
func (s *Sweeper) Run(ctx context.Context) error {
	t := time.NewTicker(s.cfg.Interval)
	defer t.Stop()
	for {
		if n, err := s.Sweep(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("removing unused posters", "err", err)
		} else if n > 0 {
			s.log.Info("removed unused posters", "count", n)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
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
func (s *Sweeper) Sweep(ctx context.Context) (int, error) {
	inUse, err := s.store.PosterNames(ctx)
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(s.cfg.Dir)
	if err != nil {
		return 0, err
	}
	cutoff := s.now().Add(-s.cfg.MinAge)
	removed := 0
	for _, e := range entries {
		if !e.Type().IsRegular() || inUse[e.Name()] {
			continue
		}
		info, err := e.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		if err := os.Remove(filepath.Join(s.cfg.Dir, e.Name())); err != nil {
			s.log.Warn("could not remove an unused poster", "file", e.Name(), "err", err)
			continue
		}
		removed++
	}
	return removed, nil
}

package posters

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

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
		if !entry.Type().IsRegular() || inUse.Contains(entry.Name()) {
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

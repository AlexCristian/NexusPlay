package posters

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// [Sweeper] deletes unused poster files, once at start and then on a timer.
// Create one with [NewSweeper] and start it with [Sweeper.Run].
type Sweeper struct {
	// [store] tells which poster files are in use.
	store Store
	// [config] holds the poster folder and timings, with defaults filled in.
	config Config
	// [log] records each sweep's result and files that could not be deleted.
	log *slog.Logger
	// [now] tells the time, for the age check. Tests replace it.
	now func() time.Time
}

// [NewSweeper] creates a poster sweeper. Nothing runs until [Sweeper.Run].
//
// Parameters:
//   - [database]: tells which poster files are in use; normally the
//     [store.Store].
//   - [config]: the poster folder and timings. A [Config.MinAge] or
//     [Config.Interval] of 0 or less becomes one hour.
//   - [log]: where sweep results and failures are logged.
//
// Returns:
//   - [*Sweeper]: the new sweeper.
func NewSweeper(database Store, config Config, log *slog.Logger) *Sweeper {
	if config.MinAge <= 0 {
		config.MinAge = time.Hour
	}
	if config.Interval <= 0 {
		config.Interval = time.Hour
	}
	return &Sweeper{store: database, config: config, log: log, now: time.Now}
}

// [Run] sweeps at once and then every [Config.Interval] until [ctx] ends,
// logging how many files each sweep removed and any error. It blocks, so
// call it in its own goroutine.
//
// Parameters:
//   - [ctx]: stops the sweeper when it is cancelled.
//
// Returns:
//   - [error]: always [nil], once [ctx] ends; failed sweeps are logged, not
//     returned.
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

// [Sweep] deletes every regular file in the poster folder that no row refers
// to and that was last modified at least [Config.MinAge] ago, including
// leftovers of interrupted downloads. Subfolders and other non-regular files
// are left alone. A file that cannot be deleted is logged and skipped.
//
// The age check is what makes this safe without a lock: a poster is written
// to disk just before the row that uses it is saved, and an hour is far
// longer than that gap.
//
// Parameters:
//   - [ctx]: cancels the database query.
//
// Returns:
//   - [int]: how many files were deleted.
//   - [error]: [nil] on success, or the error from listing the posters in
//     use or reading the poster folder, in which case nothing is deleted.
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

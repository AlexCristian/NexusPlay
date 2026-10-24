package convert

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sync"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/media"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// Store is the database as the converter uses it; *store.Store implements it.
type Store interface {
	ConversionCandidates(ctx context.Context) ([]store.ConversionCandidate, error)
	MediaByID(ctx context.Context, id int64) (store.Media, error)
	ReplaceMediaFile(ctx context.Context, id int64, f store.ReplacedFile) error
	SetConvertFailed(ctx context.Context, id int64, reason string) error
}

// Tools runs ffprobe and ffmpeg; ffmpeg.Tools is the real one.
type Tools interface {
	Probe(ctx context.Context, file string) (ffmpeg.Probe, error)
	Run(ctx context.Context, args []string, opt ffmpeg.RunOptions) error
}

// Library is the scanner as the converter sees it: the lock it shares with
// scans, and a way to ask for a rescan (which imports extracted subtitles).
type Library interface {
	Exclusive(fn func() error) error
	Trigger()
}

// Deps are the converter's collaborators.
type Deps struct {
	Store   Store
	Tools   Tools
	Library Library // nil in tests that run without a scanner
	Log     *slog.Logger
}

// Config tunes the converter.
type Config struct {
	MediaDir string
	Max      int // conversions at once (at least 1)
	// FreeSpace reports the free bytes in a folder's file system. Nil uses
	// the operating system; tests replace it.
	FreeSpace func(dir string) (uint64, error)
}

// Converter works through the queue of files that need converting, at most
// Max at a time, newest first, and deletes each original once its MP4 is
// verified and in the library. Conversions run at low CPU priority so
// playback stays smooth. Build it with New; it is safe for concurrent use.
//
// A file's state, from the control panel's point of view:
//   - queued: needs converting (media.NeedsConversion) and has not failed;
//   - running: being converted now;
//   - postponed: skipped for noSpaceRetry because the disk was full;
//   - failed: recorded in the database until an administrator retries it.
//
// Only "failed" is stored; the queue is computed from the library.
type Converter struct {
	deps Deps
	cfg  Config
	now  func() time.Time
	wake chan struct{}

	mu          sync.Mutex // guards everything below
	active      map[int64]*job
	waitUntil   map[int64]time.Time // postponed files
	unavailable string              // why Run stopped before converting anything
}

// job is one conversion in progress.
type job struct {
	media    store.Media
	duration float64 // seconds; from the probe once known
	done     float64 // seconds converted so far
}

// percent is how far the job has got, 0 to 99 (100 only once it is done).
// The caller holds the converter's mutex.
func (j *job) percent() int {
	if j.duration <= 0 {
		return 0
	}
	return int(math.Min(99, j.done/j.duration*100))
}

// New returns a converter. Call Run to start working through the queue.
func New(d Deps, c Config) *Converter {
	if c.Max < 1 {
		c.Max = 1
	}
	if c.FreeSpace == nil {
		c.FreeSpace = freeBytes
	}
	return &Converter{
		deps: d, cfg: c, now: time.Now, wake: make(chan struct{}, 1),
		active: map[int64]*job{}, waitUntil: map[int64]time.Time{},
	}
}

// Wake asks the converter to look at the queue now (after a scan found new files).
func (c *Converter) Wake() {
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Run processes the queue until ctx ends, and returns after every running
// conversion has stopped. If the media folder is not writable it returns at
// once and Status says why.
func (c *Converter) Run(ctx context.Context) error {
	if err := checkWritable(c.cfg.MediaDir); err != nil {
		c.mu.Lock()
		c.unavailable = fmt.Sprintf("Conversion is off: the media folder is read-only or not writable by this "+
			"server's user (UID %d), so originals cannot be replaced. In Docker, mount it without :ro and make "+
			"sure that user owns it, or set user: in docker-compose.yml to the folder's owner.", os.Getuid())
		c.mu.Unlock()
		c.deps.Log.Warn("automatic conversion disabled", "reason", err)
		return nil
	}
	if err := removeLeftovers(c.cfg.MediaDir); err != nil {
		c.deps.Log.Warn("could not remove files left by an interrupted conversion", "err", err)
	}

	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		c.fill(ctx, &wg)
		select {
		case <-ctx.Done():
			return nil
		case <-c.wake:
		case <-tick.C:
		}
	}
}

// fill starts conversions until Max are running or the queue is empty. Only
// Run calls it, so the free-slot count cannot change underneath it except by
// jobs finishing, which only frees more.
func (c *Converter) fill(ctx context.Context, wg *sync.WaitGroup) {
	c.mu.Lock()
	free := c.cfg.Max - len(c.active)
	c.mu.Unlock()
	if free <= 0 {
		return
	}
	queue, _, err := c.queue(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.deps.Log.Error("reading the conversion queue", "err", err)
		}
		return
	}
	for _, id := range queue[:min(free, len(queue))] {
		m, err := c.deps.Store.MediaByID(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			continue // removed by a scan since the queue was read
		}
		if err != nil {
			if ctx.Err() == nil {
				c.deps.Log.Error("reading the conversion queue", "err", err)
			}
			return
		}
		j := &job{media: m, duration: m.Duration}
		c.mu.Lock()
		c.active[m.ID] = j
		c.mu.Unlock()
		wg.Go(func() {
			c.runJob(ctx, j)
			c.mu.Lock()
			delete(c.active, j.media.ID)
			c.mu.Unlock()
			c.Wake()
		})
	}
}

// queue lists the IDs of files waiting for conversion, newest first,
// leaving out running ones. It also counts postponed ones.
func (c *Converter) queue(ctx context.Context) (queued []int64, postponed int, err error) {
	cands, err := c.deps.Store.ConversionCandidates(ctx)
	if err != nil {
		return nil, 0, err
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, m := range cands {
		f := media.Format{Path: m.Path, VideoCodec: m.VideoCodec, AudioCodec: m.AudioCodec, PixFmt: m.PixFmt}
		switch {
		case !media.NeedsConversion(f) || c.active[m.ID] != nil:
		case now.Before(c.waitUntil[m.ID]):
			postponed++
		default:
			queued = append(queued, m.ID)
		}
	}
	return queued, postponed, nil
}

// JobStatus describes a conversion in progress.
type JobStatus struct {
	Media   store.Media
	Percent int
}

// Status is a snapshot for the control panel.
type Status struct {
	Off         bool   // conversion is turned off in the configuration
	Unavailable string // why conversion cannot run (a read-only folder), if it cannot
	Active      []JobStatus
	Queued      int
	Postponed   int // waiting for disk space
}

// Status reports what the converter is doing.
func (c *Converter) Status(ctx context.Context) (Status, error) {
	c.mu.Lock()
	st := Status{Unavailable: c.unavailable}
	for _, j := range c.active {
		st.Active = append(st.Active, JobStatus{Media: j.media, Percent: j.percent()})
	}
	c.mu.Unlock()
	if st.Unavailable != "" {
		return st, nil
	}
	q, postponed, err := c.queue(ctx)
	st.Queued, st.Postponed = len(q), postponed
	return st, err
}

// Disabled stands in for the converter when conversion is turned off, so
// callers never need a nil check.
type Disabled struct{}

// Status reports that conversion is off.
func (Disabled) Status(context.Context) (Status, error) { return Status{Off: true}, nil }

// Wake does nothing.
func (Disabled) Wake() {}

// runJob converts one file and records the outcome. Shutdown, a changed
// source and a full disk are not failures; anything else is recorded so the
// control panel can show it.
func (c *Converter) runJob(ctx context.Context, j *job) {
	m := j.media
	start := c.now()
	c.deps.Log.Info("converting", "media", m.ID, "path", m.Path)
	newPath, err := c.convert(ctx, j)
	switch {
	case ctx.Err() != nil:
		c.deps.Log.Info("conversion stopped by shutdown; it starts over after the restart",
			"path", m.Path, "progress", fmt.Sprintf("%d%%", c.percent(j)))
	case errors.Is(err, ErrSourceChanged):
		c.deps.Log.Info("conversion discarded; the file changed while converting", "path", m.Path)
	case errors.Is(err, ErrNoSpace):
		c.mu.Lock()
		c.waitUntil[m.ID] = c.now().Add(noSpaceRetry)
		c.mu.Unlock()
		c.deps.Log.Warn("conversion postponed", "path", m.Path, "err", err)
	case err != nil:
		c.deps.Log.Error("conversion failed", "path", m.Path, "err", err)
		if err := c.deps.Store.SetConvertFailed(context.WithoutCancel(ctx), m.ID, err.Error()); err != nil {
			c.deps.Log.Error("recording a failed conversion", "err", err)
		}
	default:
		c.deps.Log.Info("converted", "media", m.ID, "from", m.Path, "to", newPath, "took", c.now().Sub(start).Round(time.Second))
		if c.deps.Library != nil {
			c.deps.Library.Trigger() // imports the extracted subtitles
		}
	}
}

func (c *Converter) percent(j *job) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return j.percent()
}

func (c *Converter) setProgress(j *job, seconds float64) {
	c.mu.Lock()
	j.done = seconds
	c.mu.Unlock()
}

func (c *Converter) setDuration(j *job, seconds float64) {
	if seconds > 0 {
		c.mu.Lock()
		j.duration = seconds
		c.mu.Unlock()
	}
}

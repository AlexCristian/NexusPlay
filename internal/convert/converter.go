package convert

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/media"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

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
	deps   Deps
	config Config
	now    func() time.Time
	wake   chan struct{}

	mutex       sync.Mutex // guards everything below
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
func (job *job) percent() int {
	if job.duration <= 0 {
		return 0
	}
	return int(math.Min(99, job.done/job.duration*100))
}

// New returns a converter. Call Run to start working through the queue.
func New(dependencies Deps, config Config) *Converter {
	if config.Max < 1 {
		config.Max = 1
	}
	if config.FreeSpace == nil {
		config.FreeSpace = freeBytes
	}
	return &Converter{
		deps: dependencies, config: config, now: time.Now, wake: make(chan struct{}, 1),
		active: map[int64]*job{}, waitUntil: map[int64]time.Time{},
	}
}

// Wake asks the converter to look at the queue now (after a scan found new files).
func (converter *Converter) Wake() {
	select {
	case converter.wake <- struct{}{}:
	default:
	}
}

// Run processes the queue until ctx ends, and returns after every running
// conversion has stopped. If the media folder is not writable it returns at
// once and Status says why.
func (converter *Converter) Run(ctx context.Context) error {
	if err := checkWritable(converter.config.MediaDirectory); err != nil {
		converter.mutex.Lock()
		converter.unavailable = fmt.Sprintf("Conversion is off: the media folder is read-only or not writable by this "+
			"server's user (UID %d), so originals cannot be replaced. In Docker, mount it without :ro and make "+
			"sure that user owns it, or set user: in docker-compose.yml to the folder's owner.", os.Getuid())
		converter.mutex.Unlock()
		converter.deps.Log.Warn("automatic conversion disabled", "reason", err)
		return nil
	}
	if err := removeLeftovers(converter.config.MediaDirectory); err != nil {
		converter.deps.Log.Warn("could not remove files left by an interrupted conversion", "err", err)
	}

	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	var waitGroup sync.WaitGroup
	defer waitGroup.Wait()
	for {
		converter.fill(ctx, &waitGroup)
		select {
		case <-ctx.Done():
			return nil
		case <-converter.wake:
		case <-tick.C:
		}
	}
}

// fill starts conversions until Max are running or the queue is empty. Only
// Run calls it, so the free-slot count cannot change underneath it except by
// jobs finishing, which only frees more.
func (converter *Converter) fill(ctx context.Context, waitGroup *sync.WaitGroup) {
	converter.mutex.Lock()
	free := converter.config.Max - len(converter.active)
	converter.mutex.Unlock()
	if free <= 0 {
		return
	}
	queue, _, err := converter.queue(ctx)
	if err != nil {
		if ctx.Err() == nil {
			converter.deps.Log.Error("reading the conversion queue", "err", err)
		}
		return
	}
	for _, id := range queue[:min(free, len(queue))] {
		media, err := converter.deps.Store.MediaByID(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			continue // removed by a scan since the queue was read
		}
		if err != nil {
			if ctx.Err() == nil {
				converter.deps.Log.Error("reading the conversion queue", "err", err)
			}
			return
		}
		newJob := &job{media: media, duration: media.Duration}
		converter.mutex.Lock()
		converter.active[media.ID] = newJob
		converter.mutex.Unlock()
		waitGroup.Go(func() {
			converter.runJob(ctx, newJob)
			converter.mutex.Lock()
			delete(converter.active, newJob.media.ID)
			converter.mutex.Unlock()
			converter.Wake()
		})
	}
}

// queue lists the IDs of files waiting for conversion, newest first,
// leaving out running ones. It also counts postponed ones.
func (converter *Converter) queue(ctx context.Context) (queued []int64, postponed int, err error) {
	cands, err := converter.deps.Store.ConversionCandidates(ctx)
	if err != nil {
		return nil, 0, err
	}
	now := converter.now()
	converter.mutex.Lock()
	defer converter.mutex.Unlock()
	for _, candidate := range cands {
		format := media.Format{Path: candidate.Path, VideoCodec: candidate.VideoCodec, AudioCodec: candidate.AudioCodec, PixFmt: candidate.PixFmt}
		switch {
		case !media.NeedsConversion(format) || converter.active[candidate.ID] != nil:
		case now.Before(converter.waitUntil[candidate.ID]):
			postponed++
		default:
			queued = append(queued, candidate.ID)
		}
	}
	return queued, postponed, nil
}

// Status reports what the converter is doing.
func (converter *Converter) Status(ctx context.Context) (Status, error) {
	converter.mutex.Lock()
	status := Status{Unavailable: converter.unavailable}
	for _, job := range converter.active {
		status.Active = append(status.Active, JobStatus{Media: job.media, Percent: job.percent()})
	}
	converter.mutex.Unlock()
	if status.Unavailable != "" {
		return status, nil
	}
	queued, postponed, err := converter.queue(ctx)
	status.Queued, status.Postponed = len(queued), postponed
	return status, err
}

// runJob converts one file and records the outcome. Shutdown, a changed
// source and a full disk are not failures; anything else is recorded so the
// control panel can show it.
func (converter *Converter) runJob(ctx context.Context, job *job) {
	media := job.media
	start := converter.now()
	converter.deps.Log.Info("converting", "media", media.ID, "path", media.Path)
	newPath, err := converter.convert(ctx, job)
	switch {
	case ctx.Err() != nil:
		converter.deps.Log.Info("conversion stopped by shutdown; it starts over after the restart",
			"path", media.Path, "progress", fmt.Sprintf("%d%%", converter.percent(job)))
	case errors.Is(err, ErrSourceChanged):
		converter.deps.Log.Info("conversion discarded; the file changed while converting", "path", media.Path)
	case errors.Is(err, ErrNoSpace):
		converter.mutex.Lock()
		converter.waitUntil[media.ID] = converter.now().Add(noSpaceRetry)
		converter.mutex.Unlock()
		converter.deps.Log.Warn("conversion postponed", "path", media.Path, "err", err)
	case err != nil:
		converter.deps.Log.Error("conversion failed", "path", media.Path, "err", err)
		if err := converter.deps.Store.SetConvertFailed(context.WithoutCancel(ctx), media.ID, err.Error()); err != nil {
			converter.deps.Log.Error("recording a failed conversion", "err", err)
		}
	default:
		converter.deps.Log.Info("converted", "media", media.ID, "from", media.Path, "to", newPath, "took", converter.now().Sub(start).Round(time.Second))
		if converter.deps.Library != nil {
			converter.deps.Library.Trigger() // imports the extracted subtitles
		}
	}
}

func (converter *Converter) percent(job *job) int {
	converter.mutex.Lock()
	defer converter.mutex.Unlock()
	return job.percent()
}

func (converter *Converter) setProgress(job *job, seconds float64) {
	converter.mutex.Lock()
	job.done = seconds
	converter.mutex.Unlock()
}

func (converter *Converter) setDuration(job *job, seconds float64) {
	if seconds > 0 {
		converter.mutex.Lock()
		job.duration = seconds
		converter.mutex.Unlock()
	}
}

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

// [Converter] works through the queue of files that need converting, at most
// [Config.Max] at a time, newest first, and deletes each original once its
// MP4 is checked and in the library. The encode runs at low CPU priority so
// playback stays smooth; subtitle extraction, which is short, does not. Create one with [New] and start it with
// [Converter.Run]. It is safe for concurrent use.
//
// A file's state, from the control panel's point of view:
//   - queued: needs converting ([media.NeedsConversion]) and has not failed;
//   - running: being converted now;
//   - postponed: skipped for [noSpaceRetry] because the disk was too full;
//   - failed: recorded in the database until an administrator retries it.
//
// Only "failed" is stored; the queue is computed from the library each time.
type Converter struct {
	// [dependencies] are the database, ffmpeg, scanner and log.
	dependencies Dependencies
	// [config] holds the settings, with defaults filled in.
	config Config
	// [now] tells the time, for postponing and timing. Tests replace it.
	now func() time.Time
	// [wake] carries requests from [Converter.Wake] to [Converter.Run]. It holds
	// at most one, since one look at the queue answers any number of requests.
	wake chan struct{}

	// [mutex] guards [active], [waitUntil] and [unavailable], and each active
	// job's progress.
	mutex sync.Mutex
	// [active] maps the media ID of each running conversion to its job.
	active map[int64]*job
	// [waitUntil] maps the media ID of each postponed file to when it may be
	// tried again.
	waitUntil map[int64]time.Time
	// [unavailable] explains why [Converter.Run] stopped before converting
	// anything, or is "" if it did not.
	unavailable string
}

// [job] is one conversion in progress. Its [job.duration] and [job.done]
// change while it runs; read and write them only while holding the
// converter's [Converter.mutex].
type job struct {
	// [media] is the file being converted, as it was when the job started.
	media store.Media
	// [duration] is the file's length in seconds: from the database at first,
	// then from the probe once it has run.
	duration float64
	// [done] is how many seconds of the file have been converted so far.
	done float64
}

// [percent] tells how far the job has got. The caller must hold the
// converter's [Converter.mutex].
//
// Returns:
//   - [int]: from 0 to 99, or 0 while the length is not known. It never
//     reaches 100, since a finished job is no longer listed.
func (job *job) percent() int {
	if job.duration <= 0 {
		return 0
	}
	return int(math.Min(99, job.done/job.duration*100))
}

// [New] creates a converter. Nothing runs until [Converter.Run].
//
// Parameters:
//   - [dependencies]: the database, ffmpeg, scanner and log.
//   - [config]: the media folder and limits. A [Config.Max] below 1 becomes
//     1, and a [nil] [Config.FreeSpace] asks the operating system.
//
// Returns:
//   - [*Converter]: the new converter.
func New(dependencies Dependencies, config Config) *Converter {
	if config.Max < 1 {
		config.Max = 1
	}
	if config.FreeSpace == nil {
		config.FreeSpace = freeBytes
	}
	return &Converter{
		dependencies: dependencies, config: config, now: time.Now, wake: make(chan struct{}, 1),
		active: map[int64]*job{}, waitUntil: map[int64]time.Time{},
	}
}

// [Wake] asks the converter to look at the queue now, such as after a scan
// found new files, instead of at its next minute. It never blocks.
func (converter *Converter) Wake() {
	select {
	case converter.wake <- struct{}{}:
	default:
	}
}

// [Run] works through the queue until [ctx] ends: it looks again every
// minute, whenever [Converter.Wake] is called, and whenever a conversion
// finishes. At start it deletes files left by interrupted conversions. It
// blocks, so call it in its own goroutine; once [ctx] ends it returns after
// every running conversion has stopped.
//
// If the media folder is not writable it logs a warning and returns at
// once, and [Converter.Status] tells the administrator why.
//
// Parameters:
//   - [ctx]: stops the converter, and kills running conversions, when it is
//     cancelled.
//
// Returns:
//   - [error]: always [nil]; problems are logged and shown in
//     [Converter.Status].
func (converter *Converter) Run(ctx context.Context) error {
	if err := checkWritable(converter.config.MediaDirectory); err != nil {
		converter.mutex.Lock()
		converter.unavailable = fmt.Sprintf("Conversion is off: the media folder is read-only or not writable by this "+
			"server's user (UID %d), so originals cannot be replaced. In Docker, mount it without :ro and make "+
			"sure that user owns it, or set user: in docker-compose.yml to the folder's owner.", os.Getuid())
		converter.mutex.Unlock()
		converter.dependencies.Log.Warn("automatic conversion disabled", "reason", err)
		return nil
	}
	if err := removeLeftovers(converter.config.MediaDirectory); err != nil {
		converter.dependencies.Log.Warn("could not remove files left by an interrupted conversion", "err", err)
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

// [fill] starts conversions from the front of the queue until [Config.Max]
// are running or the queue is empty, each in its own goroutine. Only
// [Converter.Run] calls it, so the number of free slots can only grow
// underneath it, as jobs finish. A queue that cannot be read is logged.
//
// Parameters:
//   - [ctx]: passed to each conversion; cancelling it stops them.
//   - [waitGroup]: tracks the started conversions, so [Converter.Run] can
//     wait for them.
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
			converter.dependencies.Log.Error("reading the conversion queue", "err", err)
		}
		return
	}
	for _, id := range queue[:min(free, len(queue))] {
		media, err := converter.dependencies.Store.MediaByID(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			continue // removed by a scan since the queue was read
		}
		if err != nil {
			if ctx.Err() == nil {
				converter.dependencies.Log.Error("reading the conversion queue", "err", err)
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

// [queue] works out which files are waiting for conversion.
//
// Parameters:
//   - [ctx]: cancels the database query.
//
// Returns:
//   - [[]int64] ([queued]): the media IDs that need converting and are
//     neither running nor postponed, newest first.
//   - [int] ([postponed]): how many files that need converting are
//     postponed because the disk was too full.
//   - [error]: [nil] on success, or the database error.
func (converter *Converter) queue(ctx context.Context) (queued []int64, postponed int, err error) {
	candidates, err := converter.dependencies.Store.ConversionCandidates(ctx)
	if err != nil {
		return nil, 0, err
	}
	now := converter.now()
	converter.mutex.Lock()
	defer converter.mutex.Unlock()
	for _, candidate := range candidates {
		format := media.Format{Path: candidate.Path, VideoCodec: candidate.VideoCodec, AudioCodec: candidate.AudioCodec, PixelFormat: candidate.PixelFormat}
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

// [Status] reports what the converter is doing, for the control panel.
//
// Parameters:
//   - [ctx]: cancels the database query for the queue.
//
// Returns:
//   - [Status]: the running conversions and the queue's size, or only
//     [Status.Unavailable] if conversion could not start. If [error] is not
//     [nil], the running conversions are still listed but the counts are 0.
//   - [error]: [nil] on success, or the database error from reading the
//     queue.
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

// [runJob] converts one file and records the outcome. A shutdown and a file
// whose row now points elsewhere or was removed ([ErrSourceChanged]) are
// logged only; a disk too full postpones
// the file for [noSpaceRetry]; any other error is recorded with
// [Store.SetConvertFailed] so the control panel can show it. After a
// success it asks for a rescan to import the extracted subtitles.
//
// Parameters:
//   - [ctx]: stops the conversion when cancelled.
//   - [job]: the conversion to run.
func (converter *Converter) runJob(ctx context.Context, job *job) {
	media := job.media
	start := converter.now()
	converter.dependencies.Log.Info("converting", "media", media.ID, "path", media.Path)
	newPath, err := converter.convert(ctx, job)
	switch {
	case ctx.Err() != nil:
		converter.dependencies.Log.Info("conversion stopped by shutdown; it starts over after the restart",
			"path", media.Path, "progress", fmt.Sprintf("%d%%", converter.percent(job)))
	case errors.Is(err, ErrSourceChanged):
		converter.dependencies.Log.Info("conversion discarded; the file changed while converting", "path", media.Path)
	case errors.Is(err, ErrNoSpace):
		converter.mutex.Lock()
		converter.waitUntil[media.ID] = converter.now().Add(noSpaceRetry)
		converter.mutex.Unlock()
		converter.dependencies.Log.Warn("conversion postponed", "path", media.Path, "err", err)
	case err != nil:
		converter.dependencies.Log.Error("conversion failed", "path", media.Path, "err", err)
		if err := converter.dependencies.Store.SetConvertFailed(context.WithoutCancel(ctx), media.ID, err.Error()); err != nil {
			converter.dependencies.Log.Error("recording a failed conversion", "err", err)
		}
	default:
		converter.dependencies.Log.Info("converted", "media", media.ID, "from", media.Path, "to", newPath, "took", converter.now().Sub(start).Round(time.Second))
		if converter.dependencies.Library != nil {
			converter.dependencies.Library.Trigger() // imports the extracted subtitles
		}
	}
}

// [percent] reads a job's progress while holding the lock.
//
// Parameters:
//   - [job]: a running conversion.
//
// Returns:
//   - [int]: as [job.percent] describes.
func (converter *Converter) percent(job *job) int {
	converter.mutex.Lock()
	defer converter.mutex.Unlock()
	return job.percent()
}

// [setProgress] records how much of a job's file has been converted.
//
// Parameters:
//   - [job]: a running conversion.
//   - [seconds]: the seconds converted so far, as ffmpeg reports it.
func (converter *Converter) setProgress(job *job, seconds float64) {
	converter.mutex.Lock()
	job.done = seconds
	converter.mutex.Unlock()
}

// [setDuration] records a job's file length once the probe knows it.
//
// Parameters:
//   - [job]: a running conversion.
//   - [seconds]: the file's length in seconds; 0 or less is ignored.
func (converter *Converter) setDuration(job *job, seconds float64) {
	if seconds > 0 {
		converter.mutex.Lock()
		job.duration = seconds
		converter.mutex.Unlock()
	}
}

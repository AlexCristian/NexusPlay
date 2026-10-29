package stream

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/media"
)

// Manager owns the transcode jobs. It is safe for concurrent use. Build it
// with NewManager and run its janitor with Run.
type Manager struct {
	start  Starter
	config Config
	log    *slog.Logger

	mutex sync.Mutex // guards jobs and seq; never held during file or process work
	jobs  map[int64]*job
	seq   int
}

// NewManager prepares the transcode folder, clearing leftovers from a
// previous run.
func NewManager(start Starter, config Config, log *slog.Logger) (*Manager, error) {
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 3 * time.Minute
	}
	if config.KeepDone == 0 {
		config.KeepDone = 6 * time.Hour
	}
	if config.Max < 1 {
		return nil, fmt.Errorf("stream: Max must be at least 1")
	}
	if err := os.RemoveAll(config.Directory); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(config.Directory, 0o755); err != nil {
		return nil, err
	}
	return &Manager{start: start, config: config, log: log, jobs: map[int64]*job{}}, nil
}

// Playlist starts (or reuses) the transcode of a media file and waits until
// its playlist lists a first segment, up to 60 s (player.js allows 70 s). It
// returns the path of the playlist file, or ErrBusy at the concurrency limit.
func (manager *Manager) Playlist(ctx context.Context, mediaID int64, source string, format media.Format) (string, error) {
	job, err := manager.job(mediaID, source, format)
	if err != nil {
		return "", err
	}
	playlist := filepath.Join(job.directory, "index.m3u8")
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if data, err := os.ReadFile(playlist); err == nil && strings.Contains(string(data), "#EXTINF") {
			return playlist, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-job.done:
			if job.err != nil {
				return "", job.err
			}
			// Finished before a poll saw a segment (a very short file).
			if _, err := os.Stat(playlist); err != nil {
				return "", errors.New("transcode produced no playlist")
			}
			return playlist, nil
		case <-deadline.C:
			return "", errors.New("transcode did not start in time")
		case <-tick.C:
		}
	}
}

// Segment returns the path of a segment of a running or finished transcode,
// keeping the transcode alive. ffmpeg lists a segment only once it is
// written, but a client resuming after a restart may ask ahead, so Segment
// waits up to 30 s for the file. It returns ErrNoSegment if the media has no
// transcode or the segment does not appear.
func (manager *Manager) Segment(ctx context.Context, mediaID int64, name string) (string, error) {
	if !IsSegmentName(name) {
		return "", ErrNoSegment
	}
	directory, found := manager.Touch(mediaID)
	if !found {
		return "", ErrNoSegment
	}
	file := filepath.Join(directory, name)
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(file); err == nil {
			return file, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", ErrNoSegment
		case <-tick.C:
		}
	}
}

// Touch records that a client fetched part of a transcode, keeping it alive.
// It returns the output directory, or ok == false when the media has no
// transcode.
func (manager *Manager) Touch(mediaID int64) (directory string, found bool) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	job, found := manager.jobs[mediaID]
	if !found {
		return "", false
	}
	job.lastAccess = time.Now()
	return job.directory, true
}

// job returns the transcode of a media file, starting one if there is none
// (or the last one failed).
func (manager *Manager) job(mediaID int64, source string, format media.Format) (*job, error) {
	job, isNew, err := manager.reserve(mediaID)
	if err != nil || !isNew {
		return job, err
	}
	if err := manager.launch(job, mediaID, source, format); err != nil {
		job.err = err
		manager.mutex.Lock()
		if manager.jobs[mediaID] == job {
			delete(manager.jobs, mediaID)
		}
		job.finished = true
		manager.mutex.Unlock()
		close(job.done)
		return nil, err
	}
	return job, nil
}

// reserve finds the job for a media file or, capacity permitting, registers
// a new one. It holds the lock only for this bookkeeping.
func (manager *Manager) reserve(mediaID int64) (reserved *job, isNew bool, err error) {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()
	if job, exists := manager.jobs[mediaID]; exists {
		select {
		case <-job.done:
			if job.err != nil {
				delete(manager.jobs, mediaID) // failed earlier: try again
				break
			}
			job.lastAccess = time.Now()
			return job, false, nil
		default:
			job.lastAccess = time.Now()
			return job, false, nil
		}
	}
	running := 0
	for _, job := range manager.jobs {
		if !job.finished {
			running++
		}
	}
	if running >= manager.config.Max {
		return nil, false, ErrBusy
	}
	manager.seq++
	ctx, cancel := context.WithCancel(context.Background())
	reserved = &job{
		directory:  filepath.Join(manager.config.Directory, fmt.Sprintf("%d-%d", mediaID, manager.seq)),
		ctx:        ctx,
		cancel:     cancel,
		done:       make(chan struct{}),
		lastAccess: time.Now(),
	}
	manager.jobs[mediaID] = reserved
	return reserved, true, nil
}

// launch creates the job's folder and starts ffmpeg, outside the lock.
func (manager *Manager) launch(job *job, mediaID int64, source string, format media.Format) error {
	if err := os.MkdirAll(job.directory, 0o755); err != nil {
		return err
	}
	proc, err := manager.start.Start(job.ctx, ffmpegArguments(source, job.directory, format))
	if err != nil {
		job.cancel()
		os.RemoveAll(job.directory)
		return err
	}
	manager.log.Info("transcode started", "media", mediaID, "video", format.VideoCodec, "audio", format.AudioCodec)
	go manager.finish(job, proc)
	return nil
}

// finish waits for ffmpeg and records the outcome. A failure is not logged
// here: Playlist returns it to the request waiting for the playlist, and the
// handler reports it there.
func (manager *Manager) finish(job *job, proc Process) {
	err := proc.Wait()
	if err != nil && job.ctx.Err() == nil {
		job.err = err
	}
	manager.mutex.Lock()
	job.finished = true
	manager.mutex.Unlock()
	close(job.done)
}

// Run evicts idle and stale transcodes every 30 s until ctx ends. Then it
// stops every transcode, waits for each ffmpeg to exit and removes the
// output, so nothing is left running or on disk.
func (manager *Manager) Run(ctx context.Context) error {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			manager.stopAll()
			return nil
		case <-ticker.C:
			manager.sweep(time.Now())
		}
	}
}

// sweep removes a transcode that:
//   - is still running but nobody fetched from it for IdleTimeout (the
//     viewer left), or
//   - finished and was unused for KeepDone, or
//   - failed (a new request starts a fresh one).
func (manager *Manager) sweep(now time.Time) {
	manager.mutex.Lock()
	var evicted []*job
	for id, job := range manager.jobs {
		idle := now.Sub(job.lastAccess)
		stale := (!job.finished && idle > manager.config.IdleTimeout) ||
			(job.finished && (idle > manager.config.KeepDone || job.err != nil))
		if stale {
			delete(manager.jobs, id)
			evicted = append(evicted, job)
			manager.log.Info("transcode cleaned up", "media", id)
		}
	}
	manager.mutex.Unlock()
	for _, job := range evicted {
		go manager.remove(job)
	}
}

// stopAll stops every transcode and removes its output before returning.
func (manager *Manager) stopAll() {
	manager.mutex.Lock()
	all := make([]*job, 0, len(manager.jobs))
	for id, job := range manager.jobs {
		all = append(all, job)
		delete(manager.jobs, id)
	}
	manager.mutex.Unlock()
	var waitGroup sync.WaitGroup
	for _, job := range all {
		waitGroup.Go(func() { manager.remove(job) })
	}
	waitGroup.Wait()
}

// remove stops a job's ffmpeg, waits for it to exit and deletes its folder.
// The folder is the job's own, so a newer job for the same media is safe.
func (manager *Manager) remove(job *job) {
	job.cancel()
	<-job.done
	if err := os.RemoveAll(job.directory); err != nil {
		manager.log.Warn("could not remove transcode output", "dir", job.directory, "err", err)
	}
}

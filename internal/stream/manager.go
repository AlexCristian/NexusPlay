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

// [Manager] owns the transcode jobs: at most one per media item, at most
// [Config.Max] running at once. Create one with [NewManager] and run its
// clean-up loop with [Manager.Run]. It is safe for concurrent use.
type Manager struct {
	// [start] launches ffmpeg; [FFmpegStarter] in production.
	start Starter
	// [config] holds the settings, with defaults filled in.
	config Config
	// [log] records transcodes starting and being cleaned up.
	log *slog.Logger

	// [mutex] guards [jobs], [sequence], and each job's [job.lastAccess] and
	// [job.finished]. It is never held during file or process work.
	mutex sync.Mutex
	// [jobs] maps each media ID to its current transcode, running or finished.
	jobs map[int64]*job
	// [sequence] counts the transcodes started, to give each one its own
	// folder.
	sequence int
}

// [NewManager] creates the transcode manager and prepares its folder,
// deleting whatever a previous run left in it.
//
// Parameters:
//   - [start]: launches ffmpeg; [FFmpegStarter] in production.
//   - [config]: the folder and limits. An [Config.IdleTimeout] or
//     [Config.KeepDone] of 0 gets its default.
//   - [log]: where transcodes starting and being cleaned up are logged.
//
// Returns:
//   - [*Manager]: the manager, or [nil] if [error] is not [nil].
//   - [error]: [nil] on success; an error if [Config.Max] is below 1;
//     otherwise the error from emptying or creating [Config.Directory].
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

// [Playlist] starts or reuses the transcode of a media item and waits until
// its playlist lists a first segment, checking every 200 ms for up to 60
// seconds (player.js waits 70). A transcode that failed earlier is started
// again.
//
// Parameters:
//   - [ctx]: stops the wait, not the transcode, when cancelled.
//   - [mediaID]: the media item, which identifies its transcode.
//   - [source]: the original file's path.
//   - [format]: the original's codecs, which decide what is re-encoded.
//
// Returns:
//   - [string]: the path of the playlist file, "index.m3u8" in the
//     transcode's folder, or "" if [error] is not [nil].
//   - [error]: [nil] on success; [ErrBusy] at the limit of running
//     transcodes; [ctx]'s error if it ended first; ffmpeg's error if the
//     transcode failed or could not start; or an error if no segment
//     appeared within 60 seconds or the transcode finished without a
//     playlist.
func (manager *Manager) Playlist(ctx context.Context, mediaID int64, source string, format media.Format) (string, error) {
	job, err := manager.job(mediaID, source, format)
	if err != nil {
		return "", err
	}
	playlist := filepath.Join(job.directory, "index.m3u8")
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	poller := time.NewTicker(200 * time.Millisecond)
	defer poller.Stop()
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
		case <-poller.C:
		}
	}
}

// [Segment] finds one segment of a running or finished transcode, and counts
// as a use that keeps the transcode alive. ffmpeg lists a segment only once
// it is written, but a client resuming after a restart may ask ahead, so
// [Segment] waits up to 30 seconds, checking every 250 ms, for the file to
// appear. It never starts a transcode.
//
// Parameters:
//   - [ctx]: stops the wait when cancelled.
//   - [mediaID]: the media item whose transcode to look in.
//   - [name]: the segment's file name, such as "seg00012.ts". Anything that
//     is not a segment name (see [IsSegmentName]) is refused before the disk
//     is touched.
//
// Returns:
//   - [string]: the segment file's path, or "" if [error] is not [nil].
//   - [error]: [nil] on success; [ErrNoSegment] if [name] is not a segment
//     name, the media item has no transcode, or the segment did not appear
//     in time; [ctx]'s error if it ended first.
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
	poller := time.NewTicker(250 * time.Millisecond)
	defer poller.Stop()
	for {
		if _, err := os.Stat(file); err == nil {
			return file, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline.C:
			return "", ErrNoSegment
		case <-poller.C:
		}
	}
}

// [Touch] records that a client fetched part of a transcode, keeping it from
// being cleaned up as idle.
//
// Parameters:
//   - [mediaID]: the media item whose transcode was used.
//
// Returns:
//   - [string] ([directory]): the transcode's output folder, or "" if the
//     media item has no transcode.
//   - [bool] ([found]): [true] if the media item has a transcode, [false]
//     otherwise.
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

// [job] gets the transcode of a media item, starting one if there is none or
// the last one failed. A transcode that cannot start is removed again, so
// the next request tries afresh.
//
// Parameters:
//   - [mediaID]: the media item.
//   - [source]: the original file's path.
//   - [format]: the original's codecs.
//
// Returns:
//   - [*job]: the running or finished transcode, or [nil] if [error] is not
//     [nil].
//   - [error]: [nil] on success; [ErrBusy] at the limit of running
//     transcodes; otherwise the error from creating the folder or starting
//     ffmpeg.
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

// [reserve] finds the transcode of a media item or, if there is none (or it
// failed) and the limit allows, registers a new one with its own folder and
// context. It holds the lock only for this bookkeeping; the caller starts
// ffmpeg. Either way the transcode counts as used now.
//
// Parameters:
//   - [mediaID]: the media item.
//
// Returns:
//   - [*job] ([reserved]): the existing or new transcode, or [nil] if
//     [error] is not [nil].
//   - [bool] ([isNew]): [true] if the transcode was just registered and must
//     be launched with [Manager.launch], [false] if it already existed.
//   - [error]: [nil] on success, or [ErrBusy] if [Config.Max] transcodes are
//     already running.
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
	manager.sequence++
	ctx, cancel := context.WithCancel(context.Background())
	reserved = &job{
		directory:  filepath.Join(manager.config.Directory, fmt.Sprintf("%d-%d", mediaID, manager.sequence)),
		ctx:        ctx,
		cancel:     cancel,
		done:       make(chan struct{}),
		lastAccess: time.Now(),
	}
	manager.jobs[mediaID] = reserved
	return reserved, true, nil
}

// [launch] creates a new transcode's folder and starts ffmpeg, outside the
// lock, then waits for ffmpeg in the background with [Manager.finish]. If
// ffmpeg cannot start, the folder is deleted again.
//
// Parameters:
//   - [job]: the transcode registered by [Manager.reserve].
//   - [mediaID]: the media item, for the log.
//   - [source]: the original file's path.
//   - [format]: the original's codecs.
//
// Returns:
//   - [error]: [nil] on success, or the error from creating the folder or
//     starting ffmpeg.
func (manager *Manager) launch(job *job, mediaID int64, source string, format media.Format) error {
	if err := os.MkdirAll(job.directory, 0o755); err != nil {
		return err
	}
	process, err := manager.start.Start(job.ctx, ffmpegArguments(source, job.directory, format))
	if err != nil {
		job.cancel()
		os.RemoveAll(job.directory)
		return err
	}
	manager.log.Info("transcode started", "media", mediaID, "video", format.VideoCodec, "audio", format.AudioCodec)
	go manager.finish(job, process)
	return nil
}

// [finish] waits for ffmpeg to exit, records whether it failed, and marks
// the transcode finished. A failure is not logged here: if a
// [Manager.Playlist] call is waiting, it returns the failure; a failure
// after that is not reported anywhere, and the next request starts a fresh
// transcode. Being stopped on purpose is not a failure.
//
// Parameters:
//   - [job]: the transcode.
//   - [process]: its ffmpeg.
func (manager *Manager) finish(job *job, process Process) {
	err := process.Wait()
	if err != nil && job.ctx.Err() == nil {
		job.err = err
	}
	manager.mutex.Lock()
	job.finished = true
	manager.mutex.Unlock()
	close(job.done)
}

// [Run] cleans up idle and stale transcodes every 30 seconds (see
// [Manager.sweep]) until [ctx] ends. Then it stops every transcode still
// registered, waits for each ffmpeg to exit and deletes its output.
// Transcodes an earlier sweep is still removing are not waited for. It blocks, so call it in its own goroutine.
//
// Parameters:
//   - [ctx]: stops the manager when cancelled.
//
// Returns:
//   - [error]: always [nil], once [ctx] has ended and everything is cleaned
//     up.
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

// [sweep] removes, in the background, every transcode that:
//   - is still running but nobody fetched from it for [Config.IdleTimeout],
//     because the viewer left;
//   - finished and was unused for [Config.KeepDone]; or
//   - failed, since a new request starts a fresh one.
//
// Parameters:
//   - [now]: the current time, which tests set.
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

// [stopAll] stops every transcode and deletes its output, and returns once
// all of them are gone.
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

// [remove] stops a transcode's ffmpeg, waits for it to exit and deletes its
// folder. The folder is the transcode's own, so a newer transcode of the
// same media item is not affected. A folder that cannot be deleted is
// logged.
//
// Parameters:
//   - [job]: the transcode, already taken out of [Manager.jobs].
func (manager *Manager) remove(job *job) {
	job.cancel()
	<-job.done
	if err := os.RemoveAll(job.directory); err != nil {
		manager.log.Warn("could not remove transcode output", "dir", job.directory, "err", err)
	}
}

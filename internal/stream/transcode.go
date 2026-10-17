// Package stream runs on-demand HLS transcodes for files browsers cannot
// play directly, with a cap on how many run at once, reuse while a file is
// being watched, and eviction once nobody is watching.
//
// Output is an HLS "event" playlist (-hls_playlist_type event): ffmpeg
// appends segments while it runs and writes #EXT-X-ENDLIST only at the end,
// so playback can start after the first segment instead of after the whole
// transcode. Players treat an open event playlist like live TV and would
// join near its end, so player.js sets hls.js startPosition explicitly; that
// is also why Playlist waits for the first segment before answering.
// Keyframes are forced every 6 s (= -hls_time) when re-encoding, so segments
// split cleanly.
package stream

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/media"
)

// Errors the Manager returns.
var (
	// ErrBusy means the concurrent transcode limit is reached. Callers
	// should ask the client to retry later.
	ErrBusy = errors.New("too many transcodes running")
	// ErrNoSegment means a requested segment did not appear in time.
	ErrNoSegment = errors.New("segment not available")
)

// Starter launches ffmpeg. FFmpegStarter adapts ffmpeg.Tools; tests use a fake.
type Starter interface {
	Start(ctx context.Context, args []string) (Process, error)
}

// Process is a running ffmpeg.
type Process interface {
	// Wait blocks until the process exits. It returns ctx.Err() when the
	// process was cancelled through the context given to Start.
	Wait() error
}

// FFmpegStarter returns a Starter that runs the real ffmpeg.
func FFmpegStarter(t ffmpeg.Tools) Starter { return ffmpegStarter{t} }

type ffmpegStarter struct{ tools ffmpeg.Tools }

func (s ffmpegStarter) Start(ctx context.Context, args []string) (Process, error) {
	return s.tools.Start(ctx, args)
}

// Config tunes a Manager. Zero durations use the defaults.
type Config struct {
	Dir         string        // scratch folder for HLS output; emptied by NewManager
	Max         int           // transcodes allowed to run at once
	IdleTimeout time.Duration // stop a running transcode nobody fetched from for this long (default 3 min)
	KeepDone    time.Duration // keep a finished transcode for this long after its last use (default 6 h)
}

// Manager owns the transcode jobs. It is safe for concurrent use. Build it
// with NewManager and run its janitor with Run.
type Manager struct {
	start Starter
	cfg   Config
	log   *slog.Logger

	mu   sync.Mutex // guards jobs and seq; never held during file or process work
	jobs map[int64]*job
	seq  int
}

// job is one transcode. Every job has its own directory, so cleaning up an
// old job never touches the output of a newer one for the same media.
type job struct {
	dir        string
	ctx        context.Context // the job's lifetime; cancelling it kills ffmpeg
	cancel     context.CancelFunc
	done       chan struct{} // closed when ffmpeg has exited (or failed to start)
	err        error         // why it failed; set before done is closed
	lastAccess time.Time     // guarded by Manager.mu
	finished   bool          // guarded by Manager.mu
}

// NewManager prepares the transcode folder, clearing leftovers from a
// previous run.
func NewManager(start Starter, cfg Config, log *slog.Logger) (*Manager, error) {
	if cfg.IdleTimeout == 0 {
		cfg.IdleTimeout = 3 * time.Minute
	}
	if cfg.KeepDone == 0 {
		cfg.KeepDone = 6 * time.Hour
	}
	if cfg.Max < 1 {
		return nil, fmt.Errorf("stream: Max must be at least 1")
	}
	if err := os.RemoveAll(cfg.Dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, err
	}
	return &Manager{start: start, cfg: cfg, log: log, jobs: map[int64]*job{}}, nil
}

// Playlist starts (or reuses) the transcode of a media file and waits until
// its playlist lists a first segment, up to 60 s (player.js allows 70 s). It
// returns the path of the playlist file, or ErrBusy at the concurrency limit.
func (m *Manager) Playlist(ctx context.Context, mediaID int64, src string, f media.Format) (string, error) {
	j, err := m.job(mediaID, src, f)
	if err != nil {
		return "", err
	}
	playlist := filepath.Join(j.dir, "index.m3u8")
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
		case <-j.done:
			if j.err != nil {
				return "", j.err
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

// segmentName matches the files an HLS transcode writes.
var segmentName = regexp.MustCompile(`^seg\d{5}\.ts$`)

// IsSegmentName reports whether name is a segment file name. Anything else
// (paths, the playlist, other files) is refused before touching the disk.
func IsSegmentName(name string) bool { return segmentName.MatchString(name) }

// Segment returns the path of a segment of a running or finished transcode,
// keeping the transcode alive. ffmpeg lists a segment only once it is
// written, but a client resuming after a restart may ask ahead, so Segment
// waits up to 30 s for the file. It returns ErrNoSegment if the media has no
// transcode or the segment does not appear.
func (m *Manager) Segment(ctx context.Context, mediaID int64, name string) (string, error) {
	if !IsSegmentName(name) {
		return "", ErrNoSegment
	}
	dir, ok := m.Touch(mediaID)
	if !ok {
		return "", ErrNoSegment
	}
	file := filepath.Join(dir, name)
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
func (m *Manager) Touch(mediaID int64) (dir string, ok bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[mediaID]
	if !ok {
		return "", false
	}
	j.lastAccess = time.Now()
	return j.dir, true
}

// job returns the transcode of a media file, starting one if there is none
// (or the last one failed).
func (m *Manager) job(mediaID int64, src string, f media.Format) (*job, error) {
	j, isNew, err := m.reserve(mediaID)
	if err != nil || !isNew {
		return j, err
	}
	if err := m.launch(j, mediaID, src, f); err != nil {
		j.err = err
		m.mu.Lock()
		if m.jobs[mediaID] == j {
			delete(m.jobs, mediaID)
		}
		j.finished = true
		m.mu.Unlock()
		close(j.done)
		return nil, err
	}
	return j, nil
}

// reserve finds the job for a media file or, capacity permitting, registers
// a new one. It holds the lock only for this bookkeeping.
func (m *Manager) reserve(mediaID int64) (j *job, isNew bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[mediaID]; ok {
		select {
		case <-j.done:
			if j.err != nil {
				delete(m.jobs, mediaID) // failed earlier: try again
				break
			}
			j.lastAccess = time.Now()
			return j, false, nil
		default:
			j.lastAccess = time.Now()
			return j, false, nil
		}
	}
	running := 0
	for _, j := range m.jobs {
		if !j.finished {
			running++
		}
	}
	if running >= m.cfg.Max {
		return nil, false, ErrBusy
	}
	m.seq++
	ctx, cancel := context.WithCancel(context.Background())
	j = &job{
		dir:        filepath.Join(m.cfg.Dir, fmt.Sprintf("%d-%d", mediaID, m.seq)),
		ctx:        ctx,
		cancel:     cancel,
		done:       make(chan struct{}),
		lastAccess: time.Now(),
	}
	m.jobs[mediaID] = j
	return j, true, nil
}

// launch creates the job's folder and starts ffmpeg, outside the lock.
func (m *Manager) launch(j *job, mediaID int64, src string, f media.Format) error {
	if err := os.MkdirAll(j.dir, 0o755); err != nil {
		return err
	}
	proc, err := m.start.Start(j.ctx, ffmpegArgs(src, j.dir, f))
	if err != nil {
		j.cancel()
		os.RemoveAll(j.dir)
		return err
	}
	m.log.Info("transcode started", "media", mediaID, "video", f.VideoCodec, "audio", f.AudioCodec)
	go m.finish(j, proc)
	return nil
}

// finish waits for ffmpeg and records the outcome. A failure is not logged
// here: Playlist returns it to the request waiting for the playlist, and the
// handler reports it there.
func (m *Manager) finish(j *job, proc Process) {
	err := proc.Wait()
	if err != nil && j.ctx.Err() == nil {
		j.err = err
	}
	m.mu.Lock()
	j.finished = true
	m.mu.Unlock()
	close(j.done)
}

// ffmpegArgs builds an HLS transcode. Browser-safe H.264 (8-bit 4:2:0) is
// copied as-is, a cheap remux for MKV files; anything else, including 10-bit
// H.264, is encoded to H.264. Audio is copied when it is AAC and otherwise
// made stereo AAC, which every browser plays.
func ffmpegArgs(src, dir string, f media.Format) []string {
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-i", src,
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn"}
	if f.VideoCodec == "h264" && media.BrowserSafeH264(f.PixFmt) {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "21",
			"-pix_fmt", "yuv420p", "-profile:v", "high", "-force_key_frames", "expr:gte(t,n_forced*6)")
	}
	if f.AudioCodec == "aac" {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "aac", "-b:a", "160k", "-ac", "2")
	}
	return append(args,
		"-f", "hls", "-hls_time", "6", "-hls_playlist_type", "event",
		"-hls_segment_filename", filepath.Join(dir, "seg%05d.ts"),
		filepath.Join(dir, "index.m3u8"))
}

// Run evicts idle and stale transcodes every 30 s until ctx ends. Then it
// stops every transcode, waits for each ffmpeg to exit and removes the
// output, so nothing is left running or on disk.
func (m *Manager) Run(ctx context.Context) error {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			m.stopAll()
			return nil
		case <-t.C:
			m.sweep(time.Now())
		}
	}
}

// sweep removes a transcode that:
//   - is still running but nobody fetched from it for IdleTimeout (the
//     viewer left), or
//   - finished and was unused for KeepDone, or
//   - failed (a new request starts a fresh one).
func (m *Manager) sweep(now time.Time) {
	m.mu.Lock()
	var evicted []*job
	for id, j := range m.jobs {
		idle := now.Sub(j.lastAccess)
		stale := (!j.finished && idle > m.cfg.IdleTimeout) ||
			(j.finished && (idle > m.cfg.KeepDone || j.err != nil))
		if stale {
			delete(m.jobs, id)
			evicted = append(evicted, j)
			m.log.Info("transcode cleaned up", "media", id)
		}
	}
	m.mu.Unlock()
	for _, j := range evicted {
		go m.remove(j)
	}
}

// stopAll stops every transcode and removes its output before returning.
func (m *Manager) stopAll() {
	m.mu.Lock()
	all := make([]*job, 0, len(m.jobs))
	for id, j := range m.jobs {
		all = append(all, j)
		delete(m.jobs, id)
	}
	m.mu.Unlock()
	var wg sync.WaitGroup
	for _, j := range all {
		wg.Go(func() { m.remove(j) })
	}
	wg.Wait()
}

// remove stops a job's ffmpeg, waits for it to exit and deletes its folder.
// The folder is the job's own, so a newer job for the same media is safe.
func (m *Manager) remove(j *job) {
	j.cancel()
	<-j.done
	if err := os.RemoveAll(j.dir); err != nil {
		m.log.Warn("could not remove transcode output", "dir", j.dir, "err", err)
	}
}

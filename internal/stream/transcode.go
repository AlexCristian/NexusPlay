// Package stream decides how a file is played and runs on-demand HLS
// transcodes with ffmpeg for files browsers cannot play directly.
package stream

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
)

// ErrBusy means the concurrent transcode limit is reached.
var ErrBusy = errors.New("too many transcodes running")

// idleTimeout stops a transcode nobody has requested a segment from for this long.
const idleTimeout = 3 * time.Minute

// keepFinished is how long a completed transcode stays cached on disk.
const keepFinished = 6 * time.Hour

// Manager runs at most Max ffmpeg processes and reuses their output while it
// is being watched.
type Manager struct {
	Tools ffmpeg.Tools
	Dir   string
	Max   int
	Log   *slog.Logger

	mu   sync.Mutex
	jobs map[int64]*job
}

type job struct {
	dir        string
	cancel     context.CancelFunc
	done       chan struct{}
	err        error
	finished   bool
	lastAccess time.Time
}

// NewManager prepares the transcode directory, clearing leftovers from a
// previous run.
func NewManager(tools ffmpeg.Tools, dir string, max int, log *slog.Logger) (*Manager, error) {
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Manager{Tools: tools, Dir: dir, Max: max, Log: log, jobs: map[int64]*job{}}, nil
}

// Playlist starts (or reuses) a transcode for mediaID and waits until its
// playlist has a first segment. It returns the directory holding the output.
func (m *Manager) Playlist(ctx context.Context, mediaID int64, src, videoCodec, audioCodec string) (string, error) {
	j, err := m.start(mediaID, src, videoCodec, audioCodec)
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
			return j.dir, nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-j.done:
			if j.err != nil {
				return "", j.err
			}
			// Finished before the first poll saw a segment (a short file).
			if _, err := os.Stat(playlist); err != nil {
				return "", fmt.Errorf("transcode produced no playlist")
			}
			return j.dir, nil
		case <-deadline.C:
			return "", fmt.Errorf("transcode did not start in time")
		case <-tick.C:
		}
	}
}

// Touch records that a client fetched part of a transcode, keeping it alive.
// It returns the output directory, or "" when no transcode is running.
func (m *Manager) Touch(mediaID int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[mediaID]
	if !ok {
		return ""
	}
	j.lastAccess = time.Now()
	return j.dir
}

func (m *Manager) start(mediaID int64, src, videoCodec, audioCodec string) (*job, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if j, ok := m.jobs[mediaID]; ok {
		select {
		case <-j.done:
			if j.err == nil {
				j.lastAccess = time.Now()
				return j, nil
			}
			delete(m.jobs, mediaID) // failed earlier: try again
		default:
			j.lastAccess = time.Now()
			return j, nil
		}
	}
	running := 0
	for _, j := range m.jobs {
		if !j.finished {
			running++
		}
	}
	if running >= m.Max {
		return nil, ErrBusy
	}

	dir := filepath.Join(m.Dir, strconv.FormatInt(mediaID, 10))
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	j := &job{dir: dir, cancel: cancel, done: make(chan struct{}), lastAccess: time.Now()}
	m.jobs[mediaID] = j

	proc, err := m.Tools.Start(ctx, ffmpegArgs(src, dir, videoCodec, audioCodec))
	if err != nil {
		cancel()
		delete(m.jobs, mediaID)
		return nil, err
	}
	m.Log.Info("transcode started", "media", mediaID, "video", videoCodec, "audio", audioCodec)
	go func() {
		err := proc.Wait()
		m.mu.Lock()
		j.finished = true
		if err != nil && ctx.Err() == nil {
			// Not logged here: Playlist returns it to the request that waits
			// for the playlist, and the handler reports it there.
			j.err = err
		}
		m.mu.Unlock()
		close(j.done)
	}()
	return j, nil
}

// ffmpegArgs builds an HLS transcode. H.264 video is copied as-is (a cheap
// remux, e.g. for MKV files); anything else is encoded to H.264. Audio is
// always made stereo AAC, which every browser plays.
func ffmpegArgs(src, dir, videoCodec, audioCodec string) []string {
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-i", src,
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn"}
	if videoCodec == "h264" {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-crf", "21",
			"-pix_fmt", "yuv420p", "-profile:v", "high", "-force_key_frames", "expr:gte(t,n_forced*6)")
	}
	if audioCodec == "aac" {
		args = append(args, "-c:a", "copy")
	} else {
		args = append(args, "-c:a", "aac", "-b:a", "160k", "-ac", "2")
	}
	return append(args,
		"-f", "hls", "-hls_time", "6", "-hls_playlist_type", "event",
		"-hls_segment_filename", filepath.Join(dir, "seg%05d.ts"),
		filepath.Join(dir, "index.m3u8"))
}

// Janitor stops abandoned transcodes and removes stale output until ctx ends.
func (m *Manager) Janitor(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			m.stopAll()
			return
		case <-t.C:
			m.sweep(time.Now())
		}
	}
}

func (m *Manager) sweep(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, j := range m.jobs {
		idle := now.Sub(j.lastAccess)
		if (!j.finished && idle > idleTimeout) || (j.finished && (idle > keepFinished || j.err != nil)) {
			j.cancel()
			delete(m.jobs, id)
			dir := j.dir
			done := j.done
			go func() {
				<-done
				os.RemoveAll(dir)
			}()
			m.Log.Info("transcode cleaned up", "media", id)
		}
	}
}

func (m *Manager) stopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, j := range m.jobs {
		j.cancel()
		delete(m.jobs, id)
	}
}

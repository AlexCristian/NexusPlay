package stream

import (
	"context"
	"errors"
	"path/filepath"
	"regexp"
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

// FFmpegStarter returns a Starter that runs the real ffmpeg.
func FFmpegStarter(tools ffmpeg.Tools) Starter { return ffmpegStarter{tools} }

type ffmpegStarter struct{ tools ffmpeg.Tools }

func (starter ffmpegStarter) Start(ctx context.Context, arguments []string) (Process, error) {
	return starter.tools.Start(ctx, arguments)
}

// job is one transcode. Every job has its own directory, so cleaning up an
// old job never touches the output of a newer one for the same media.
type job struct {
	directory  string
	ctx        context.Context // the job's lifetime; cancelling it kills ffmpeg
	cancel     context.CancelFunc
	done       chan struct{} // closed when ffmpeg has exited (or failed to start)
	err        error         // why it failed; set before done is closed
	lastAccess time.Time     // guarded by Manager.mu
	finished   bool          // guarded by Manager.mu
}

// segmentName matches the files an HLS transcode writes.
var segmentName = regexp.MustCompile(`^seg\d{5}\.ts$`)

// IsSegmentName reports whether name is a segment file name. Anything else
// (paths, the playlist, other files) is refused before touching the disk.
func IsSegmentName(name string) bool { return segmentName.MatchString(name) }

// ffmpegArguments builds an HLS transcode. Browser-safe H.264 (8-bit 4:2:0) is
// copied as-is, a cheap remux for MKV files; anything else, including 10-bit
// H.264, is encoded to H.264. Audio is copied when it is AAC and otherwise
// made stereo AAC, which every browser plays.
func ffmpegArguments(source, directory string, format media.Format) []string {
	arguments := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-i", source,
		"-map", "0:v:0", "-map", "0:a:0?", "-sn", "-dn"}
	if format.VideoCodec == "h264" && media.BrowserSafeH264(format.PixelFormat) {
		arguments = append(arguments, "-c:v", "copy")
	} else {
		arguments = append(arguments, "-c:v", "libx264", "-preset", "veryfast", "-crf", "21",
			"-pix_fmt", "yuv420p", "-profile:v", "high", "-force_key_frames", "expr:gte(t,n_forced*6)")
	}
	if format.AudioCodec == "aac" {
		arguments = append(arguments, "-c:a", "copy")
	} else {
		arguments = append(arguments, "-c:a", "aac", "-b:a", "160k", "-ac", "2")
	}
	return append(arguments,
		"-f", "hls", "-hls_time", "6", "-hls_playlist_type", "event",
		"-hls_segment_filename", filepath.Join(directory, "seg%05d.ts"),
		filepath.Join(directory, "index.m3u8"))
}

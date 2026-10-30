package stream

import (
	"context"
	"path/filepath"
	"regexp"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/media"
)

// [FFmpegStarter] adapts [ffmpeg.Tools] to [Starter], to run the real ffmpeg.
//
// Parameters:
//   - [tools]: where ffmpeg is.
//
// Returns:
//   - [Starter]: a starter that runs [ffmpeg.Tools.Start].
func FFmpegStarter(tools ffmpeg.Tools) Starter { return ffmpegStarter{tools} }

// [ffmpegStarter] is the [Starter] [FFmpegStarter] returns.
type ffmpegStarter struct {
	// [tools] is where ffmpeg is.
	tools ffmpeg.Tools
}

// [Start] launches ffmpeg with [ffmpeg.Tools.Start].
//
// Parameters:
//   - [ctx]: kills ffmpeg when cancelled.
//   - [arguments]: ffmpeg's command-line arguments.
//
// Returns:
//   - [Process]: the running ffmpeg.
//   - [error]: [nil] on success, or the error from [ffmpeg.Tools.Start].
func (starter ffmpegStarter) Start(ctx context.Context, arguments []string) (Process, error) {
	return starter.tools.Start(ctx, arguments)
}

// [job] is one transcode. Every job has its own folder, so cleaning up an old
// job never touches the output of a newer one for the same media item.
type job struct {
	// [directory] is the transcode's own output folder.
	directory string
	// [ctx] is the transcode's lifetime; cancelling it kills ffmpeg.
	ctx context.Context
	// [cancel] cancels [ctx], stopping the transcode.
	cancel context.CancelFunc
	// [done] is closed when ffmpeg has exited, or failed to start.
	done chan struct{}
	// [err] is why the transcode failed, or [nil]. It is set before [done] is
	// closed and not changed after, so read it only once [done] is closed.
	err error
	// [lastAccess] is when a client last used the transcode. Guarded by
	// [Manager.mutex].
	lastAccess time.Time
	// [finished] is [true] once ffmpeg has exited or failed to start. Guarded by
	// [Manager.mutex].
	finished bool
}

// [segmentName] matches the segment file names an HLS transcode writes,
// "seg" and five digits, such as "seg00012.ts".
var segmentName = regexp.MustCompile(`^seg\d{5}\.ts$`)

// [IsSegmentName] reports whether a name is a transcode's segment file
// name. Anything else (paths, the playlist, other files) is refused before
// the disk is touched.
//
// Parameters:
//   - [name]: the requested file name.
//
// Returns:
//   - [bool]: [true] for names such as "seg00012.ts", [false] otherwise.
func IsSegmentName(name string) bool { return segmentName.MatchString(name) }

// [ffmpegArguments] builds the ffmpeg arguments for an HLS transcode of the
// first video and first audio stream (if any), without subtitles or data.
// Browser-safe H.264 (8-bit 4:2:0) is copied as it is, a cheap remux for MKV
// files; anything else, including 10-bit H.264, is encoded to H.264. Audio
// is copied when it is AAC and otherwise made stereo AAC at 160 kbit/s,
// which every browser plays. Segments are 6 seconds long.
//
// Parameters:
//   - [source]: the original file's path.
//   - [directory]: the transcode's folder, where "index.m3u8" and the
//     segments are written.
//   - [format]: the original's codecs.
//
// Returns:
//   - [[]string]: ffmpeg's arguments, without the program name.
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

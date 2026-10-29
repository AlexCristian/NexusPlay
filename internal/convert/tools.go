package convert

import (
	"context"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
)

// [Tools] runs ffprobe and ffmpeg; [ffmpeg.Tools] is the real one.
type Tools interface {
	// [Probe] inspects a media file with ffprobe.
	//
	// Parameters:
	//   - [ctx]: kills ffprobe when cancelled.
	//   - [file]: the path of the file to inspect.
	//
	// Returns:
	//   - [ffmpeg.Probe]: the file's streams and length.
	//   - [error]: [nil] on success, or the error described at
	//     [ffmpeg.Tools.Probe].
	Probe(ctx context.Context, file string) (ffmpeg.Probe, error)
	// [Run] runs ffmpeg to completion.
	//
	// Parameters:
	//   - [ctx]: kills ffmpeg when cancelled.
	//   - [arguments]: ffmpeg's command-line arguments.
	//   - [options]: progress reporting and priority.
	//
	// Returns:
	//   - [error]: [nil] on success, or the error described at
	//     [ffmpeg.Tools.Run].
	Run(ctx context.Context, arguments []string, options ffmpeg.RunOptions) error
}

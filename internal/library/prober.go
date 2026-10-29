package library

import (
	"context"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
)

// [Prober] reads a media file's streams; [ffmpeg.Tools] is the real one.
type Prober interface {
	// [Probe] inspects a media file with ffprobe.
	//
	// Parameters:
	//   - [ctx]: cancels the probe.
	//   - [file]: the file's path.
	//
	// Returns:
	//   - [ffmpeg.Probe]: the file's streams and length.
	//   - [error]: as [ffmpeg.Tools.Probe] describes.
	Probe(ctx context.Context, file string) (ffmpeg.Probe, error)
}

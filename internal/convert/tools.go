package convert

import (
	"context"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
)

// Tools runs ffprobe and ffmpeg; ffmpeg.Tools is the real one.
type Tools interface {
	Probe(ctx context.Context, file string) (ffmpeg.Probe, error)
	Run(ctx context.Context, arguments []string, options ffmpeg.RunOptions) error
}

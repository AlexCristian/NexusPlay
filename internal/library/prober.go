package library

import (
	"context"
	_ "embed"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
)

// Prober reads a media file's streams; ffmpeg.Tools is the real one.
type Prober interface {
	Probe(ctx context.Context, file string) (ffmpeg.Probe, error)
}

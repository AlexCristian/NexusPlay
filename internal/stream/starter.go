package stream

import (
	"context"
)

// Starter launches ffmpeg. FFmpegStarter adapts ffmpeg.Tools; tests use a fake.
type Starter interface {
	Start(ctx context.Context, arguments []string) (Process, error)
}

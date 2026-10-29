package stream

import (
	"context"
)

// [Starter] launches ffmpeg. [FFmpegStarter] runs the real one; tests use a
// fake.
type Starter interface {
	// [Start] launches ffmpeg without waiting for it.
	//
	// Parameters:
	//   - [ctx]: kills ffmpeg when cancelled.
	//   - [arguments]: ffmpeg's command-line arguments.
	//
	// Returns:
	//   - [Process]: the running ffmpeg, whose [Process.Wait] must be called.
	//   - [error]: [nil] on success, or why ffmpeg could not be started.
	Start(ctx context.Context, arguments []string) (Process, error)
}

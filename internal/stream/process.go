package stream

// Process is a running ffmpeg.
type Process interface {
	// Wait blocks until the process exits. It returns ctx.Err() when the
	// process was cancelled through the context given to Start.
	Wait() error
}

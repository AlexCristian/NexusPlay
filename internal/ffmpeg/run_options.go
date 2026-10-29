package ffmpeg

// RunOptions tune a Run call. The zero value runs ffmpeg plainly.
type RunOptions struct {
	// OnProgress, if set, receives how many seconds of output ffmpeg has
	// written so far. Run adds "-progress pipe:1 -nostats" to read it.
	OnProgress func(seconds float64)
	// LowPriority runs ffmpeg at a lower CPU priority so it yields to
	// playback (Linux only; ignored elsewhere).
	LowPriority bool
}

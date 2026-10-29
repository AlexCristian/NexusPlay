package ffmpeg

// [RunOptions] tunes a [Tools.Run] call. The zero value runs ffmpeg plainly.
type RunOptions struct {
	// [OnProgress], if set, is called with how many seconds of output ffmpeg has
	// written so far, on the goroutine that called [Tools.Run]. [Tools.Run] adds
	// "-progress pipe:1 -nostats" to ffmpeg's arguments to read it.
	OnProgress func(seconds float64)
	// [LowPriority] runs ffmpeg at a lower CPU priority so it yields to playback.
	// It works on Linux only and is ignored elsewhere.
	LowPriority bool
}

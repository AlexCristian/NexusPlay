package ffmpeg

import (
	"fmt"
)

// ExitError is a program that ran and failed. LastLine is the last line it
// wrote to stderr, which for ffmpeg is nearly always the actual reason.
type ExitError struct {
	Program  string // "ffmpeg" or "ffprobe"
	Err      error  // usually *exec.ExitError
	LastLine string
}

func (exitError *ExitError) Error() string {
	if exitError.LastLine == "" {
		return fmt.Sprintf("%s: %v", exitError.Program, exitError.Err)
	}
	return fmt.Sprintf("%s: %v: %s", exitError.Program, exitError.Err, exitError.LastLine)
}

func (exitError *ExitError) Unwrap() error { return exitError.Err }

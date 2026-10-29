package ffmpeg

import (
	"fmt"
)

// [ExitError] reports a program that ran and failed, as opposed to one that
// was cancelled (which is reported as the context's error instead).
type ExitError struct {
	// [Program] is the program that failed: "ffmpeg" or "ffprobe".
	Program string
	// [Err] is why it failed, usually an [*exec.ExitError] with its exit status.
	Err error
	// [LastLine] is the last line the program wrote to stderr, which for ffmpeg
	// is nearly always the actual reason, or "" if it wrote nothing.
	LastLine string
}

// [Error] describes the failure for logs and messages.
//
// Returns:
//   - [string]: the program, its error and its last stderr line, such as
//     "ffmpeg: exit status 1: Invalid data found when processing input".
func (exitError *ExitError) Error() string {
	if exitError.LastLine == "" {
		return fmt.Sprintf("%s: %v", exitError.Program, exitError.Err)
	}
	return fmt.Sprintf("%s: %v: %s", exitError.Program, exitError.Err, exitError.LastLine)
}

// [Unwrap] gives the underlying error, so [errors.As] can find the
// [*exec.ExitError] inside.
//
// Returns:
//   - [error]: [ExitError.Err].
func (exitError *ExitError) Unwrap() error { return exitError.Err }

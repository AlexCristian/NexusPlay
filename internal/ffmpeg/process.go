package ffmpeg

import (
	"context"
	"os/exec"
)

// [Process] is an ffmpeg run started with [Tools.Start] that runs in the
// background. Call [Process.Wait] once to collect its result.
type Process struct {
	// [command] is the running program.
	command *exec.Cmd
	// [ctx] is the context it was started with; its end kills the program.
	ctx context.Context
	// [stderr] captures the end of the program's stderr, for errors.
	stderr *lastLineWriter
}

// [Wait] blocks until the process exits. Call it exactly once.
//
// Returns:
//   - [error]: [nil] if ffmpeg succeeded; [ctx]'s error
//     ([context.Canceled] or [context.DeadlineExceeded]) if the process was
//     killed because its context ended; an [*ExitError] if ffmpeg failed.
func (process *Process) Wait() error {
	return exitError("ffmpeg", process.ctx, process.command.Wait(), process.stderr)
}

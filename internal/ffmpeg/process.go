package ffmpeg

import (
	"context"
	"os/exec"
)

// Process is an ffmpeg started with Start that runs in the background.
type Process struct {
	command *exec.Cmd
	ctx     context.Context
	stderr  *lastLineWriter
}

// Wait waits for the process to exit. Like Run, it returns ctx.Err() when
// the process was cancelled and an *ExitError when ffmpeg failed.
func (process *Process) Wait() error {
	return exitError("ffmpeg", process.ctx, process.command.Wait(), process.stderr)
}

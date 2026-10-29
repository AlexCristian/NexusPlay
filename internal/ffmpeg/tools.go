package ffmpeg

import (
	"context"
	"fmt"
	"io"
)

// Tools locates the ffmpeg and ffprobe binaries (names on PATH or paths).
// The zero value is not usable; set both fields.
type Tools struct {
	FFmpeg  string
	FFprobe string
}

// Run runs ffmpeg to completion. It returns ctx.Err() when ctx was cancelled
// (the process is killed) and an *ExitError when ffmpeg failed.
func (tools Tools) Run(ctx context.Context, arguments []string, options RunOptions) error {
	if options.OnProgress != nil {
		arguments = append([]string{"-progress", "pipe:1", "-nostats"}, arguments...)
	}
	cmd := command(ctx, tools.FFmpeg, arguments...)
	stderr := &lastLineWriter{}
	cmd.Stderr = stderr
	var stdout io.Reader
	if options.OnProgress != nil {
		pipe, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		stdout = pipe
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting ffmpeg: %w", err)
	}
	if options.LowPriority {
		lowerPriority(cmd.Process.Pid)
	}
	if stdout != nil {
		readProgress(stdout, options.OnProgress)
	}
	return exitError("ffmpeg", ctx, cmd.Wait(), stderr)
}

// Start launches ffmpeg and returns without waiting for it. Cancelling ctx
// kills the process. The caller must call Wait.
func (tools Tools) Start(ctx context.Context, arguments []string) (*Process, error) {
	cmd := command(ctx, tools.FFmpeg, arguments...)
	process := &Process{cmd: cmd, ctx: ctx, stderr: &lastLineWriter{}}
	cmd.Stderr = process.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting ffmpeg: %w", err)
	}
	return process, nil
}

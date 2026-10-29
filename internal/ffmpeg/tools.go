package ffmpeg

import (
	"context"
	"fmt"
	"io"
)

// [Tools] locates the ffmpeg and ffprobe programs. The zero value is not
// usable; set both fields.
type Tools struct {
	// [FFmpeg] is the ffmpeg program: a name looked up on the PATH, or a path.
	FFmpeg string
	// [FFprobe] is the ffprobe program: a name looked up on the PATH, or a path.
	FFprobe string
}

// [Run] runs ffmpeg to completion, killing it if [ctx] ends first.
//
// Parameters:
//   - [ctx]: kills ffmpeg when cancelled.
//   - [arguments]: ffmpeg's command-line arguments, without the program
//     name.
//   - [options]: progress reporting and priority; the zero value for neither.
//
// Returns:
//   - [error]: [nil] if ffmpeg succeeded; [ctx]'s error
//     ([context.Canceled] or [context.DeadlineExceeded]) if ffmpeg was
//     killed because [ctx] ended; an [*ExitError] if ffmpeg failed; or an
//     error wrapping the cause if ffmpeg could not be started.
func (tools Tools) Run(ctx context.Context, arguments []string, options RunOptions) error {
	if options.OnProgress != nil {
		arguments = append([]string{"-progress", "pipe:1", "-nostats"}, arguments...)
	}
	execCommand := command(ctx, tools.FFmpeg, arguments...)
	stderr := &lastLineWriter{}
	execCommand.Stderr = stderr
	var stdout io.Reader
	if options.OnProgress != nil {
		pipe, err := execCommand.StdoutPipe()
		if err != nil {
			return err
		}
		stdout = pipe
	}
	if err := execCommand.Start(); err != nil {
		return fmt.Errorf("starting ffmpeg: %w", err)
	}
	if options.LowPriority {
		lowerPriority(execCommand.Process.Pid)
	}
	if stdout != nil {
		readProgress(stdout, options.OnProgress)
	}
	return exitError("ffmpeg", ctx, execCommand.Wait(), stderr)
}

// [Start] launches ffmpeg and returns without waiting for it to finish. The
// caller must call [Process.Wait] once.
//
// Parameters:
//   - [ctx]: kills ffmpeg when cancelled, at any point until it exits.
//   - [arguments]: ffmpeg's command-line arguments, without the program
//     name.
//
// Returns:
//   - [*Process]: the running process, or [nil] if [error] is not [nil].
//   - [error]: [nil] on success, or an error wrapping the cause if ffmpeg
//     could not be started.
func (tools Tools) Start(ctx context.Context, arguments []string) (*Process, error) {
	execCommand := command(ctx, tools.FFmpeg, arguments...)
	process := &Process{command: execCommand, ctx: ctx, stderr: &lastLineWriter{}}
	execCommand.Stderr = process.stderr
	if err := execCommand.Start(); err != nil {
		return nil, fmt.Errorf("starting ffmpeg: %w", err)
	}
	return process, nil
}

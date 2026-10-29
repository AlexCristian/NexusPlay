// Package ffmpeg runs the ffmpeg and ffprobe programs and parses their
// output. No other package executes them, so process handling, stderr
// capture, progress parsing and the "cancelled versus failed" distinction
// live here and are tested once.
package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Tools locates the ffmpeg and ffprobe binaries (names on PATH or paths).
// The zero value is not usable; set both fields.
type Tools struct {
	FFmpeg  string
	FFprobe string
}

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

// RunOptions tune a Run call. The zero value runs ffmpeg plainly.
type RunOptions struct {
	// OnProgress, if set, receives how many seconds of output ffmpeg has
	// written so far. Run adds "-progress pipe:1 -nostats" to read it.
	OnProgress func(seconds float64)
	// LowPriority runs ffmpeg at a lower CPU priority so it yields to
	// playback (Linux only; ignored elsewhere).
	LowPriority bool
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

// Process is an ffmpeg started with Start that runs in the background.
type Process struct {
	cmd    *exec.Cmd
	ctx    context.Context
	stderr *lastLineWriter
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

// Wait waits for the process to exit. Like Run, it returns ctx.Err() when
// the process was cancelled and an *ExitError when ffmpeg failed.
func (process *Process) Wait() error {
	return exitError("ffmpeg", process.ctx, process.cmd.Wait(), process.stderr)
}

// command prepares a process that is killed when ctx is cancelled. WaitDelay
// bounds how long Wait then waits for output pipes to close, so a child
// process that inherited them cannot hold up shutdown.
func command(ctx context.Context, name string, arguments ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arguments...)
	cmd.WaitDelay = 2 * time.Second
	return cmd
}

// exitError turns the result of cmd.Wait into this package's error vocabulary.
func exitError(program string, ctx context.Context, err error, stderr *lastLineWriter) error {
	switch {
	case err == nil:
		return nil
	case ctx.Err() != nil:
		return ctx.Err() // killed because we asked it to stop: not a failure
	default:
		return &ExitError{Program: program, Err: err, LastLine: stderr.last()}
	}
}

// readProgress parses ffmpeg's "-progress pipe:1" key=value stream.
func readProgress(reader io.Reader, report func(seconds float64)) {
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		// out_time_us is microseconds; so is out_time_ms, despite its name.
		key, value, found := strings.Cut(scanner.Text(), "=")
		if !found || (key != "out_time_us" && key != "out_time_ms") {
			continue
		}
		if microseconds, err := strconv.ParseInt(value, 10, 64); err == nil && microseconds > 0 {
			report(float64(microseconds) / 1e6)
		}
	}
	// Keep draining so ffmpeg never blocks on a full pipe.
	_, _ = io.Copy(io.Discard, reader)
}

// lastLineWriter keeps the tail of a program's stderr. ffmpeg can write a
// lot (one line per bad frame), and only the end matters for the error.
type lastLineWriter struct {
	buffer bytes.Buffer
}

const keepStderr = 4096

func (writer *lastLineWriter) Write(data []byte) (int, error) {
	writer.buffer.Write(data)
	if writer.buffer.Len() > 2*keepStderr {
		tail := writer.buffer.Bytes()[writer.buffer.Len()-keepStderr:]
		writer.buffer = *bytes.NewBuffer(append([]byte(nil), tail...))
	}
	return len(data), nil
}

func (writer *lastLineWriter) last() string {
	text := strings.TrimSpace(writer.buffer.String())
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		return strings.TrimSpace(text[i+1:])
	}
	return text
}

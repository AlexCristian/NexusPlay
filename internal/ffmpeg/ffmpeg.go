package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

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

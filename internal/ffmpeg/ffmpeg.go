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

// [command] prepares a program run that is killed when [ctx] is cancelled.
// After the program exits or is killed, waiting for its output pipes to
// close is limited to 2
// seconds, so a child process that inherited them cannot hold up shutdown.
//
// Parameters:
//   - [ctx]: kills the program when cancelled.
//   - [name]: the program to run: a name looked up on the PATH, or a path.
//   - [arguments]: the program's command-line arguments.
//
// Returns:
//   - [*exec.Cmd]: the prepared command, not yet started.
func command(ctx context.Context, name string, arguments ...string) *exec.Cmd {
	command := exec.CommandContext(ctx, name, arguments...)
	command.WaitDelay = 2 * time.Second
	return command
}

// [exitError] turns the result of waiting for a program into this package's
// errors: a program killed because [ctx] was cancelled did not fail.
//
// Parameters:
//   - [program]: "ffmpeg" or "ffprobe", for the message.
//   - [ctx]: the context the program ran under.
//   - [err]: the error from [exec.Cmd.Wait] or [exec.Cmd.Output].
//   - [stderr]: the program's captured stderr.
//
// Returns:
//   - [error]: [nil] if [err] is [nil]; [ctx]'s error
//     ([context.Canceled] or [context.DeadlineExceeded]) if [ctx] has ended;
//     otherwise an [*ExitError] with the last line of stderr.
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

// [readProgress] reads ffmpeg's "-progress pipe:1" output, a stream of
// key=value lines, and reports how far the output has got each time ffmpeg
// says so. It returns when the stream ends, and reads it to the end even
// after an error, so ffmpeg never blocks on a full pipe.
//
// Parameters:
//   - [reader]: ffmpeg's standard output.
//   - [report]: called with the seconds of output written so far, from each
//     positive "out_time_us" or "out_time_ms" value (both are microseconds,
//     despite the second's name).
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

// [lastLineWriter] keeps only the end of a program's stderr, for the error
// message. ffmpeg can write a lot (one line per bad frame), and only the end
// matters. It keeps at least the last [keepStderr] bytes and at most twice
// that. It is not safe for concurrent use.
type lastLineWriter struct {
	// [buffer] holds the most recent stderr output.
	buffer bytes.Buffer
}

// [keepStderr] is how many bytes of stderr [lastLineWriter] keeps at least.
const keepStderr = 4096

// [Write] appends stderr output, dropping all but the last [keepStderr]
// bytes once more than twice that is held. It never fails.
//
// Parameters:
//   - [data]: the bytes the program wrote.
//
// Returns:
//   - [int]: the length of [data], since everything is accepted.
//   - [error]: always [nil].
func (writer *lastLineWriter) Write(data []byte) (int, error) {
	writer.buffer.Write(data)
	if writer.buffer.Len() > 2*keepStderr {
		tail := writer.buffer.Bytes()[writer.buffer.Len()-keepStderr:]
		writer.buffer = *bytes.NewBuffer(append([]byte(nil), tail...))
	}
	return len(data), nil
}

// [last] gets the last line the program wrote.
//
// Returns:
//   - [string]: the last line of the kept output that is not blank, with
//     spaces around it trimmed, or "" if nothing was written.
func (writer *lastLineWriter) last() string {
	text := strings.TrimSpace(writer.buffer.String())
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		return strings.TrimSpace(text[i+1:])
	}
	return text
}

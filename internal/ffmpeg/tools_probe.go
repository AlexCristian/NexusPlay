package ffmpeg

import (
	"context"
)

// [Probe] inspects a media file with ffprobe.
//
// Parameters:
//   - [ctx]: kills ffprobe when cancelled.
//   - [file]: the path of the file to inspect.
//
// Returns:
//   - [Probe]: the file's details, or a zero [Probe] if [error] is not
//     [nil].
//   - [error]: [nil] on success; [ctx]'s error if ffprobe was killed because
//     [ctx] ended; an [*ExitError] if ffprobe failed (for example on a file
//     that is not media) or could not be started (its [ExitError.Err] is
//     then the [exec] error, such as [exec.ErrNotFound]); or an error from
//     [parseProbe] if its output could not be read.
func (tools Tools) Probe(ctx context.Context, file string) (Probe, error) {
	execCommand := command(ctx, tools.FFprobe, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", file)
	stderr := &lastLineWriter{}
	execCommand.Stderr = stderr
	output, err := execCommand.Output()
	if err != nil {
		if exitErr := exitError("ffprobe", ctx, err, stderr); exitErr != nil {
			return Probe{}, exitErr
		}
	}
	return parseProbe(output)
}

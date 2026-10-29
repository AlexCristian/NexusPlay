package ffmpeg

import (
	"context"
)

// Probe inspects a media file with ffprobe. Failures of ffprobe itself are
// *ExitError.
func (tools Tools) Probe(ctx context.Context, file string) (Probe, error) {
	cmd := command(ctx, tools.FFprobe, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", file)
	stderr := &lastLineWriter{}
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if err != nil {
		if exitErr := exitError("ffprobe", ctx, err, stderr); exitErr != nil {
			return Probe{}, exitErr
		}
	}
	return parseProbe(out)
}

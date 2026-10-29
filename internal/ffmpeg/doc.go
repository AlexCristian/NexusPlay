// Package [ffmpeg] runs the ffmpeg and ffprobe programs and parses their
// output. No other package executes them, so process handling, stderr
// capture, progress parsing and telling "cancelled" from "failed" live here
// and are tested once.
package ffmpeg

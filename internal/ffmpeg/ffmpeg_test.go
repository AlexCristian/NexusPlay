package ffmpeg

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeProgram writes an executable shell script and returns its path.
func fakeProgram(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fake")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRunReportsLastLine(t *testing.T) {
	tools := Tools{FFmpeg: fakeProgram(t, `echo "Input #0" >&2; echo "in.avi: Invalid data found" >&2; exit 1`)}
	err := tools.Run(context.Background(), []string{"-i", "in.avi"}, RunOptions{})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v, want *ExitError", err)
	}
	if exitErr.LastLine != "in.avi: Invalid data found" || exitErr.Program != "ffmpeg" {
		t.Errorf("ExitError = %+v", exitErr)
	}
	if want := "ffmpeg: exit status 1: in.avi: Invalid data found"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	var procErr *exec.ExitError
	if !errors.As(err, &procErr) || procErr.ExitCode() != 1 {
		t.Errorf("ExitError should unwrap to the process exit status, got %v", err)
	}
}

func TestRunCancelledIsNotExitError(t *testing.T) {
	tools := Tools{FFmpeg: fakeProgram(t, `exec sleep 10`)}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := tools.Run(ctx, nil, RunOptions{})
	var exitErr *ExitError
	if errors.As(err, &exitErr) || !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want the context's error and no ExitError", err)
	}
}

func TestRunReportsProgress(t *testing.T) {
	// The fake checks it was given the progress flags, then reports twice.
	tools := Tools{FFmpeg: fakeProgram(t, `
case "$*" in "-progress pipe:1 -nostats -i in.mkv out.mp4") ;; *) echo "unexpected args: $*" >&2; exit 2;; esac
printf 'frame=10\nout_time_us=1500000\nprogress=continue\nout_time_ms=3000000\nprogress=end\n'`)}
	var got []float64
	err := tools.Run(context.Background(), []string{"-i", "in.mkv", "out.mp4"}, RunOptions{
		OnProgress:  func(s float64) { got = append(got, s) },
		LowPriority: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 1.5 || got[1] != 3 {
		t.Errorf("progress = %v, want [1.5 3]", got)
	}
}

func TestRunKeepsOnlyTheEndOfLongStderr(t *testing.T) {
	tools := Tools{FFmpeg: fakeProgram(t, `i=0; while [ $i -lt 3000 ]; do echo "warning line $i" >&2; i=$((i+1)); done; echo "the real error" >&2; exit 1`)}
	err := tools.Run(context.Background(), nil, RunOptions{})
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.LastLine != "the real error" {
		t.Errorf("err = %v", err)
	}
}

func TestStartAndWait(t *testing.T) {
	tools := Tools{FFmpeg: fakeProgram(t, `sleep 0.1; echo "boom" >&2; exit 3`)}
	p, err := tools.Start(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var exitErr *ExitError
	if err := p.Wait(); !errors.As(err, &exitErr) || exitErr.LastLine != "boom" {
		t.Errorf("Wait = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	p, err = Tools{FFmpeg: fakeProgram(t, `exec sleep 10`)}.Start(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if err := p.Wait(); !errors.Is(err, context.Canceled) {
		t.Errorf("Wait after cancel = %v, want context.Canceled", err)
	}
}

func TestStartMissingBinary(t *testing.T) {
	_, err := Tools{FFmpeg: filepath.Join(t.TempDir(), "missing")}.Start(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "starting ffmpeg") {
		t.Errorf("err = %v", err)
	}
}

const probeJSON = `{
  "streams": [
    {"index": 0, "codec_type": "video", "codec_name": "mjpeg", "disposition": {"attached_pic": 1}},
    {"index": 1, "codec_type": "video", "codec_name": "h264", "pix_fmt": "yuv420p10le", "disposition": {"default": 1}},
    {"index": 2, "codec_type": "audio", "codec_name": "ac3", "channels": 6, "tags": {"language": "FRE", "title": "VF"}},
    {"index": 3, "codec_type": "audio", "codec_name": "aac", "channels": 2, "tags": {"language": "eng"}, "disposition": {"default": 1}},
    {"index": 4, "codec_type": "subtitle", "codec_name": "subrip", "tags": {"language": "eng"}}
  ],
  "format": {"format_name": "matroska,webm", "duration": "1312.480000"}
}`

func TestProbe(t *testing.T) {
	jsonFile := filepath.Join(t.TempDir(), "probe.json")
	os.WriteFile(jsonFile, []byte(probeJSON), 0o644)
	tools := Tools{FFprobe: fakeProgram(t, `cat `+jsonFile)}
	p, err := tools.Probe(context.Background(), "movie.mkv")
	if err != nil {
		t.Fatal(err)
	}
	if p.Duration != 1312.48 || p.Container != "matroska,webm" || len(p.Streams) != 5 {
		t.Errorf("probe = %+v", p)
	}
	if v, ok := p.Video(); !ok || v.Index != 1 || v.PixFmt != "yuv420p10le" {
		t.Errorf("Video() = %+v, %v; want stream 1, skipping the cover art", v, ok)
	}
	if a, ok := p.Audio(); !ok || a.Language != "fre" || a.Channels != 6 || a.Title != "VF" {
		t.Errorf("Audio() = %+v", a)
	}
	if p.VideoCodec() != "h264" || p.AudioCodec() != "ac3" || p.Count("audio") != 2 || p.Count("subtitle") != 1 {
		t.Errorf("codecs %s/%s, counts audio=%d subtitle=%d", p.VideoCodec(), p.AudioCodec(), p.Count("audio"), p.Count("subtitle"))
	}
	if !p.Streams[3].Default || p.Streams[2].Default {
		t.Error("default dispositions not read")
	}
}

func TestProbeFailures(t *testing.T) {
	_, err := Tools{FFprobe: fakeProgram(t, `echo "movie.mkv: No such file or directory" >&2; exit 1`)}.Probe(context.Background(), "movie.mkv")
	var exitErr *ExitError
	if !errors.As(err, &exitErr) || exitErr.Program != "ffprobe" || exitErr.LastLine != "movie.mkv: No such file or directory" {
		t.Errorf("err = %v", err)
	}
	if _, err := (Tools{FFprobe: fakeProgram(t, `echo not json`)}).Probe(context.Background(), "x"); err == nil {
		t.Error("garbage output should be an error")
	}
	p, err := parseProbe([]byte(`{"format": {"duration": "N/A"}, "streams": [{"codec_type": "audio", "codec_name": "mp3"}]}`))
	if err != nil || p.Duration != 0 {
		t.Errorf("N/A duration: %v, %v", p.Duration, err)
	}
	if _, ok := p.Video(); ok {
		t.Error("an audio-only file has no video stream")
	}
}

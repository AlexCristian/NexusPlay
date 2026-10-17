package stream

import (
	"slices"
	"testing"
)

func TestFFmpegArgs(t *testing.T) {
	remux := ffmpegArgs("in.mkv", "/out", "h264", "aac")
	if !slices.Contains(remux, "copy") || slices.Contains(remux, "libx264") {
		t.Errorf("h264/aac should be copied, got %v", remux)
	}
	encode := ffmpegArgs("in.avi", "/out", "mpeg4", "mp3")
	if !slices.Contains(encode, "libx264") || !slices.Contains(encode, "aac") {
		t.Errorf("mpeg4/mp3 should be encoded, got %v", encode)
	}
	if encode[len(encode)-1] != "/out/index.m3u8" {
		t.Errorf("playlist should be the last argument, got %v", encode)
	}
}

package stream

import (
	"slices"
	"testing"
)

func TestDirectPlayable(t *testing.T) {
	tests := []struct {
		file, video, audio string
		want               bool
	}{
		{"a.mp4", "h264", "aac", true},
		{"a.M4V", "h264", "mp3", true},
		{"a.mp4", "h264", "", true},
		{"a.mp4", "hevc", "aac", false},
		{"a.mp4", "h264", "ac3", false},
		{"a.webm", "vp9", "opus", true},
		{"a.webm", "h264", "aac", false},
		{"a.mkv", "h264", "aac", false},
		{"a.avi", "mpeg4", "mp3", false},
	}
	for _, tt := range tests {
		if got := DirectPlayable(tt.file, tt.video, tt.audio); got != tt.want {
			t.Errorf("DirectPlayable(%q, %q, %q) = %v, want %v", tt.file, tt.video, tt.audio, got, tt.want)
		}
	}
}

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

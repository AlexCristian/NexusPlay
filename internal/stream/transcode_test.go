package stream

import (
	"slices"
	"testing"

	"github.com/AlexCristian/NexusPlay/internal/media"
)

func TestFFmpegArgs(t *testing.T) {
	remux := ffmpegArgs("in.mkv", "/out", media.Format{VideoCodec: "h264", AudioCodec: "aac", PixFmt: "yuv420p"})
	if !slices.Contains(remux, "copy") || slices.Contains(remux, "libx264") {
		t.Errorf("h264/aac should be copied, got %v", remux)
	}
	if tenBit := ffmpegArgs("in.mkv", "/out", media.Format{VideoCodec: "h264", AudioCodec: "aac", PixFmt: "yuv420p10le"}); !slices.Contains(tenBit, "libx264") {
		t.Errorf("10-bit h264 must be re-encoded for browsers, got %v", tenBit)
	}
	encode := ffmpegArgs("in.avi", "/out", media.Format{VideoCodec: "mpeg4", AudioCodec: "mp3"})
	if !slices.Contains(encode, "libx264") || !slices.Contains(encode, "aac") {
		t.Errorf("mpeg4/mp3 should be encoded, got %v", encode)
	}
	if encode[len(encode)-1] != "/out/index.m3u8" {
		t.Errorf("playlist should be the last argument, got %v", encode)
	}
}

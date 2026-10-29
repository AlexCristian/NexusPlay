package media

import "testing"

func TestPolicy(t *testing.T) {
	tests := []struct {
		format  Format
		deliver Delivery
		convert bool
	}{
		// The one format the library keeps: plays directly, never converted.
		{Format{Path: "a.mp4", VideoCodec: "h264", AudioCodec: "aac", PixFmt: "yuv420p"}, DirectPlay, false},
		{Format{Path: "a.M4V", VideoCodec: "h264", AudioCodec: "mp3"}, DirectPlay, false},
		{Format{Path: "a.mp4", VideoCodec: "h264"}, DirectPlay, false}, // no audio
		// Browsers play these, but the library still converts them.
		{Format{Path: "a.mp4", VideoCodec: "vp9", AudioCodec: "opus"}, DirectPlay, true},
		{Format{Path: "a.mp4", VideoCodec: "h264", AudioCodec: "flac"}, DirectPlay, true},
		{Format{Path: "a.webm", VideoCodec: "vp9", AudioCodec: "opus"}, DirectPlay, true},
		// Browsers can't play these: HLS until converted.
		{Format{Path: "a.mp4", VideoCodec: "hevc", AudioCodec: "aac"}, HLS, true},
		{Format{Path: "a.mp4", VideoCodec: "h264", AudioCodec: "ac3"}, HLS, true},
		{Format{Path: "a.webm", VideoCodec: "h264", AudioCodec: "aac"}, HLS, true},
		{Format{Path: "a.mkv", VideoCodec: "h264", AudioCodec: "aac"}, HLS, true},
		{Format{Path: "a.avi", VideoCodec: "mpeg4", AudioCodec: "mp3"}, HLS, true},
		// 10-bit H.264 plays in no browser, whatever the container.
		{Format{Path: "a.mp4", VideoCodec: "h264", AudioCodec: "aac", PixFmt: "yuv420p10le"}, HLS, true},
		{Format{Path: "a.mp4", VideoCodec: "h264", AudioCodec: "aac", PixFmt: "yuv444p"}, HLS, true},
	}
	for _, testCase := range tests {
		if got := Deliver(testCase.format); got != testCase.deliver {
			t.Errorf("Deliver(%+v) = %v, want %v", testCase.format, got, testCase.deliver)
		}
		if got := NeedsConversion(testCase.format); got != testCase.convert {
			t.Errorf("NeedsConversion(%+v) = %v, want %v", testCase.format, got, testCase.convert)
		}
	}
}

func TestFileTypes(t *testing.T) {
	for name, want := range map[string]string{"a.MP4": "video/mp4", "b.mkv": "video/x-matroska", "c.webm": "video/webm"} {
		if got, known := MIMEType(name); !known || got != want || !IsVideo(name) {
			t.Errorf("%s: MIMEType = %q, %v", name, got, known)
		}
	}
	for _, name := range []string{"a.srt", "notes.txt", "noext"} {
		if IsVideo(name) {
			t.Errorf("IsVideo(%q) = true", name)
		}
	}
}

func TestVideoTypesLoaded(t *testing.T) {
	if mimeType, known := MIMEType("Movie.MKV"); !known || mimeType != "video/x-matroska" {
		t.Errorf("MIMEType(Movie.MKV) = %q, %v; want video/x-matroska", mimeType, known)
	}
	if IsVideo("poster.jpg") {
		t.Error("IsVideo(poster.jpg) = true; want false")
	}
	if len(videoTypes) != 8 {
		t.Errorf("video_types.txt loaded %d extensions; want 8", len(videoTypes))
	}
}

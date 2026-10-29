package media

import (
	"path"
	"slices"
	"strings"
)

// videoTypes maps the video file extensions NexusPlay picks up to the MIME
// type it serves them with.
var videoTypes = map[string]string{
	".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm",
	".mkv": "video/x-matroska", ".mov": "video/quicktime", ".avi": "video/x-msvideo",
	".ts": "video/mp2t", ".wmv": "video/x-ms-wmv",
}

// IsVideo reports whether a file name has a video extension NexusPlay uses.
func IsVideo(name string) bool {
	_, known := videoTypes[strings.ToLower(path.Ext(name))]
	return known
}

// MIMEType returns the Content-Type to serve a video file with.
func MIMEType(name string) (string, bool) {
	mimeType, known := videoTypes[strings.ToLower(path.Ext(name))]
	return mimeType, known
}

const (
	// DirectPlay serves the original file with range requests.
	DirectPlay Delivery = iota
	// HLS converts the file on the fly with ffmpeg.
	HLS
)

// Deliver decides how to play a file now. It accepts anything current
// browsers decode natively, which is broader than what NeedsConversion keeps.
func Deliver(format Format) Delivery {
	playable := false
	switch ext(format.Path) {
	case ".mp4", ".m4v":
		playable = slices.Contains([]string{"h264", "av1", "vp9"}, format.VideoCodec) &&
			slices.Contains([]string{"", "aac", "mp3", "opus", "flac"}, format.AudioCodec)
	case ".webm":
		playable = slices.Contains([]string{"vp8", "vp9", "av1"}, format.VideoCodec) &&
			slices.Contains([]string{"", "opus", "vorbis"}, format.AudioCodec)
	}
	if playable && format.VideoCodec == "h264" && !BrowserSafeH264(format.PixFmt) {
		playable = false
	}
	if playable {
		return DirectPlay
	}
	return HLS
}

// NeedsConversion reports whether the converter should rewrite a file. The
// library keeps only MP4 files with H.264 video and AAC or MP3 audio: the
// one combination that plays everywhere, including Safari and TV browsers.
// So WebM and VP9 files, which Deliver plays directly in most browsers, are
// still converted.
func NeedsConversion(format Format) bool {
	switch {
	case ext(format.Path) != ".mp4" && ext(format.Path) != ".m4v":
		return true
	case format.VideoCodec != "h264" || !BrowserSafeH264(format.PixFmt):
		return true
	default:
		return !slices.Contains([]string{"", "aac", "mp3"}, format.AudioCodec)
	}
}

// BrowserSafeH264 reports whether H.264 video with this pixel format plays in
// browsers, which decode only 8-bit 4:2:0. An unknown format ("") counts as
// safe, which is what nearly all H.264 files are.
func BrowserSafeH264(pixFmt string) bool {
	return pixFmt == "" || pixFmt == "yuv420p" || pixFmt == "yuvj420p"
}

func ext(name string) string { return strings.ToLower(path.Ext(name)) }

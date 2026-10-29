package media

import (
	_ "embed"
	"path"
	"slices"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/datafile"
)

// [videoTypesFile] is the contents of video_types.txt, built into the program
// by the compiler. The build fails if the file is missing.
//
//go:embed video_types.txt
var videoTypesFile string

// [videoTypes] maps the video file extensions NexusPlay picks up to the MIME
// type it serves them with, from video_types.txt.
var videoTypes = datafile.Table("video_types.txt", videoTypesFile)

// [IsVideo] reports whether a file is one NexusPlay picks up as a video,
// judged by its extension alone, from video_types.txt.
//
// Parameters:
//   - [name]: a file name or path, such as "Movie.MKV". Case does not matter.
//
// Returns:
//   - [bool]: [true] if the extension is in [videoTypes], [false] otherwise.
func IsVideo(name string) bool {
	_, known := videoTypes[strings.ToLower(path.Ext(name))]
	return known
}

// [MIMEType] gets the Content-Type header value to serve a video file with.
//
// Parameters:
//   - [name]: a file name or path, such as "Movie.mkv". Case does not matter.
//
// Returns:
//   - [string]: the MIME type for the file's extension, such as
//     "video/x-matroska", or "" if the extension is not a video type.
//   - [bool]: [true] if the extension is in [videoTypes], [false] otherwise.
func MIMEType(name string) (string, bool) {
	mimeType, known := videoTypes[strings.ToLower(path.Ext(name))]
	return mimeType, known
}

const (
	// [DirectPlay] serves the original file as it is, with HTTP range requests
	// so the player can seek.
	DirectPlay Delivery = iota
	// [HLS] converts the file on the fly with ffmpeg into an HLS stream, for
	// files the browser cannot play as they are.
	HLS
)

// [Deliver] decides how to play a file right now. It accepts anything
// current browsers decode natively: MP4 or M4V files with H.264, AV1 or VP9
// video and AAC, MP3, Opus or FLAC audio (or none), and WebM files with VP8,
// VP9 or AV1 video and Opus or Vorbis audio (or none). H.264 must also have a
// pixel format [BrowserSafeH264] accepts. This is broader than what
// [NeedsConversion] lets the library keep.
//
// Parameters:
//   - [format]: the file's container and codecs.
//
// Returns:
//   - [Delivery]: [DirectPlay] if a browser can play the file as it is,
//     [HLS] otherwise.
func Deliver(format Format) Delivery {
	playable := false
	switch extension(format.Path) {
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

// [NeedsConversion] reports whether the converter should rewrite a file. The
// library keeps only MP4 or M4V files with H.264 video in a pixel format
// [BrowserSafeH264] accepts and AAC or MP3 audio (or none): the one
// combination that plays everywhere, including Safari and TV browsers. So
// WebM and VP9 files, which [Deliver] plays directly in most browsers, are
// still converted.
//
// Parameters:
//   - [format]: the file's container and codecs.
//
// Returns:
//   - [bool]: [true] if the file should be converted, [false] if it can stay
//     as it is.
func NeedsConversion(format Format) bool {
	switch {
	case extension(format.Path) != ".mp4" && extension(format.Path) != ".m4v":
		return true
	case format.VideoCodec != "h264" || !BrowserSafeH264(format.PixFmt):
		return true
	default:
		return !slices.Contains([]string{"", "aac", "mp3"}, format.AudioCodec)
	}
}

// [BrowserSafeH264] reports whether H.264 video in a given pixel format plays
// in browsers, which decode only 8-bit 4:2:0 H.264.
//
// Parameters:
//   - [pixFmt]: the pixel format as ffprobe names it, such as "yuv420p", or
//     "" when it is not known.
//
// Returns:
//   - [bool]: [true] for "yuv420p", "yuvj420p" and "" (an unknown format
//     counts as safe, which nearly all H.264 files are); [false] for anything
//     else, such as 10-bit "yuv420p10le".
func BrowserSafeH264(pixFmt string) bool {
	return pixFmt == "" || pixFmt == "yuv420p" || pixFmt == "yuvj420p"
}

// [extension] gets a file's extension for comparing with the policy's lists.
//
// Parameters:
//   - [name]: a file name or path.
//
// Returns:
//   - [string]: the extension in lower case with its dot, such as ".mkv", or
//     "" if [name] has none.
func extension(name string) string { return strings.ToLower(path.Ext(name)) }

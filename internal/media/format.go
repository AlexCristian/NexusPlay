package media

// [Format] is what the playback policy needs to know about a file, as read by
// ffprobe.
type Format struct {
	// [Path] is the file's path or name. Only its extension is used, compared
	// case-insensitively.
	Path string
	// [VideoCodec] is the video stream's codec as ffprobe names it, such as
	// "h264", "hevc" or "vp9".
	VideoCodec string
	// [AudioCodec] is the first audio stream's codec as ffprobe names it, such
	// as "aac", or "" when the file has no audio.
	AudioCodec string
	// [PixFmt] is the video stream's pixel format as ffprobe names it, such as
	// "yuv420p" or "yuv420p10le", or "" when it is not known.
	PixFmt string
}

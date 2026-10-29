package store

// [ConversionCandidate] is what the converter needs to decide whether a file
// needs converting, from [Store.ConversionCandidates].
type ConversionCandidate struct {
	// [ID] is the media item's ID.
	ID int64
	// [Path] is the file's path relative to the media folder; its extension is
	// the container.
	Path string
	// [VideoCodec] is the video codec as ffprobe names it, such as "h264".
	VideoCodec string
	// [AudioCodec] is the first audio track's codec, or "" for no audio.
	AudioCodec string
	// [PixelFormat] is the video's pixel format, or "" if unknown.
	PixelFormat string
}

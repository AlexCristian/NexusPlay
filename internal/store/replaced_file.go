package store

// [ReplacedFile] describes the converted file that took an original's place,
// for [Store.ReplaceMediaFile].
type ReplacedFile struct {
	// [Path] is the converted file's path relative to the media folder, with
	// forward slashes.
	Path string
	// [Duration] is the converted file's length in seconds.
	Duration float64
	// [Container] is ffprobe's container format names for the converted file.
	Container string
	// [VideoCodec] is the converted file's video codec, "h264".
	VideoCodec string
	// [AudioCodec] is the converted file's first audio codec, or "".
	AudioCodec string
	// [PixelFormat] is the converted file's pixel format.
	PixelFormat string
	// [Size] is the converted file's size in bytes.
	Size int64
}

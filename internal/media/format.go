package media

// Format is what the policy needs to know about a file.
type Format struct {
	Path       string // only the extension matters
	VideoCodec string // ffprobe codec name, e.g. "h264"
	AudioCodec string // "" when the file has no audio
	PixFmt     string // ffprobe pixel format; "" when unknown
}

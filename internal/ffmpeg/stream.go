package ffmpeg

// Stream is one stream of a file.
type Stream struct {
	Index     int    // position in the file, as used by ffmpeg's -map 0:N
	Type      string // "video", "audio", "subtitle", "data" or "attachment"
	Codec     string // e.g. "h264", "aac", "subrip"
	PixelFormat    string // video only, e.g. "yuv420p"
	Channels  int    // audio only
	Language  string // lower-case language tag from the file; untrusted
	Title     string
	Default   bool // the file marks this stream as the one to play by default
	AttachedPicture bool // cover art stored as a video stream
}

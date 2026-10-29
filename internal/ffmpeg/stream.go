package ffmpeg

// [Stream] is one stream of a file, as ffprobe describes it.
type Stream struct {
	// [Index] is the stream's position in the file, as ffmpeg's "-map 0:N"
	// expects it.
	Index int
	// [Type] is the kind of stream: "video", "audio", "subtitle", "data" or
	// "attachment".
	Type string
	// [Codec] is the codec as ffprobe names it, such as "h264", "aac" or
	// "subrip".
	Codec string
	// [PixelFormat] is a video stream's pixel format, such as "yuv420p", or ""
	// for other streams.
	PixelFormat string
	// [Channels] is an audio stream's channel count, such as 2 or 6, or 0 for
	// other streams.
	Channels int
	// [Language] is the stream's language tag from the file, lowercased, such as
	// "fre", or "" if it has none. It comes from the file, so it is untrusted.
	Language string
	// [Title] is the stream's title tag from the file, such as "Director's
	// commentary", or "" if it has none.
	Title string
	// [Default] is [true] when the file marks this stream as the one to play by
	// default.
	Default bool
	// [AttachedPicture] is [true] for cover art stored as a video stream.
	AttachedPicture bool
}

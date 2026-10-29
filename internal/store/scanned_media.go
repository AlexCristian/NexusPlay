package store

// [ScannedMedia] is what the library scanner knows about a new file, for
// [Store.InsertScannedMedia].
type ScannedMedia struct {
	// [Path] is the file's path relative to the media folder, with forward
	// slashes.
	Path string
	// [Title] is the title guessed from the file name.
	Title string
	// [Year] is the year guessed from the file name, or "".
	Year string
	// [Season] is the season number guessed from the file name, or 0.
	Season int
	// [Episode] is the episode number guessed from the file name, or 0.
	Episode int
	// [EpisodeEnd] is the last episode of a multi-episode file, or 0 (see
	// [Media.EpisodeEnd]).
	EpisodeEnd int
	// [Duration] is the length in seconds from ffprobe, or 0.
	Duration float64
	// [Container] is ffprobe's container format names.
	Container string
	// [VideoCodec] is the video codec from ffprobe.
	VideoCodec string
	// [AudioCodec] is the first audio track's codec from ffprobe, or "".
	AudioCodec string
	// [PixelFormat] is the video's pixel format from ffprobe, or "".
	PixelFormat string
	// [Size] is the file's size in bytes.
	Size int64
}

package web

// [Playback] tells the player how to load a video.
type Playback struct {
	// [Direct] is [true] when the browser plays the file itself, and [false]
	// when it plays an HLS transcode.
	Direct bool
	// [URL] is the file's or the HLS playlist's URL.
	URL string
}

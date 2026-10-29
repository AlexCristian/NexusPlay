package web

// Playback describes how the browser should load a video.
type Playback struct {
	Direct bool   // the file itself; otherwise an HLS conversion
	URL    string // the file or the HLS playlist
}

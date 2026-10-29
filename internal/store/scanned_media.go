package store

import (
	_ "modernc.org/sqlite"
)

// ScannedMedia is what the library scanner knows about a new file.
type ScannedMedia struct {
	Path       string
	Title      string
	Year       string
	Season     int
	Episode    int
	EpisodeEnd int // see Media.EpisodeEnd
	Duration   float64
	Container  string
	VideoCodec string
	AudioCodec string
	PixelFormat     string
	Size       int64
}

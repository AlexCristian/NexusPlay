package store

import (
	_ "modernc.org/sqlite"
)

// ReplacedFile describes the converted file that took an original's place.
type ReplacedFile struct {
	Path        string
	Duration    float64
	Container   string
	VideoCodec  string
	AudioCodec  string
	PixelFormat string
	Size        int64
}

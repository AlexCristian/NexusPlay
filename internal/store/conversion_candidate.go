package store

import (
	_ "modernc.org/sqlite"
)

// ConversionCandidate is what the converter needs to decide whether a file
// needs converting.
type ConversionCandidate struct {
	ID                                   int64
	Path, VideoCodec, AudioCodec, PixFmt string
}

package store

import (
	_ "modernc.org/sqlite"
)

// ConversionFailure is a file the converter gave up on.
type ConversionFailure struct {
	Media Media
	Error string
}

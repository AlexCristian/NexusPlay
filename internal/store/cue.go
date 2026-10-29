package store

import (
	_ "modernc.org/sqlite"
)

// Cue is a stored subtitle line.
type Cue struct {
	ID      int64
	StartMS int64
	EndMS   int64
	Text    string
}

package store

import (
	_ "modernc.org/sqlite"
)

// ScannedMetadata is what an online lookup found for a new file. Empty
// fields mean "nothing found" and leave the current value alone.
type ScannedMetadata struct {
	Title, Year, Overview string
	Poster                string // file name in the poster directory
}

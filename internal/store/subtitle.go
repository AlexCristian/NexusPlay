package store

import (
	_ "modernc.org/sqlite"
)

// Subtitle is one subtitle track for a media item.
type Subtitle struct {
	ID       int64
	MediaID  int64
	Language string // BCP 47 code, e.g. "en"
	Label    string // shown in the player, e.g. "English"
	Source   string // sidecar file it was imported from, if any
}

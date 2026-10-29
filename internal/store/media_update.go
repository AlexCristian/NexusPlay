package store

import (
	_ "modernc.org/sqlite"
)

// MediaUpdate holds the editable metadata fields.
type MediaUpdate struct {
	Title        string
	Year         string
	Overview     string
	Category     string
	CollectionID int64
	Season       int
	Episode      int
	EpisodeEnd   int    // the last episode of a multi-episode file; 0 for one
	Poster       string // a new poster file; empty keeps the current one
	RemovePoster bool   // clear the poster (Poster must be empty)
}

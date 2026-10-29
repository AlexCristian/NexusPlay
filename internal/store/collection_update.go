package store

import (
	_ "modernc.org/sqlite"
)

// CollectionUpdate holds a collection's editable fields.
type CollectionUpdate struct {
	Name         string
	Category     string
	Year         string
	Overview     string
	Poster       string // a new poster file; empty keeps the current one
	RemovePoster bool   // clear the poster (Poster must be empty)
}

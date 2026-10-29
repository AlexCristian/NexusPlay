package store

import (
	_ "modernc.org/sqlite"
)

// Tile is one poster on the home page: a standalone item or a whole collection.
type Tile struct {
	IsCollection bool
	ID           int64
	Title        string
	Year         string
	Poster       string
	Watched      bool
}

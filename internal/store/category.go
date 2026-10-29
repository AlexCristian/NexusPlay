package store

import (
	_ "modernc.org/sqlite"
)

// Category is a named row of tiles.
type Category struct {
	Name  string
	Tiles []Tile
}

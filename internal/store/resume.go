package store

import (
	_ "modernc.org/sqlite"
)

// Resume is a partly watched item for the "Previously watching" list.
type Resume struct {
	Media    Media
	Position float64
}

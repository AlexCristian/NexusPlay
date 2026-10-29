package store

import (
	"time"

	_ "modernc.org/sqlite"
)

// Collection groups media, typically a TV show organized by season.
type Collection struct {
	ID        int64
	Name      string
	Category  string
	Year      string
	Overview  string
	Poster    string
	CreatedAt time.Time
}

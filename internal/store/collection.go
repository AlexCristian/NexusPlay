package store

import (
	"time"
)

// [Collection] groups media, typically a TV show organized by season, or a
// film series.
type Collection struct {
	// [ID] identifies the collection.
	ID int64
	// [Name] is the collection's name, unique regardless of letter case.
	Name string
	// [Category] is the home-page row the collection appears in, such as "TV
	// Shows", or "" (shown as "Other").
	Category string
	// [Year] is the year the show began or the series' first film came out, or
	// "" if unknown.
	Year string
	// [Overview] is the collection's summary, or "" if there is none.
	Overview string
	// [Poster] is the poster's file name in the poster folder, or "".
	Poster string
	// [CreatedAt] is when the collection was created.
	CreatedAt time.Time
}

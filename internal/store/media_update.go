package store

// [MediaUpdate] holds a media item's editable fields, for
// [Store.UpdateMedia].
type MediaUpdate struct {
	// [Title] is the new title.
	Title string
	// [Year] is the new year, or "".
	Year string
	// [Overview] is the new summary, or "".
	Overview string
	// [Category] is the new category; [canonicalCategory] tidies it.
	Category string
	// [CollectionID] is the collection to put the item in, or 0 for none.
	CollectionID int64
	// [Season] is the new season number, or 0.
	Season int
	// [Episode] is the new episode number, or 0.
	Episode int
	// [EpisodeEnd] is the last episode of a multi-episode file, or 0 for one
	// episode.
	EpisodeEnd int
	// [Poster] is a new poster's file name, or "" to keep the current one.
	Poster string
	// [RemovePoster] clears the item's own poster, so its collection's shows
	// instead. It wins over [MediaUpdate.Poster], which should then be "".
	RemovePoster bool
}

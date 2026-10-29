package store

// [CollectionUpdate] holds a collection's editable fields, for
// [Store.UpdateCollection].
type CollectionUpdate struct {
	// [Name] is the collection's new name.
	Name string
	// [Category] is the new category; [canonicalCategory] tidies it.
	Category string
	// [Year] is the new year, or "".
	Year string
	// [Overview] is the new summary, or "".
	Overview string
	// [Poster] is a new poster's file name, or "" to keep the current one.
	Poster string
	// [RemovePoster] clears the poster. It wins over [CollectionUpdate.Poster],
	// which should then be "".
	RemovePoster bool
}

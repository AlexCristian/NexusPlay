package store

// [Resume] is a partly watched item for the "Previously watching" list.
type Resume struct {
	// [Media] is the item.
	Media Media
	// [Position] is where playback stopped, in seconds from the start.
	Position float64
}

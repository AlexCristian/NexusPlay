package store

// [Category] is one named row of tiles on the home page or in search
// results.
type Category struct {
	// [Name] is the category's name, such as "Movies", or "Other" for items
	// without one.
	Name string
	// [Tiles] lists the row's items, by title.
	Tiles []Tile
}

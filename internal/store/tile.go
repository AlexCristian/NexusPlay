package store

// [Tile] is one poster on the home page: a standalone item or a whole
// collection.
type Tile struct {
	// [IsCollection] is [true] when the tile stands for a collection, so
	// [Tile.ID] is a collection ID; otherwise it is a media ID.
	IsCollection bool
	// [ID] is the media item's or the collection's ID (see
	// [Tile.IsCollection]).
	ID int64
	// [Title] is the item's title or the collection's name.
	Title string
	// [Year] is the year shown under the title, or "".
	Year string
	// [Poster] is the poster's file name in the poster folder, or "".
	Poster string
	// [Watched] is [true] when the viewer has finished this item. Always
	// [false] for collections.
	Watched bool
}

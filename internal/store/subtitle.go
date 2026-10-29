package store

// [Subtitle] is one subtitle track of a media item.
type Subtitle struct {
	// [ID] identifies the track.
	ID int64
	// [MediaID] is the media item the track belongs to.
	MediaID int64
	// [Language] is the track's language code, such as "en".
	Language string
	// [Label] is the name shown in the player, such as "English".
	Label string
	// [Source] is the sidecar file the track was imported from, relative to the
	// media folder, or "" for an upload.
	Source string
}

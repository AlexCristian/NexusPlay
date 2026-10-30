package web

import (
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [MediaForm] is the data behind the control panel's edit and publish form
// for one media item.
type MediaForm struct {
	// [Media] is the item as stored, or after a failed save the item with
	// what was typed into the form (see [withTypedValues]).
	Media store.Media
	// [CollectionName] is the collection field's value, which may name a
	// collection that does not exist yet.
	CollectionName string
	// [Collections] lists every collection, for the collection field's
	// suggestions.
	Collections []store.Collection
	// [Categories] lists the categories in use, for the category field's
	// suggestions.
	Categories []string
	// [Subtitles] lists the item's subtitle tracks, for editing their labels.
	Subtitles []store.Subtitle
	// [MetadataEnabled] is [true] when TMDB lookups are configured, which shows
	// the "Refresh metadata" button.
	MetadataEnabled bool
	// [Error] is the message for a form that failed to save, or "".
	Error string
	// [Saved] is [true] right after the form was saved.
	Saved bool
	// [Refreshed] is [true] right after the details were fetched again from
	// TMDB.
	Refreshed bool
}

package web

import (
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// MediaForm is the data behind the edit and publish form.
type MediaForm struct {
	Media           store.Media
	CollectionName  string
	Collections     []store.Collection
	Categories      []string
	Subtitles       []store.Subtitle
	MetadataEnabled bool
	Error           string
	Saved           bool
	Refreshed       bool // details were just fetched again from TMDB
}

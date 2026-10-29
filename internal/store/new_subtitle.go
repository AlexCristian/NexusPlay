package store

import (
	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

// [NewSubtitle] is a subtitle track to store with [Store.AddSubtitle].
type NewSubtitle struct {
	// [MediaID] is the media item the track belongs to.
	MediaID int64
	// [Language] is the track's language code, such as "en".
	Language string
	// [Label] is the name shown in the player, such as "English".
	Label string
	// [Source] is the sidecar file it came from, relative to the media folder,
	// or "" for an upload. A track with a source is stored only once per media
	// item.
	Source string
	// [Cues] are the track's lines, in order.
	Cues []subtitle.Cue
}

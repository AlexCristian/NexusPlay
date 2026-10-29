package store

import (
	"github.com/AlexCristian/NexusPlay/internal/subtitle"
	_ "modernc.org/sqlite"
)

// NewSubtitle is a subtitle track to store.
type NewSubtitle struct {
	MediaID  int64
	Language string // BCP 47 code, e.g. "en"
	Label    string // shown in the player, e.g. "English"
	Source   string // sidecar file it came from, relative to the media folder; "" for uploads
	Cues     []subtitle.Cue
}

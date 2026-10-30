package web

import (
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [Season] is one season's episodes of a collection, for its dialog.
type Season struct {
	// [Number] is the season number, where 0 holds specials.
	Number int
	// [Episodes] lists the season's published episodes in order.
	Episodes []store.Media
}

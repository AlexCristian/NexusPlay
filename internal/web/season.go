package web

import (
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// Season is one season's episodes in a collection.
type Season struct {
	Number   int
	Episodes []store.Media
}

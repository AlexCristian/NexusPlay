package posters

import (
	"context"

	"github.com/AlexCristian/NexusPlay/internal/collections"
)

// Store lists the poster files in use; *store.Store implements it.
type Store interface {
	PosterNames(ctx context.Context) (collections.Set[string], error)
}

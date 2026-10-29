package posters

import (
	"context"
)

// Store lists the poster files in use; *store.Store implements it.
type Store interface {
	PosterNames(ctx context.Context) (map[string]bool, error)
}

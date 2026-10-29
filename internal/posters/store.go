package posters

import (
	"context"

	"github.com/AlexCristian/NexusPlay/internal/collections"
)

// [Store] is the database as the [Sweeper] uses it; [store.Store]
// implements it.
type Store interface {
	// [PosterNames] lists the poster files that any media or collection uses.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//
	// Returns:
	//   - [collections.Set[string]]: the file names in use, relative to the
	//     poster folder, such as "tmdb-movie-603.jpg".
	//   - [error]: [nil] on success, or the database error.
	PosterNames(ctx context.Context) (collections.Set[string], error)
}

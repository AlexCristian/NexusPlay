package convert

import (
	"context"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// Store is the database as the converter uses it; *store.Store implements it.
type Store interface {
	ConversionCandidates(ctx context.Context) ([]store.ConversionCandidate, error)
	MediaByID(ctx context.Context, id int64) (store.Media, error)
	ReplaceMediaFile(ctx context.Context, id int64, replaced store.ReplacedFile) error
	SetConvertFailed(ctx context.Context, id int64, reason string) error
}

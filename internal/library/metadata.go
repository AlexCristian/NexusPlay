package library

import (
	"context"
	_ "embed"

	"github.com/AlexCristian/NexusPlay/internal/metadata"
)

// Metadata looks titles up online; *metadata.Client is the real one. Its
// ErrDisabled and ErrNoMatch are expected and not logged.
type Metadata interface {
	Movie(ctx context.Context, title, year string) (metadata.Info, error)
	Show(ctx context.Context, name, year string) (metadata.Info, error)
	Episode(ctx context.Context, showID, season, episode int) (metadata.Info, error)
	Season(ctx context.Context, showID, season int) ([]metadata.Episode, error)
}

package library

import (
	"context"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/collections"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// Store is the database as the scanner uses it; *store.Store implements it.
type Store interface {
	MediaPaths(ctx context.Context) (map[string]int64, error)
	InsertScannedMedia(ctx context.Context, media store.ScannedMedia) (id int64, inserted bool, err error)
	SetScannedMetadata(ctx context.Context, id int64, scannedMetadata store.ScannedMetadata) error
	ReplaceMetadata(ctx context.Context, id int64, scannedMetadata store.ScannedMetadata) error
	MarkMetadataChecked(ctx context.Context, id int64) error
	MediaNeedingMetadata(ctx context.Context, checkedBefore time.Time, limit int) ([]store.Media, error)
	MediaByID(ctx context.Context, id int64) (store.Media, error)
	DeleteMedia(ctx context.Context, id int64) error
	SubtitleSources(ctx context.Context) (collections.Set[string], error)
	AddSubtitle(ctx context.Context, subtitle store.NewSubtitle) (id int64, inserted bool, err error)
	H264MissingPixelFormat(ctx context.Context) (map[string]int64, error)
	SetEpisodeEnd(ctx context.Context, id int64, episode, end int) error
	SetPixelFormat(ctx context.Context, id int64, pixelFormat string) error
}

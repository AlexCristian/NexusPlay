package library

import (
	"context"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/collections"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [Store] is the database as the scanner uses it; [store.Store] implements
// it. Each method is described at the [store.Store] method of the same name.
type Store interface {
	// [MediaPaths] lists every known file.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//
	// Returns:
	//   - [map[string]int64]: each path mapped to its media ID.
	//   - [error]: [nil] on success, or the database error.
	MediaPaths(ctx context.Context) (map[string]int64, error)
	// [InsertScannedMedia] adds a new file as unpublished media, unless its path
	// is already known.
	//
	// Parameters:
	//   - [ctx]: cancels the insert.
	//   - [media]: what the scanner knows about the file.
	//
	// Returns:
	//   - [int64] ([id]): the new media item's ID, or 0.
	//   - [bool] ([inserted]): [true] if a row was added.
	//   - [error] ([err]): [nil] on success, or the database error.
	InsertScannedMedia(ctx context.Context, media store.ScannedMedia) (id int64, inserted bool, err error)
	// [SetScannedMetadata] saves what a lookup found for an unpublished item.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [id]: the media item.
	//   - [scannedMetadata]: what the lookup found.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	SetScannedMetadata(ctx context.Context, id int64, scannedMetadata store.ScannedMetadata) error
	// [ReplaceMetadata] saves what a lookup found, published or not.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [id]: the media item.
	//   - [scannedMetadata]: what the lookup found.
	//
	// Returns:
	//   - [error]: [nil] on success, [store.ErrNotFound] if the item does not
	//     exist, or another database error.
	ReplaceMetadata(ctx context.Context, id int64, scannedMetadata store.ScannedMetadata) error
	// [MarkMetadataChecked] records a lookup that found nothing to save.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [id]: the media item.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	MarkMetadataChecked(ctx context.Context, id int64) error
	// [MediaNeedingMetadata] lists unpublished media missing a poster or
	// description, not looked up since a given time.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [checkedBefore]: only items last looked up before this are listed.
	//   - [limit]: the most items to list.
	//
	// Returns:
	//   - [[]store.Media]: the items, least recently checked first.
	//   - [error]: [nil] on success, or the database error.
	MediaNeedingMetadata(ctx context.Context, checkedBefore time.Time, limit int) ([]store.Media, error)
	// [MediaByID] loads one media item.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [id]: the media item.
	//
	// Returns:
	//   - [store.Media]: the media item.
	//   - [error]: [nil] on success, [store.ErrNotFound] if it does not exist,
	//     or another database error.
	MediaByID(ctx context.Context, id int64) (store.Media, error)
	// [DeleteMedia] removes a media item whose file is gone.
	//
	// Parameters:
	//   - [ctx]: cancels the delete.
	//   - [id]: the media item.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	DeleteMedia(ctx context.Context, id int64) error
	// [SubtitleSources] lists the sidecar files already imported.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//
	// Returns:
	//   - [collections.Set[string]]: the sidecar paths.
	//   - [error]: [nil] on success, or the database error.
	SubtitleSources(ctx context.Context) (collections.Set[string], error)
	// [AddSubtitle] stores a subtitle track, unless one from the same source
	// exists.
	//
	// Parameters:
	//   - [ctx]: cancels the inserts.
	//   - [subtitle]: the track and its lines.
	//
	// Returns:
	//   - [int64] ([id]): the new track's ID, or 0.
	//   - [bool] ([inserted]): [true] if the track was added.
	//   - [error] ([err]): [nil] on success, or the database error.
	AddSubtitle(ctx context.Context, subtitle store.NewSubtitle) (id int64, inserted bool, err error)
	// [H264MissingPixelFormat] finds H.264 media with no recorded pixel format.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//
	// Returns:
	//   - [map[string]int64]: each such path mapped to its media ID.
	//   - [error]: [nil] on success, or the database error.
	H264MissingPixelFormat(ctx context.Context) (map[string]int64, error)
	// [SetEpisodeEnd] records a multi-episode file's last episode, if not
	// already recorded or renumbered.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [id]: the media item.
	//   - [episode]: the first episode number the file name gives.
	//   - [end]: the last episode number the file name gives.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	SetEpisodeEnd(ctx context.Context, id int64, episode, end int) error
	// [SetPixelFormat] records a media item's video pixel format.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [id]: the media item.
	//   - [pixelFormat]: the pixel format, such as "yuv420p".
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	SetPixelFormat(ctx context.Context, id int64, pixelFormat string) error
}

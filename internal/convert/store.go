package convert

import (
	"context"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [Store] is the database as the converter uses it; [store.Store]
// implements it.
type Store interface {
	// [ConversionCandidates] lists the media that have not failed conversion,
	// newest first, with just what [media.NeedsConversion] needs.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//
	// Returns:
	//   - [[]store.ConversionCandidate]: the candidates, newest first.
	//   - [error]: [nil] on success, or the database error.
	ConversionCandidates(ctx context.Context) ([]store.ConversionCandidate, error)
	// [MediaByID] loads one media item.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [id]: the media item's ID.
	//
	// Returns:
	//   - [store.Media]: the media item.
	//   - [error]: [nil] on success, [store.ErrNotFound] if it no longer exists,
	//     or another database error.
	MediaByID(ctx context.Context, id int64) (store.Media, error)
	// [ReplaceMediaFile] points a media item at its converted file, keeping its
	// ID so progress, subtitles, votes and metadata carry over, and clears any
	// recorded failure.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [id]: the media item's ID.
	//   - [replaced]: the converted file's path and details.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	ReplaceMediaFile(ctx context.Context, id int64, replaced store.ReplacedFile) error
	// [SetConvertFailed] records that converting a file failed, which takes it
	// out of the queue until an administrator retries it.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [id]: the media item's ID.
	//   - [reason]: the error to show in the control panel.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	SetConvertFailed(ctx context.Context, id int64, reason string) error
}

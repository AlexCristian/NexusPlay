package store

import (
	"context"
	"database/sql"
	"strings"
)

// [maximumConversionErrorBytes] is the most of a conversion error's text
// that is stored, in bytes.
const maximumConversionErrorBytes = 2000

// [ConversionCandidates] lists the media that have not failed conversion,
// newest first. The caller decides which of them actually need converting.
// The control panel polls this while conversions run, so it reads only the
// columns the decision needs.
//
// Parameters:
//   - [ctx]: cancels the query.
//
// Returns:
//   - [[]ConversionCandidate]: the candidates, most recently added first;
//     [nil] if there are none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) ConversionCandidates(ctx context.Context) ([]ConversionCandidate, error) {
	return queryAll(ctx, store.database, func(row rowScanner) (ConversionCandidate, error) {
		var candidate ConversionCandidate
		err := row.Scan(&candidate.ID, &candidate.Path, &candidate.VideoCodec, &candidate.AudioCodec, &candidate.PixelFormat)
		return candidate, err
	}, `SELECT id, path, video_codec, audio_codec, pix_fmt FROM media
		WHERE convert_status = '' ORDER BY added_at DESC, id DESC`)
}

// [ConversionFailures] lists the media whose conversion failed, by path, for
// the control panel.
//
// Parameters:
//   - [ctx]: cancels the query.
//
// Returns:
//   - [[]ConversionFailure]: the failures with their errors, or [nil] if
//     there are none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) ConversionFailures(ctx context.Context) ([]ConversionFailure, error) {
	return queryAll(ctx, store.database, func(row rowScanner) (ConversionFailure, error) {
		var failure ConversionFailure
		var err error
		failure.Media, err = scanMediaWith(row, &failure.Error)
		return failure, err
	}, `SELECT `+mediaColumns+`, m.convert_error`+mediaFrom+`
		WHERE m.convert_status = 'failed' ORDER BY m.path`)
}

// [SetConvertFailed] records that converting a file failed, which takes it
// out of the queue until [Store.RetryConversion].
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [id]: the media item.
//   - [reason]: the error to show; spaces around it are trimmed and it is
//     cut to [maximumConversionErrorBytes].
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) SetConvertFailed(ctx context.Context, id int64, reason string) error {
	_, err := store.database.ExecContext(ctx, `UPDATE media SET convert_status = 'failed', convert_error = ? WHERE id = ?`,
		truncateToBytes(strings.TrimSpace(reason), maximumConversionErrorBytes), id)
	return err
}

// [RetryConversion] puts a failed file back in the conversion queue and
// forgets its error.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [id]: the media item.
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) RetryConversion(ctx context.Context, id int64) error {
	_, err := store.database.ExecContext(ctx, `UPDATE media SET convert_status = '', convert_error = '' WHERE id = ?`, id)
	return err
}

// [ReplaceMediaFile] points a media item at its converted file and clears
// any recorded failure. The ID stays the same, so progress, subtitles, votes
// and metadata carry over.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [id]: the media item.
//   - [replaced]: the converted file's path and details.
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) ReplaceMediaFile(ctx context.Context, id int64, replaced ReplacedFile) error {
	return store.withTransaction(ctx, func(transaction *sql.Tx) error {
		_, err := transaction.ExecContext(ctx, `
			UPDATE media SET path = ?, duration = ?, container = ?, video_codec = ?, audio_codec = ?,
				pix_fmt = ?, size = ?, convert_status = '', convert_error = ''
			WHERE id = ?`, replaced.Path, replaced.Duration, replaced.Container, replaced.VideoCodec, replaced.AudioCodec, replaced.PixelFormat, replaced.Size, id)
		return err
	})
}

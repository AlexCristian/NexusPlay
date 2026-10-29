package store

import (
	"context"
	"database/sql"
	"strings"
)

// Conversion states stored in media.convert_status.
const (
	ConvertQueued = ""       // waiting, or nothing to do
	ConvertFailed = "failed" // gave up; an administrator can retry
)

// ConversionCandidate is what the converter needs to decide whether a file
// needs converting.
type ConversionCandidate struct {
	ID                                   int64
	Path, VideoCodec, AudioCodec, PixFmt string
}

// ConversionCandidates lists media that have not failed conversion, newest
// first. The caller decides which of them actually need converting. The
// control panel polls this while conversions run, so it reads only the
// columns the decision needs.
func (store *Store) ConversionCandidates(ctx context.Context) ([]ConversionCandidate, error) {
	return queryAll(ctx, store.database, func(row rowScanner) (ConversionCandidate, error) {
		var candidate ConversionCandidate
		err := row.Scan(&candidate.ID, &candidate.Path, &candidate.VideoCodec, &candidate.AudioCodec, &candidate.PixFmt)
		return candidate, err
	}, `SELECT id, path, video_codec, audio_codec, pix_fmt FROM media
		WHERE convert_status = '' ORDER BY added_at DESC, id DESC`)
}

// ConversionFailure is a file the converter gave up on.
type ConversionFailure struct {
	Media Media
	Error string
}

// ConversionFailures lists failed conversions for the control panel.
func (store *Store) ConversionFailures(ctx context.Context) ([]ConversionFailure, error) {
	return queryAll(ctx, store.database, func(row rowScanner) (ConversionFailure, error) {
		var failure ConversionFailure
		var err error
		failure.Media, err = scanMediaWith(row, &failure.Error)
		return failure, err
	}, `SELECT `+mediaCols+`, m.convert_error`+mediaFrom+`
		WHERE m.convert_status = 'failed' ORDER BY m.path`)
}

// SetConvertFailed records that converting a file failed.
func (store *Store) SetConvertFailed(ctx context.Context, id int64, reason string) error {
	_, err := store.database.ExecContext(ctx, `UPDATE media SET convert_status = 'failed', convert_error = ? WHERE id = ?`,
		truncateRunes(strings.TrimSpace(reason), 2000), id)
	return err
}

// RetryConversion puts a failed file back in the queue.
func (store *Store) RetryConversion(ctx context.Context, id int64) error {
	_, err := store.database.ExecContext(ctx, `UPDATE media SET convert_status = '', convert_error = '' WHERE id = ?`, id)
	return err
}

// ReplacedFile describes the converted file that took an original's place.
type ReplacedFile struct {
	Path       string
	Duration   float64
	Container  string
	VideoCodec string
	AudioCodec string
	PixFmt     string
	Size       int64
}

// ReplaceMediaFile points a media item at its converted file. The ID stays
// the same, so progress, subtitles, votes and metadata carry over.
func (store *Store) ReplaceMediaFile(ctx context.Context, id int64, replaced ReplacedFile) error {
	return store.withTransaction(ctx, func(transaction *sql.Tx) error {
		_, err := transaction.ExecContext(ctx, `
			UPDATE media SET path = ?, duration = ?, container = ?, video_codec = ?, audio_codec = ?,
				pix_fmt = ?, size = ?, convert_status = '', convert_error = ''
			WHERE id = ?`, replaced.Path, replaced.Duration, replaced.Container, replaced.VideoCodec, replaced.AudioCodec, replaced.PixFmt, replaced.Size, id)
		return err
	})
}

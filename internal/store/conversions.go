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

// ConversionCandidates lists media that have not failed conversion, newest
// first. The caller decides which of them actually need converting.
func (s *Store) ConversionCandidates(ctx context.Context) ([]Media, error) {
	return s.queryMedia(ctx, `WHERE m.convert_status = '' ORDER BY m.added_at DESC, m.id DESC`)
}

// ConversionFailure is a file the converter gave up on.
type ConversionFailure struct {
	Media Media
	Error string
}

// ConversionFailures lists failed conversions for the control panel.
func (s *Store) ConversionFailures(ctx context.Context) ([]ConversionFailure, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+mediaCols+`, m.convert_error`+mediaFrom+`
		WHERE m.convert_status = 'failed' ORDER BY m.path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ConversionFailure
	for rows.Next() {
		var f ConversionFailure
		var added, published int64
		m := &f.Media
		if err := rows.Scan(&m.ID, &m.Path, &m.Title, &m.Year, &m.Overview, &m.Category, &m.CollectionID,
			&m.Season, &m.Episode, &m.Poster, &m.Duration, &m.Container, &m.VideoCodec, &m.AudioCodec,
			&m.Size, &m.Published, &m.Views, &added, &published, &m.CollectionName, &f.Error); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// SetConvertFailed records that converting a file failed.
func (s *Store) SetConvertFailed(ctx context.Context, id int64, reason string) error {
	if len(reason) > 2000 {
		reason = reason[:2000]
	}
	_, err := s.db.ExecContext(ctx, `UPDATE media SET convert_status = 'failed', convert_error = ? WHERE id = ?`,
		strings.TrimSpace(reason), id)
	return err
}

// RetryConversion puts a failed file back in the queue.
func (s *Store) RetryConversion(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media SET convert_status = '', convert_error = '' WHERE id = ?`, id)
	return err
}

// ReplacedFile describes the converted file that took an original's place.
type ReplacedFile struct {
	Path       string
	Duration   float64
	Container  string
	VideoCodec string
	AudioCodec string
	Size       int64
}

// ReplaceMediaFile points a media item at its converted file. The ID stays
// the same, so progress, subtitles, votes and metadata carry over.
func (s *Store) ReplaceMediaFile(ctx context.Context, id int64, f ReplacedFile) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
			UPDATE media SET path = ?, duration = ?, container = ?, video_codec = ?, audio_codec = ?,
				size = ?, convert_status = '', convert_error = ''
			WHERE id = ?`, f.Path, f.Duration, f.Container, f.VideoCodec, f.AudioCodec, f.Size, id)
		return err
	})
}

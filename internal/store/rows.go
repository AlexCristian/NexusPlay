package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
	"unicode/utf8"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// rowScanner is either a *sql.Row or *sql.Rows.
type rowScanner interface {
	Scan(destinations ...any) error
}

// querier is either a *sql.DB or a *sql.Tx.
type querier interface {
	QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error)
}

// queryAll runs a query and converts every row with scan. It replaces the
// rows/defer/Next/Err loop that every list query would otherwise repeat.
func queryAll[T any](ctx context.Context, database querier, scan func(rowScanner) (T, error), query string, arguments ...any) ([]T, error) {
	rows, err := database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		value, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

// scanMediaWith scans the columns listed in mediaCols, followed by any extra
// destinations for columns a query selects after them. Every media query goes
// through it, so none of them can drift from mediaCols (two hand-written
// copies once forgot AddedAt and PublishedAt).
func scanMediaWith(row rowScanner, extra ...any) (Media, error) {
	var media Media
	var added, published int64
	destinations := []any{&media.ID, &media.Path, &media.Title, &media.Year, &media.Overview, &media.Category, &media.CollectionID,
		&media.Season, &media.Episode, &media.Poster, &media.Duration, &media.Container, &media.VideoCodec, &media.AudioCodec,
		&media.Size, &media.Published, &media.Views, &added, &published, &media.CollectionName, &media.PixFmt, &media.OwnPoster, &media.EpisodeEnd}
	if err := row.Scan(append(destinations, extra...)...); err != nil {
		return Media{}, err
	}
	media.AddedAt = time.Unix(added, 0)
	if published > 0 {
		media.PublishedAt = time.Unix(published, 0)
	}
	return media, nil
}

// scanMedia scans one row of mediaCols.
func scanMedia(row rowScanner) (Media, error) { return scanMediaWith(row) }

// isUniqueViolation reports whether err is SQLite rejecting a duplicate value
// in a UNIQUE column, judged by the error code rather than the message text.
func isUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

// truncateRunes shortens s to at most n bytes without cutting a UTF-8
// character in half.
func truncateRunes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

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
	Scan(dest ...any) error
}

// querier is either a *sql.DB or a *sql.Tx.
type querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// queryAll runs a query and converts every row with scan. It replaces the
// rows/defer/Next/Err loop that every list query would otherwise repeat.
func queryAll[T any](ctx context.Context, q querier, scan func(rowScanner) (T, error), query string, args ...any) ([]T, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []T
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// scanMediaWith scans the columns listed in mediaCols, followed by any extra
// destinations for columns a query selects after them. Every media query goes
// through it, so none of them can drift from mediaCols (two hand-written
// copies once forgot AddedAt and PublishedAt).
func scanMediaWith(row rowScanner, extra ...any) (Media, error) {
	var m Media
	var added, published int64
	dest := []any{&m.ID, &m.Path, &m.Title, &m.Year, &m.Overview, &m.Category, &m.CollectionID,
		&m.Season, &m.Episode, &m.Poster, &m.Duration, &m.Container, &m.VideoCodec, &m.AudioCodec,
		&m.Size, &m.Published, &m.Views, &added, &published, &m.CollectionName}
	if err := row.Scan(append(dest, extra...)...); err != nil {
		return Media{}, err
	}
	m.AddedAt = time.Unix(added, 0)
	if published > 0 {
		m.PublishedAt = time.Unix(published, 0)
	}
	return m, nil
}

// scanMedia scans one row of mediaCols.
func scanMedia(row rowScanner) (Media, error) { return scanMediaWith(row) }

// isUniqueViolation reports whether err is SQLite rejecting a duplicate value
// in a UNIQUE column, judged by the error code rather than the message text.
func isUniqueViolation(err error) bool {
	var e *sqlite.Error
	return errors.As(err, &e) && e.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

// truncateRunes shortens s to at most n bytes without cutting a UTF-8
// character in half.
func truncateRunes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

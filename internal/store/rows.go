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

// [rowScanner] is either an [*sql.Row] or [*sql.Rows]: anything that reads
// the columns of one row.
type rowScanner interface {
	// [Scan] reads the current row's columns.
	//
	// Parameters:
	//   - [destinations]: pointers to receive each column, in order.
	//
	// Returns:
	//   - [error]: [nil] on success, [sql.ErrNoRows] for an [*sql.Row] that
	//     matched nothing, or another driver error.
	Scan(destinations ...any) error
}

// [querier] is either an [*sql.DB] or an [*sql.Tx]: anything that runs a
// query.
type querier interface {
	// [QueryContext] runs a query that returns rows.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [query]: the SQL, with "?" placeholders.
	//   - [arguments]: the placeholders' values, in order.
	//
	// Returns:
	//   - [*sql.Rows]: the result, which the caller must close.
	//   - [error]: [nil] on success, or the driver's error.
	QueryContext(ctx context.Context, query string, arguments ...any) (*sql.Rows, error)
}

// [queryAll] runs a query and converts every row with a scan function. It
// replaces the rows/defer/Next/Err loop that every list query would
// otherwise repeat.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [database]: where to run it: the pool or a transaction.
//   - [scan]: converts one row into a value.
//   - [query]: the SQL, with "?" placeholders.
//   - [arguments]: the placeholders' values, in order.
//
// Returns:
//   - [[]T]: one value per row, in the query's order; [nil] if there are no
//     rows or the query or a scan fails. If reading the rows fails partway,
//     the values read so far come back with the error.
//   - [error]: [nil] on success, or the first error from the query, a scan
//     or reading the rows.
func queryAll[T any](ctx context.Context, database querier, scan func(rowScanner) (T, error), query string, arguments ...any) ([]T, error) {
	rows, err := database.QueryContext(ctx, query, arguments...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var values []T
	for rows.Next() {
		value, err := scan(rows)
		if err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}

// [scanMediaWith] reads the columns listed in [mediaColumns], followed by
// any extra columns a query selects after them. Every media query goes
// through it, so none of them can drift from [mediaColumns] (two
// hand-written copies once forgot [Media.AddedAt] and [Media.PublishedAt]).
//
// Parameters:
//   - [row]: the row to read.
//   - [extra]: pointers to receive the columns after [mediaColumns], in order.
//
// Returns:
//   - [Media]: the media item, or a zero [Media] if [error] is not [nil].
//     [Media.PublishedAt] is the zero time for unpublished media.
//   - [error]: [nil] on success, or the scan error.
func scanMediaWith(row rowScanner, extra ...any) (Media, error) {
	var media Media
	var added, published int64
	destinations := []any{&media.ID, &media.Path, &media.Title, &media.Year, &media.Overview, &media.Category, &media.CollectionID,
		&media.Season, &media.Episode, &media.Poster, &media.Duration, &media.Container, &media.VideoCodec, &media.AudioCodec,
		&media.Size, &media.Published, &media.Views, &added, &published, &media.CollectionName, &media.PixelFormat, &media.OwnPoster, &media.EpisodeEnd}
	if err := row.Scan(append(destinations, extra...)...); err != nil {
		return Media{}, err
	}
	media.AddedAt = time.Unix(added, 0)
	if published > 0 {
		media.PublishedAt = time.Unix(published, 0)
	}
	return media, nil
}

// [scanMedia] reads one row of [mediaColumns].
//
// Parameters:
//   - [row]: the row to read.
//
// Returns:
//   - [Media]: the media item, or a zero [Media] if [error] is not [nil].
//   - [error]: [nil] on success, or the scan error.
func scanMedia(row rowScanner) (Media, error) { return scanMediaWith(row) }

// [isUniqueViolation] reports whether SQLite rejected a write for a duplicate
// value in a UNIQUE column, judged by the error code rather than the message
// text.
//
// Parameters:
//   - [err]: an error from a write, or [nil].
//
// Returns:
//   - [bool]: [true] for a UNIQUE constraint violation, [false] otherwise.
func isUniqueViolation(err error) bool {
	var sqliteErr *sqlite.Error
	return errors.As(err, &sqliteErr) && sqliteErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

// [truncateToBytes] shortens text to fit a byte limit without cutting a UTF-8
// character in half.
//
// Parameters:
//   - [text]: the text to shorten.
//   - [limit]: the maximum length in bytes.
//
// Returns:
//   - [string]: [text] unchanged if it fits; otherwise its longest prefix of
//     at most [limit] bytes that ends between characters.
func truncateToBytes(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	for limit > 0 && !utf8.RuneStart(text[limit]) {
		limit--
	}
	return text[:limit]
}

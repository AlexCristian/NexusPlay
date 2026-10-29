package store

import (
	"time"
)

// [collectionColumns] are the columns every collection query selects, in the
// order [scanCollection] reads them.
const collectionColumns = `id, name, category, year, overview, poster, created_at`

// [scanCollection] reads one row of [collectionColumns].
//
// Parameters:
//   - [row]: the row to read.
//
// Returns:
//   - [Collection]: the collection.
//   - [error]: [nil] on success, or the scan error.
func scanCollection(row rowScanner) (Collection, error) {
	var collection Collection
	var created int64
	err := row.Scan(&collection.ID, &collection.Name, &collection.Category, &collection.Year, &collection.Overview, &collection.Poster, &created)
	collection.CreatedAt = time.Unix(created, 0)
	return collection, err
}

// [posterSQL] is the SQL for the new value of a poster column, taking three
// parameters: whether to remove the poster, then the new file name twice.
// It removes the poster, sets a new one if given, or keeps the old one. The
// old file is left for the [posters.Sweeper], since another row may use it
// too.
const posterSQL = `CASE WHEN ? THEN '' WHEN ? <> '' THEN ? ELSE poster END`

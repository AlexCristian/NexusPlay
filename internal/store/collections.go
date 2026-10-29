package store

import (
	"time"
)

const collectionCols = `id, name, category, year, overview, poster, created_at`

func scanCollection(row rowScanner) (Collection, error) {
	var collection Collection
	var created int64
	err := row.Scan(&collection.ID, &collection.Name, &collection.Category, &collection.Year, &collection.Overview, &collection.Poster, &created)
	collection.CreatedAt = time.Unix(created, 0)
	return collection, err
}

// posterSQL is the new value of a poster column, with the parameters
// (remove, poster, poster). The old file is left for posters.Sweeper, since
// another row may use it too.
const posterSQL = `CASE WHEN ? THEN '' WHEN ? <> '' THEN ? ELSE poster END`

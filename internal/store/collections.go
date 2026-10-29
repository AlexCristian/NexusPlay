package store

import (
	"context"
	"database/sql"
	"time"
)

// Collection groups media, typically a TV show organized by season.
type Collection struct {
	ID        int64
	Name      string
	Category  string
	Year      string
	Overview  string
	Poster    string
	CreatedAt time.Time
}

const collectionCols = `id, name, category, year, overview, poster, created_at`

func scanCollection(row rowScanner) (Collection, error) {
	var collection Collection
	var created int64
	err := row.Scan(&collection.ID, &collection.Name, &collection.Category, &collection.Year, &collection.Overview, &collection.Poster, &created)
	collection.CreatedAt = time.Unix(created, 0)
	return collection, err
}

// CreateCollection adds a collection. It returns ErrDuplicate if the name
// is taken, ignoring letter case.
func (store *Store) CreateCollection(ctx context.Context, collection Collection) (id int64, err error) {
	err = store.withTransaction(ctx, func(transaction *sql.Tx) error {
		if collection.Category, err = canonicalCategory(ctx, transaction, collection.Category); err != nil {
			return err
		}
		result, err := transaction.ExecContext(ctx,
			`INSERT INTO collections (name, category, year, overview, poster, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			collection.Name, collection.Category, collection.Year, collection.Overview, collection.Poster, store.unix())
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		if err != nil {
			return err
		}
		id, err = result.LastInsertId()
		return err
	})
	return id, err
}

// CollectionUpdate holds a collection's editable fields.
type CollectionUpdate struct {
	Name         string
	Category     string
	Year         string
	Overview     string
	Poster       string // a new poster file; empty keeps the current one
	RemovePoster bool   // clear the poster (Poster must be empty)
}

// posterSQL is the new value of a poster column, with the parameters
// (remove, poster, poster). The old file is left for posters.Sweeper, since
// another row may use it too.
const posterSQL = `CASE WHEN ? THEN '' WHEN ? <> '' THEN ? ELSE poster END`

// UpdateCollection saves a collection's fields. It returns ErrDuplicate if
// another collection has the name.
func (store *Store) UpdateCollection(ctx context.Context, id int64, update CollectionUpdate) error {
	return store.withTransaction(ctx, func(transaction *sql.Tx) error {
		category, err := canonicalCategory(ctx, transaction, update.Category)
		if err != nil {
			return err
		}
		_, err = transaction.ExecContext(ctx, `
			UPDATE collections SET name = ?, category = ?, year = ?, overview = ?, poster = `+posterSQL+`
			WHERE id = ?`, update.Name, category, update.Year, update.Overview, update.RemovePoster, update.Poster, update.Poster, id)
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	})
}

// DeleteCollection removes a collection; its media become standalone.
func (store *Store) DeleteCollection(ctx context.Context, id int64) error {
	_, err := store.database.ExecContext(ctx, `DELETE FROM collections WHERE id = ?`, id)
	return err
}

// CollectionByID returns one collection, or ErrNotFound.
func (store *Store) CollectionByID(ctx context.Context, id int64) (Collection, error) {
	collection, err := scanCollection(store.database.QueryRowContext(ctx, `SELECT `+collectionCols+` FROM collections WHERE id = ?`, id))
	return collection, notFound(err)
}

// CollectionByName returns one collection by name, case-insensitively, or
// ErrNotFound.
func (store *Store) CollectionByName(ctx context.Context, name string) (Collection, error) {
	collection, err := scanCollection(store.database.QueryRowContext(ctx, `SELECT `+collectionCols+` FROM collections WHERE name = ?`, name))
	return collection, notFound(err)
}

// Collections lists every collection by name.
func (store *Store) Collections(ctx context.Context) ([]Collection, error) {
	return queryAll(ctx, store.database, scanCollection, `SELECT `+collectionCols+` FROM collections ORDER BY name COLLATE NOCASE`)
}

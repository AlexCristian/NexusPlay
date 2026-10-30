package store

import (
	"context"
	"database/sql"
)

// [CreateCollection] adds a collection. [canonicalCategory] tidies its
// category.
//
// Parameters:
//   - [ctx]: cancels the insert.
//   - [collection]: the new collection; its [Collection.ID] and
//     [Collection.CreatedAt] are ignored.
//
// Returns:
//   - [int64]: the new collection's ID; not meaningful if [error] is not
//     [nil].
//   - [error]: [nil] on success; [ErrDuplicate] if the name is
//     taken, regardless of ASCII letter case; otherwise the database error.
func (store *Store) CreateCollection(ctx context.Context, collection Collection) (int64, error) {
	var id int64
	err := store.withTransaction(ctx, func(transaction *sql.Tx) error {
		category, err := canonicalCategory(ctx, transaction, collection.Category)
		if err != nil {
			return err
		}
		collection.Category = category
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

// [UpdateCollection] saves a collection's editable fields. [canonicalCategory]
// tidies its category.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [id]: the collection.
//   - [update]: the new values.
//
// Returns:
//   - [error]: [nil] on success, including when the collection does not
//     exist; [ErrDuplicate] if another collection has the name; otherwise the
//     database error.
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

// [DeleteCollection] removes a collection. Its media are kept and become
// standalone; its subscriptions are removed. Deleting one that does not
// exist is not an error.
//
// Parameters:
//   - [ctx]: cancels the delete.
//   - [id]: the collection.
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) DeleteCollection(ctx context.Context, id int64) error {
	_, err := store.database.ExecContext(ctx, `DELETE FROM collections WHERE id = ?`, id)
	return err
}

// [CollectionByID] loads one collection.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [id]: the collection.
//
// Returns:
//   - [Collection]: the collection.
//   - [error]: [nil] on success, [ErrNotFound] if it does not exist, or
//     another database error.
func (store *Store) CollectionByID(ctx context.Context, id int64) (Collection, error) {
	collection, err := scanCollection(store.database.QueryRowContext(ctx, `SELECT `+collectionColumns+` FROM collections WHERE id = ?`, id))
	return collection, notFound(err)
}

// [CollectionByName] finds a collection by name, regardless of ASCII letter case.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [name]: the collection's name.
//
// Returns:
//   - [Collection]: the collection.
//   - [error]: [nil] on success, [ErrNotFound] if no collection has that
//     name, or another database error.
func (store *Store) CollectionByName(ctx context.Context, name string) (Collection, error) {
	collection, err := scanCollection(store.database.QueryRowContext(ctx, `SELECT `+collectionColumns+` FROM collections WHERE name = ?`, name))
	return collection, notFound(err)
}

// [Collections] lists every collection, by name regardless of ASCII letter case.
//
// Parameters:
//   - [ctx]: cancels the query.
//
// Returns:
//   - [[]Collection]: the collections, or [nil] if there are none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) Collections(ctx context.Context) ([]Collection, error) {
	return queryAll(ctx, store.database, scanCollection, `SELECT `+collectionColumns+` FROM collections ORDER BY name COLLATE NOCASE`)
}

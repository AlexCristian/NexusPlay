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
	var c Collection
	var created int64
	err := row.Scan(&c.ID, &c.Name, &c.Category, &c.Year, &c.Overview, &c.Poster, &created)
	c.CreatedAt = time.Unix(created, 0)
	return c, err
}

// CreateCollection adds a collection. It returns ErrDuplicate if the name
// is taken, ignoring letter case.
func (s *Store) CreateCollection(ctx context.Context, c Collection) (id int64, err error) {
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if c.Category, err = canonicalCategory(ctx, tx, c.Category); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO collections (name, category, year, overview, poster, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
			c.Name, c.Category, c.Year, c.Overview, c.Poster, s.unix())
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		if err != nil {
			return err
		}
		id, err = res.LastInsertId()
		return err
	})
	return id, err
}

// UpdateCollection saves a collection's fields. An empty poster keeps the
// current one. It returns ErrDuplicate if another collection has the name.
func (s *Store) UpdateCollection(ctx context.Context, c Collection) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		if c.Category, err = canonicalCategory(ctx, tx, c.Category); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `
			UPDATE collections SET name = ?, category = ?, year = ?, overview = ?,
				poster = CASE WHEN ? <> '' THEN ? ELSE poster END
			WHERE id = ?`, c.Name, c.Category, c.Year, c.Overview, c.Poster, c.Poster, c.ID)
		if isUniqueViolation(err) {
			return ErrDuplicate
		}
		return err
	})
}

// DeleteCollection removes a collection; its media become standalone.
func (s *Store) DeleteCollection(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM collections WHERE id = ?`, id)
	return err
}

// CollectionByID returns one collection.
func (s *Store) CollectionByID(ctx context.Context, id int64) (Collection, error) {
	c, err := scanCollection(s.db.QueryRowContext(ctx, `SELECT `+collectionCols+` FROM collections WHERE id = ?`, id))
	return c, notFound(err)
}

// CollectionByName returns one collection by name, case-insensitively.
func (s *Store) CollectionByName(ctx context.Context, name string) (Collection, error) {
	c, err := scanCollection(s.db.QueryRowContext(ctx, `SELECT `+collectionCols+` FROM collections WHERE name = ?`, name))
	return c, notFound(err)
}

// Collections lists every collection by name.
func (s *Store) Collections(ctx context.Context) ([]Collection, error) {
	return queryAll(ctx, s.db, scanCollection, `SELECT `+collectionCols+` FROM collections ORDER BY name COLLATE NOCASE`)
}

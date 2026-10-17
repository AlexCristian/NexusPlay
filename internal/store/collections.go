package store

import (
	"context"
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

// CreateCollection adds a collection.
func (s *Store) CreateCollection(ctx context.Context, c Collection) (int64, error) {
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO collections (name, category, year, overview, poster, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		c.Name, c.Category, c.Year, c.Overview, c.Poster, s.unix())
	if err != nil {
		if isUniqueViolation(err) {
			return 0, ErrDuplicate
		}
		return 0, err
	}
	return res.LastInsertId()
}

// UpdateCollection saves a collection's fields. An empty poster keeps the current one.
func (s *Store) UpdateCollection(ctx context.Context, c Collection) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE collections SET name = ?, category = ?, year = ?, overview = ?,
			poster = CASE WHEN ? <> '' THEN ? ELSE poster END
		WHERE id = ?`, c.Name, c.Category, c.Year, c.Overview, c.Poster, c.Poster, c.ID)
	if isUniqueViolation(err) {
		return ErrDuplicate
	}
	return err
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

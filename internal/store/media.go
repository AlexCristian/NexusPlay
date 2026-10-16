package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// Media is one playable file.
type Media struct {
	ID           int64
	Path         string // relative to the media directory, always with forward slashes
	Title        string
	Year         string
	Overview     string
	Category     string
	CollectionID int64 // 0 when standalone
	Season       int
	Episode      int
	Poster       string // file name inside the poster directory
	Duration     float64
	Container    string
	VideoCodec   string
	AudioCodec   string
	Size         int64
	Published    bool
	Views        int
	AddedAt      time.Time
	PublishedAt  time.Time

	// Filled by queries that join collections.
	CollectionName string
}

// ScannedMedia is what the library scanner knows about a new file.
type ScannedMedia struct {
	Path       string
	Title      string
	Year       string
	Season     int
	Episode    int
	Duration   float64
	Container  string
	VideoCodec string
	AudioCodec string
	Size       int64
}

const mediaCols = `m.id, m.path, m.title, m.year, m.overview, m.category, COALESCE(m.collection_id, 0),
	m.season, m.episode, CASE WHEN m.poster <> '' THEN m.poster ELSE COALESCE(c.poster, '') END,
	m.duration, m.container, m.video_codec, m.audio_codec, m.size, m.published, m.views,
	m.added_at, m.published_at, COALESCE(c.name, '')`

const mediaFrom = ` FROM media m LEFT JOIN collections c ON c.id = m.collection_id `

func scanMedia(row interface{ Scan(...any) error }) (Media, error) {
	var m Media
	var added, published int64
	err := row.Scan(&m.ID, &m.Path, &m.Title, &m.Year, &m.Overview, &m.Category, &m.CollectionID,
		&m.Season, &m.Episode, &m.Poster, &m.Duration, &m.Container, &m.VideoCodec, &m.AudioCodec,
		&m.Size, &m.Published, &m.Views, &added, &published, &m.CollectionName)
	m.AddedAt = time.Unix(added, 0)
	if published > 0 {
		m.PublishedAt = time.Unix(published, 0)
	}
	return m, err
}

func (s *Store) queryMedia(ctx context.Context, where string, args ...any) ([]Media, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+mediaCols+mediaFrom+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Media
	for rows.Next() {
		m, err := scanMedia(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// InsertScannedMedia adds a newly discovered file as unpublished media.
// It returns the new ID, or 0 when the path was already known.
func (s *Store) InsertScannedMedia(ctx context.Context, sm ScannedMedia) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO media (path, title, year, season, episode, duration, container, video_codec, audio_codec, size, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO NOTHING`,
		sm.Path, sm.Title, sm.Year, sm.Season, sm.Episode, sm.Duration, sm.Container,
		sm.VideoCodec, sm.AudioCodec, sm.Size, s.unix())
	if err != nil {
		return 0, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, nil
	}
	return res.LastInsertId()
}

// SetScannedMetadata fills in details found by a metadata lookup. It only
// touches unpublished media, so an administrator's edits are never overwritten.
func (s *Store) SetScannedMetadata(ctx context.Context, id int64, title, year, overview, poster string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE media SET
			title = CASE WHEN ? <> '' THEN ? ELSE title END,
			year = CASE WHEN ? <> '' THEN ? ELSE year END,
			overview = CASE WHEN ? <> '' THEN ? ELSE overview END,
			poster = CASE WHEN ? <> '' THEN ? ELSE poster END
		WHERE id = ? AND published = 0`,
		title, title, year, year, overview, overview, poster, poster, id)
	return err
}

// MediaPaths returns every known path mapped to its media ID.
func (s *Store) MediaPaths(ctx context.Context) (map[string]int64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT path, id FROM media`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var p string
		var id int64
		if err := rows.Scan(&p, &id); err != nil {
			return nil, err
		}
		out[p] = id
	}
	return out, rows.Err()
}

// DeleteMedia removes a media row (used when its file disappears).
func (s *Store) DeleteMedia(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM media WHERE id = ?`, id)
	return err
}

// MediaByID returns one media item.
func (s *Store) MediaByID(ctx context.Context, id int64) (Media, error) {
	m, err := scanMedia(s.db.QueryRowContext(ctx, `SELECT `+mediaCols+mediaFrom+`WHERE m.id = ?`, id))
	return m, notFound(err)
}

// PendingMedia lists files waiting for an administrator to publish them.
func (s *Store) PendingMedia(ctx context.Context) ([]Media, error) {
	return s.queryMedia(ctx, `WHERE m.published = 0 ORDER BY m.path`)
}

// RecentlyPublished returns the newest published media.
func (s *Store) RecentlyPublished(ctx context.Context, limit int) ([]Media, error) {
	return s.queryMedia(ctx, `WHERE m.published = 1 ORDER BY m.published_at DESC, m.id DESC LIMIT ?`, limit)
}

// CollectionEpisodes lists the published media in a collection, by season and episode.
func (s *Store) CollectionEpisodes(ctx context.Context, collectionID int64) ([]Media, error) {
	return s.queryMedia(ctx, `WHERE m.published = 1 AND m.collection_id = ? ORDER BY m.season, m.episode, m.title`, collectionID)
}

// MediaUpdate holds the editable metadata fields.
type MediaUpdate struct {
	Title        string
	Year         string
	Overview     string
	Category     string
	CollectionID int64
	Season       int
	Episode      int
	Poster       string // empty keeps the current poster
}

// UpdateMedia saves metadata and, when publish is true, publishes the media and
// notifies everyone subscribed to its collection.
func (s *Store) UpdateMedia(ctx context.Context, id int64, u MediaUpdate, publish bool) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var coll any
		if u.CollectionID != 0 {
			coll = u.CollectionID
		}
		_, err := tx.ExecContext(ctx, `
			UPDATE media SET title = ?, year = ?, overview = ?, category = ?, collection_id = ?,
				season = ?, episode = ?, poster = CASE WHEN ? <> '' THEN ? ELSE poster END
			WHERE id = ?`,
			u.Title, u.Year, u.Overview, u.Category, coll, u.Season, u.Episode, u.Poster, u.Poster, id)
		if err != nil {
			return err
		}
		if !publish {
			return nil
		}
		res, err := tx.ExecContext(ctx, `UPDATE media SET published = 1, published_at = ? WHERE id = ? AND published = 0`, s.unix(), id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 || u.CollectionID == 0 {
			return nil
		}
		_, err = tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO notifications (user_id, media_id, created_at)
			SELECT user_id, ?, ? FROM subscriptions WHERE collection_id = ?`, id, s.unix(), u.CollectionID)
		return err
	})
}

// IncrementViews counts a play.
func (s *Store) IncrementViews(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media SET views = views + 1 WHERE id = ?`, id)
	return err
}

// Tile is one poster on the home page: a standalone item or a whole collection.
type Tile struct {
	IsCollection bool
	ID           int64
	Title        string
	Year         string
	Poster       string
	Watched      bool
}

// Category is a named row of tiles.
type Category struct {
	Name  string
	Tiles []Tile
}

// Categories groups everything published into categories. Media in a
// collection appears once, as the collection's tile, under the collection's category.
func (s *Store) Categories(ctx context.Context, userID int64) ([]Category, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT category, 0, id, title, year, poster,
		       EXISTS (SELECT 1 FROM watched w WHERE w.media_id = m.id AND w.user_id = ?)
		FROM media m WHERE published = 1 AND collection_id IS NULL
		UNION ALL
		SELECT c.category, 1, c.id, c.name, c.year, c.poster, 0
		FROM collections c WHERE EXISTS (SELECT 1 FROM media m WHERE m.collection_id = c.id AND m.published = 1)
		ORDER BY 1 COLLATE NOCASE, 4 COLLATE NOCASE`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var cat string
		var t Tile
		if err := rows.Scan(&cat, &t.IsCollection, &t.ID, &t.Title, &t.Year, &t.Poster, &t.Watched); err != nil {
			return nil, err
		}
		if cat == "" {
			cat = "Other"
		}
		if len(out) == 0 || out[len(out)-1].Name != cat {
			out = append(out, Category{Name: cat})
		}
		out[len(out)-1].Tiles = append(out[len(out)-1].Tiles, t)
	}
	return out, rows.Err()
}

// RecentTiles returns tiles for the newest published media, collapsing
// episodes into their collection.
func (s *Store) RecentTiles(ctx context.Context, userID int64, limit int) ([]Tile, error) {
	recent, err := s.RecentlyPublished(ctx, limit*4)
	if err != nil {
		return nil, err
	}
	seen := map[int64]bool{}
	var out []Tile
	for _, m := range recent {
		if len(out) == limit {
			break
		}
		if m.CollectionID != 0 {
			if seen[m.CollectionID] {
				continue
			}
			seen[m.CollectionID] = true
			c, err := s.CollectionByID(ctx, m.CollectionID)
			if err != nil {
				return nil, err
			}
			out = append(out, Tile{IsCollection: true, ID: c.ID, Title: c.Name, Year: c.Year, Poster: c.Poster})
			continue
		}
		w, err := s.IsWatched(ctx, userID, m.ID)
		if err != nil {
			return nil, err
		}
		out = append(out, Tile{ID: m.ID, Title: m.Title, Year: m.Year, Poster: m.Poster, Watched: w})
	}
	return out, nil
}

// Search finds published standalone media and collections whose name contains q.
func (s *Store) Search(ctx context.Context, userID int64, q string, limit int) ([]Tile, error) {
	pattern := "%" + likeEscape(q) + "%"
	rows, err := s.db.QueryContext(ctx, `
		SELECT 0, id, title, year, poster,
		       EXISTS (SELECT 1 FROM watched w WHERE w.media_id = m.id AND w.user_id = ?)
		FROM media m WHERE published = 1 AND collection_id IS NULL AND title LIKE ? ESCAPE '\'
		UNION ALL
		SELECT 1, c.id, c.name, c.year, c.poster, 0
		FROM collections c WHERE c.name LIKE ? ESCAPE '\'
		  AND EXISTS (SELECT 1 FROM media m WHERE m.collection_id = c.id AND m.published = 1)
		ORDER BY 3 COLLATE NOCASE LIMIT ?`, userID, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tile
	for rows.Next() {
		var t Tile
		if err := rows.Scan(&t.IsCollection, &t.ID, &t.Title, &t.Year, &t.Poster, &t.Watched); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CategoryNames lists the categories in use, for the admin form.
func (s *Store) CategoryNames(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT category FROM media WHERE category <> ''
		UNION SELECT category FROM collections WHERE category <> ''
		ORDER BY 1 COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

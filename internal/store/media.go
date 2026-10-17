package store

import (
	"context"
	"database/sql"
	"errors"
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
	Poster       string // file name in the poster directory: its own, else its collection's
	OwnPoster    bool   // Poster is the media's own, not its collection's
	Duration     float64
	Container    string
	VideoCodec   string
	AudioCodec   string
	PixFmt       string // video pixel format, e.g. "yuv420p"; "" if not probed yet
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
	PixFmt     string
	Size       int64
}

const mediaCols = `m.id, m.path, m.title, m.year, m.overview, m.category, COALESCE(m.collection_id, 0),
	m.season, m.episode, CASE WHEN m.poster <> '' THEN m.poster ELSE COALESCE(c.poster, '') END,
	m.duration, m.container, m.video_codec, m.audio_codec, m.size, m.published, m.views,
	m.added_at, m.published_at, COALESCE(c.name, ''), m.pix_fmt, m.poster <> ''`

const mediaFrom = ` FROM media m LEFT JOIN collections c ON c.id = m.collection_id `

// queryMedia lists media rows. where is the SQL after the FROM clause
// (filters, ordering, limit).
func (s *Store) queryMedia(ctx context.Context, where string, args ...any) ([]Media, error) {
	return queryAll(ctx, s.db, scanMedia, `SELECT `+mediaCols+mediaFrom+where, args...)
}

// InsertScannedMedia adds a newly discovered file as unpublished media. If
// the path is already known it changes nothing and returns inserted == false.
func (s *Store) InsertScannedMedia(ctx context.Context, sm ScannedMedia) (id int64, inserted bool, err error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO media (path, title, year, season, episode, duration, container, video_codec, audio_codec, pix_fmt, size, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO NOTHING`,
		sm.Path, sm.Title, sm.Year, sm.Season, sm.Episode, sm.Duration, sm.Container,
		sm.VideoCodec, sm.AudioCodec, sm.PixFmt, sm.Size, s.unix())
	if err != nil {
		return 0, false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n == 0 {
		return 0, false, err
	}
	id, err = res.LastInsertId()
	return id, err == nil, err
}

// ScannedMetadata is what an online lookup found for a new file. Empty
// fields mean "nothing found" and leave the current value alone.
type ScannedMetadata struct {
	Title, Year, Overview string
	Poster                string // file name in the poster directory
}

// SetScannedMetadata fills in details found by a metadata lookup. It only
// touches unpublished media, so an administrator's edits are never
// overwritten, and only fields the lookup found: here an empty string means
// "the lookup found nothing", so it keeps the current value.
func (s *Store) SetScannedMetadata(ctx context.Context, id int64, md ScannedMetadata) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE media SET
			title = CASE WHEN ? <> '' THEN ? ELSE title END,
			year = CASE WHEN ? <> '' THEN ? ELSE year END,
			overview = CASE WHEN ? <> '' THEN ? ELSE overview END,
			poster = CASE WHEN ? <> '' THEN ? ELSE poster END
		WHERE id = ? AND published = 0`,
		md.Title, md.Title, md.Year, md.Year, md.Overview, md.Overview, md.Poster, md.Poster, id)
	return err
}

// H264MissingPixFmt returns H.264 media whose pixel format has not been
// recorded yet (added before it was stored), path mapped to ID.
func (s *Store) H264MissingPixFmt(ctx context.Context) (map[string]int64, error) {
	type row struct {
		path string
		id   int64
	}
	rows, err := queryAll(ctx, s.db, func(r rowScanner) (row, error) {
		var x row
		err := r.Scan(&x.path, &x.id)
		return x, err
	}, `SELECT path, id FROM media WHERE video_codec = 'h264' AND pix_fmt = ''`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, x := range rows {
		out[x.path] = x.id
	}
	return out, nil
}

// SetPixFmt records a media item's video pixel format.
func (s *Store) SetPixFmt(ctx context.Context, id int64, pixFmt string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE media SET pix_fmt = ? WHERE id = ?`, pixFmt, id)
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

// MediaByID returns one media item, published or not, or ErrNotFound.
func (s *Store) MediaByID(ctx context.Context, id int64) (Media, error) {
	m, err := scanMedia(s.db.QueryRowContext(ctx, `SELECT `+mediaCols+mediaFrom+`WHERE m.id = ?`, id))
	return m, notFound(err)
}

// PendingMedia lists files waiting for an administrator to publish them.
func (s *Store) PendingMedia(ctx context.Context) ([]Media, error) {
	return s.queryMedia(ctx, `WHERE m.published = 0 ORDER BY m.path`)
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
	Poster       string // a new poster file; empty keeps the current one
	RemovePoster bool   // clear the poster (Poster must be empty)
}

// UpdateMedia saves metadata and, when publish is true, publishes the media and
// notifies everyone subscribed to its collection.
func (s *Store) UpdateMedia(ctx context.Context, id int64, u MediaUpdate, publish bool) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		var coll any
		if u.CollectionID != 0 {
			coll = u.CollectionID
		}
		category, err := canonicalCategory(ctx, tx, u.Category)
		if err != nil {
			return err
		}
		u.Category = category
		_, err = tx.ExecContext(ctx, `
			UPDATE media SET title = ?, year = ?, overview = ?, category = ?, collection_id = ?,
				season = ?, episode = ?, poster = `+posterSQL+`
			WHERE id = ?`,
			u.Title, u.Year, u.Overview, u.Category, coll, u.Season, u.Episode, u.RemovePoster, u.Poster, u.Poster, id)
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
		n, err := res.RowsAffected()
		if err != nil || n == 0 || u.CollectionID == 0 {
			return err // nil when already published or not in a collection: nobody to notify
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

// PosterNames returns the poster files any media or collection uses.
func (s *Store) PosterNames(ctx context.Context) (map[string]bool, error) {
	names, err := queryAll(ctx, s.db, scanString,
		`SELECT poster FROM media WHERE poster <> '' UNION SELECT poster FROM collections WHERE poster <> ''`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(names))
	for _, n := range names {
		out[n] = true
	}
	return out, nil
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
	return s.categorized(ctx, userID, "", -1)
}

// Search is Categories limited to standalone media and collections whose
// name contains q, at most limit tiles in all.
func (s *Store) Search(ctx context.Context, userID int64, q string, limit int) ([]Category, error) {
	return s.categorized(ctx, userID, q, limit)
}

// categorized lists tiles whose name contains q ("" matches everything),
// grouped by category, at most limit of them (-1 for no limit).
func (s *Store) categorized(ctx context.Context, userID int64, q string, limit int) ([]Category, error) {
	type row struct {
		category string
		tile     Tile
	}
	pattern := "%" + likeEscape(q) + "%"
	// Category names are compared without regard to letter case. Writes
	// already reuse an existing spelling (canonicalCategory); grouping here
	// also merges rows saved before that rule existed.
	rows, err := queryAll(ctx, s.db, func(r rowScanner) (row, error) {
		var x row
		t := &x.tile
		err := r.Scan(&x.category, &t.IsCollection, &t.ID, &t.Title, &t.Year, &t.Poster, &t.Watched)
		return x, err
	}, `
		SELECT COALESCE(NULLIF(TRIM(category), ''), 'Other'), 0, id, title, year, poster,
		       EXISTS (SELECT 1 FROM watched w WHERE w.media_id = m.id AND w.user_id = ?)
		FROM media m WHERE published = 1 AND collection_id IS NULL AND title LIKE ? ESCAPE '\'
		UNION ALL
		SELECT COALESCE(NULLIF(TRIM(c.category), ''), 'Other'), 1, c.id, c.name, c.year, c.poster, 0
		FROM collections c WHERE c.name LIKE ? ESCAPE '\'
		  AND EXISTS (SELECT 1 FROM media m WHERE m.collection_id = c.id AND m.published = 1)
		ORDER BY 1 COLLATE NOCASE, 4 COLLATE NOCASE LIMIT ?`, userID, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	var out []Category
	for _, x := range rows {
		if len(out) == 0 || !strings.EqualFold(out[len(out)-1].Name, x.category) {
			out = append(out, Category{Name: x.category})
		}
		out[len(out)-1].Tiles = append(out[len(out)-1].Tiles, x.tile)
	}
	return out, nil
}

// RecentTiles returns tiles for the newest published media. Episodes appear
// as their collection's tile, placed by the collection's newest episode.
func (s *Store) RecentTiles(ctx context.Context, userID int64, limit int) ([]Tile, error) {
	return queryAll(ctx, s.db, func(r rowScanner) (Tile, error) {
		var t Tile
		err := r.Scan(&t.IsCollection, &t.ID, &t.Title, &t.Year, &t.Poster, &t.Watched)
		return t, err
	}, `
		WITH newest AS (
			SELECT collection_id IS NOT NULL AS is_coll, COALESCE(collection_id, id) AS id,
			       MAX(published_at) AS at, MAX(id) AS last_id
			FROM media WHERE published = 1
			GROUP BY 1, 2
		)
		SELECT n.is_coll, n.id, COALESCE(c.name, m.title), COALESCE(c.year, m.year), COALESCE(c.poster, m.poster),
		       NOT n.is_coll AND EXISTS (SELECT 1 FROM watched w WHERE w.media_id = n.id AND w.user_id = ?)
		FROM newest n
		LEFT JOIN collections c ON n.is_coll AND c.id = n.id
		LEFT JOIN media m ON NOT n.is_coll AND m.id = n.id
		ORDER BY n.at DESC, n.last_id DESC
		LIMIT ?`, userID, limit)
}

// CategoryNames lists the categories in use, for the admin form.
func (s *Store) CategoryNames(ctx context.Context) ([]string, error) {
	return queryAll(ctx, s.db, scanString, `
		SELECT category FROM media WHERE category <> ''
		UNION SELECT category FROM collections WHERE category <> ''
		ORDER BY 1 COLLATE NOCASE`)
}

// canonicalCategory trims a category name and, if a category with the same
// name in different letter case already exists, returns that spelling, so
// "movies" joins "Movies" instead of starting a second home-page tab.
func canonicalCategory(ctx context.Context, tx *sql.Tx, name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", nil
	}
	var existing string
	err := tx.QueryRowContext(ctx, `
		SELECT category FROM (
			SELECT category, MIN(id) AS first FROM media WHERE category = ? COLLATE NOCASE GROUP BY category
			UNION ALL
			SELECT category, MIN(id) FROM collections WHERE category = ? COLLATE NOCASE GROUP BY category
		) ORDER BY first LIMIT 1`, name, name).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return name, nil
	}
	return existing, err
}

func scanString(r rowScanner) (string, error) {
	var v string
	err := r.Scan(&v)
	return v, err
}

func likeEscape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// queryMedia lists media rows. where is the SQL after the FROM clause
// (filters, ordering, limit).
func (store *Store) queryMedia(ctx context.Context, where string, arguments ...any) ([]Media, error) {
	return queryAll(ctx, store.database, scanMedia, `SELECT `+mediaCols+mediaFrom+where, arguments...)
}

// InsertScannedMedia adds a newly discovered file as unpublished media. If
// the path is already known it changes nothing and returns inserted == false.
func (store *Store) InsertScannedMedia(ctx context.Context, scanned ScannedMedia) (id int64, inserted bool, err error) {
	result, err := store.database.ExecContext(ctx, `
		INSERT INTO media (path, title, year, season, episode, episode_end, duration, container, video_codec, audio_codec, pix_fmt, size, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO NOTHING`,
		scanned.Path, scanned.Title, scanned.Year, scanned.Season, scanned.Episode, scanned.EpisodeEnd, scanned.Duration, scanned.Container,
		scanned.VideoCodec, scanned.AudioCodec, scanned.PixFmt, scanned.Size, store.unix())
	if err != nil {
		return 0, false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil || rowsAffected == 0 {
		return 0, false, err
	}
	id, err = result.LastInsertId()
	return id, err == nil, err
}

// SetScannedMetadata fills in details found by a metadata lookup and
// records that the lookup happened. It only touches unpublished media, so an
// administrator's edits are never overwritten, and only fields the lookup
// found: here an empty string means "the lookup found nothing", so it keeps
// the current value.
func (store *Store) SetScannedMetadata(ctx context.Context, id int64, scannedMetadata ScannedMetadata) error {
	_, err := store.database.ExecContext(ctx, `
		UPDATE media SET
			title = CASE WHEN ? <> '' THEN ? ELSE title END,
			year = CASE WHEN ? <> '' THEN ? ELSE year END,
			overview = CASE WHEN ? <> '' THEN ? ELSE overview END,
			poster = CASE WHEN ? <> '' THEN ? ELSE poster END,
			metadata_checked_at = ?
		WHERE id = ? AND published = 0`,
		scannedMetadata.Title, scannedMetadata.Title, scannedMetadata.Year, scannedMetadata.Year, scannedMetadata.Overview, scannedMetadata.Overview, scannedMetadata.Poster, scannedMetadata.Poster, store.unix(), id)
	return err
}

// ReplaceMetadata is SetScannedMetadata for an administrator's explicit
// "Refresh metadata": it applies to published media too. It returns
// ErrNotFound if the media does not exist.
func (store *Store) ReplaceMetadata(ctx context.Context, id int64, scannedMetadata ScannedMetadata) error {
	result, err := store.database.ExecContext(ctx, `
		UPDATE media SET
			title = CASE WHEN ? <> '' THEN ? ELSE title END,
			year = CASE WHEN ? <> '' THEN ? ELSE year END,
			overview = CASE WHEN ? <> '' THEN ? ELSE overview END,
			poster = CASE WHEN ? <> '' THEN ? ELSE poster END,
			metadata_checked_at = ?
		WHERE id = ?`,
		scannedMetadata.Title, scannedMetadata.Title, scannedMetadata.Year, scannedMetadata.Year, scannedMetadata.Overview, scannedMetadata.Overview, scannedMetadata.Poster, scannedMetadata.Poster, store.unix(), id)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err == nil && rowsAffected == 0 {
		err = ErrNotFound
	}
	return err
}

// MarkMetadataChecked records a lookup that found nothing to save.
func (store *Store) MarkMetadataChecked(ctx context.Context, id int64) error {
	_, err := store.database.ExecContext(ctx, `UPDATE media SET metadata_checked_at = ? WHERE id = ?`, store.unix(), id)
	return err
}

// MediaNeedingMetadata lists unpublished media with no poster or no
// description of its own whose last lookup was before checkedBefore,
// least recently checked first, at most limit of them.
func (store *Store) MediaNeedingMetadata(ctx context.Context, checkedBefore time.Time, limit int) ([]Media, error) {
	return store.queryMedia(ctx, `WHERE m.published = 0 AND (m.poster = '' OR m.overview = '')
		AND m.metadata_checked_at < ? ORDER BY m.metadata_checked_at, m.id LIMIT ?`, checkedBefore.Unix(), limit)
}

// H264MissingPixFmt returns H.264 media whose pixel format has not been
// recorded yet (added before it was stored), path mapped to ID.
func (store *Store) H264MissingPixFmt(ctx context.Context) (map[string]int64, error) {
	type row struct {
		path string
		id   int64
	}
	rows, err := queryAll(ctx, store.database, func(scannedRow rowScanner) (row, error) {
		var entry row
		err := scannedRow.Scan(&entry.path, &entry.id)
		return entry, err
	}, `SELECT path, id FROM media WHERE video_codec = 'h264' AND pix_fmt = ''`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(rows))
	for _, entry := range rows {
		out[entry.path] = entry.id
	}
	return out, nil
}

// SetPixFmt records a media item's video pixel format.
func (store *Store) SetPixFmt(ctx context.Context, id int64, pixFmt string) error {
	_, err := store.database.ExecContext(ctx, `UPDATE media SET pix_fmt = ? WHERE id = ?`, pixFmt, id)
	return err
}

// MediaPaths returns every known path mapped to its media ID.
func (store *Store) MediaPaths(ctx context.Context) (map[string]int64, error) {
	rows, err := store.database.QueryContext(ctx, `SELECT path, id FROM media`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var path string
		var id int64
		if err := rows.Scan(&path, &id); err != nil {
			return nil, err
		}
		out[path] = id
	}
	return out, rows.Err()
}

// DeleteMedia removes a media row (used when its file disappears).
func (store *Store) DeleteMedia(ctx context.Context, id int64) error {
	_, err := store.database.ExecContext(ctx, `DELETE FROM media WHERE id = ?`, id)
	return err
}

// MediaByID returns one media item, published or not, or ErrNotFound.
func (store *Store) MediaByID(ctx context.Context, id int64) (Media, error) {
	media, err := scanMedia(store.database.QueryRowContext(ctx, `SELECT `+mediaCols+mediaFrom+`WHERE m.id = ?`, id))
	return media, notFound(err)
}

// PendingMedia lists files waiting for an administrator to publish them.
func (store *Store) PendingMedia(ctx context.Context) ([]Media, error) {
	return store.queryMedia(ctx, `WHERE m.published = 0 ORDER BY m.path`)
}

// SetEpisodeEnd records the last episode of a multi-episode file, for files
// scanned before the range was stored. It changes only rows with no range
// whose episode number is still the one the file name gives, so an
// administrator's renumbering is left alone.
func (store *Store) SetEpisodeEnd(ctx context.Context, id int64, episode, end int) error {
	_, err := store.database.ExecContext(ctx, `UPDATE media SET episode_end = ? WHERE id = ? AND episode_end = 0 AND episode = ?`, end, id, episode)
	return err
}

// CollectionEpisodes lists the published media in a collection, by season and episode.
func (store *Store) CollectionEpisodes(ctx context.Context, collectionID int64) ([]Media, error) {
	return store.queryMedia(ctx, `WHERE m.published = 1 AND m.collection_id = ? ORDER BY m.season, m.episode, m.title`, collectionID)
}

// UpdateMedia saves metadata and, when publish is true, publishes the media and
// notifies everyone subscribed to its collection.
func (store *Store) UpdateMedia(ctx context.Context, id int64, update MediaUpdate, publish bool) error {
	return store.withTransaction(ctx, func(transaction *sql.Tx) error {
		var coll any
		if update.CollectionID != 0 {
			coll = update.CollectionID
		}
		category, err := canonicalCategory(ctx, transaction, update.Category)
		if err != nil {
			return err
		}
		update.Category = category
		_, err = transaction.ExecContext(ctx, `
			UPDATE media SET title = ?, year = ?, overview = ?, category = ?, collection_id = ?,
				season = ?, episode = ?, episode_end = ?, poster = `+posterSQL+`
			WHERE id = ?`,
			update.Title, update.Year, update.Overview, update.Category, coll, update.Season, update.Episode, update.EpisodeEnd, update.RemovePoster, update.Poster, update.Poster, id)
		if err != nil {
			return err
		}
		if !publish {
			return nil
		}
		result, err := transaction.ExecContext(ctx, `UPDATE media SET published = 1, published_at = ? WHERE id = ? AND published = 0`, store.unix(), id)
		if err != nil {
			return err
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil || rowsAffected == 0 || update.CollectionID == 0 {
			return err // nil when already published or not in a collection: nobody to notify
		}
		_, err = transaction.ExecContext(ctx, `
			INSERT OR IGNORE INTO notifications (user_id, media_id, created_at)
			SELECT user_id, ?, ? FROM subscriptions WHERE collection_id = ?`, id, store.unix(), update.CollectionID)
		return err
	})
}

// IncrementViews counts a play.
func (store *Store) IncrementViews(ctx context.Context, id int64) error {
	_, err := store.database.ExecContext(ctx, `UPDATE media SET views = views + 1 WHERE id = ?`, id)
	return err
}

// PosterNames returns the poster files any media or collection uses.
func (store *Store) PosterNames(ctx context.Context) (map[string]bool, error) {
	names, err := queryAll(ctx, store.database, scanString,
		`SELECT poster FROM media WHERE poster <> '' UNION SELECT poster FROM collections WHERE poster <> ''`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(names))
	for _, name := range names {
		out[name] = true
	}
	return out, nil
}

// Categories groups everything published into categories. Media in a
// collection appears once, as the collection's tile, under the collection's category.
func (store *Store) Categories(ctx context.Context, userID int64) ([]Category, error) {
	return store.categorized(ctx, userID, "", -1)
}

// Search is Categories limited to standalone media and collections whose
// name contains q, at most limit tiles in all.
func (store *Store) Search(ctx context.Context, userID int64, query string, limit int) ([]Category, error) {
	return store.categorized(ctx, userID, query, limit)
}

// categorized lists tiles whose name contains q ("" matches everything),
// grouped by category, at most limit of them (-1 for no limit).
func (store *Store) categorized(ctx context.Context, userID int64, query string, limit int) ([]Category, error) {
	type row struct {
		category string
		tile     Tile
	}
	pattern := "%" + likeEscape(query) + "%"
	// Category names are compared without regard to letter case. Writes
	// already reuse an existing spelling (canonicalCategory); grouping here
	// also merges rows saved before that rule existed.
	rows, err := queryAll(ctx, store.database, func(scannedRow rowScanner) (row, error) {
		var entry row
		tile := &entry.tile
		err := scannedRow.Scan(&entry.category, &tile.IsCollection, &tile.ID, &tile.Title, &tile.Year, &tile.Poster, &tile.Watched)
		return entry, err
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
	for _, entry := range rows {
		if len(out) == 0 || !strings.EqualFold(out[len(out)-1].Name, entry.category) {
			out = append(out, Category{Name: entry.category})
		}
		out[len(out)-1].Tiles = append(out[len(out)-1].Tiles, entry.tile)
	}
	return out, nil
}

// RecentTiles returns tiles for the newest published media. Episodes appear
// as their collection's tile, placed by the collection's newest episode.
func (store *Store) RecentTiles(ctx context.Context, userID int64, limit int) ([]Tile, error) {
	return queryAll(ctx, store.database, func(row rowScanner) (Tile, error) {
		var tile Tile
		err := row.Scan(&tile.IsCollection, &tile.ID, &tile.Title, &tile.Year, &tile.Poster, &tile.Watched)
		return tile, err
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
func (store *Store) CategoryNames(ctx context.Context) ([]string, error) {
	return queryAll(ctx, store.database, scanString, `
		SELECT category FROM media WHERE category <> ''
		UNION SELECT category FROM collections WHERE category <> ''
		ORDER BY 1 COLLATE NOCASE`)
}

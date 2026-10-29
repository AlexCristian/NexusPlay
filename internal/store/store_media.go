package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/collections"
)

// [queryMedia] lists media rows selected with [mediaColumns].
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [where]: the SQL after the FROM clause: joins, filters, ordering and
//     limit, using the aliases "m" and "c" of [mediaFrom].
//   - [arguments]: the values of [where]'s placeholders, in order.
//
// Returns:
//   - [[]Media]: the matching media, or [nil] if there are none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) queryMedia(ctx context.Context, where string, arguments ...any) ([]Media, error) {
	return queryAll(ctx, store.database, scanMedia, `SELECT `+mediaColumns+mediaFrom+where, arguments...)
}

// [InsertScannedMedia] adds a newly found file as unpublished media. A path
// that is already known is left unchanged.
//
// Parameters:
//   - [ctx]: cancels the insert.
//   - [scanned]: what the scanner knows about the file.
//
// Returns:
//   - [int64] ([id]): the new media item's ID, or 0 if the path was already
//     known.
//   - [bool] ([inserted]): [true] if a row was added, [false] if the path
//     was already known.
//   - [error]: [nil] on success, or the database error.
func (store *Store) InsertScannedMedia(ctx context.Context, scanned ScannedMedia) (id int64, inserted bool, err error) {
	result, err := store.database.ExecContext(ctx, `
		INSERT INTO media (path, title, year, season, episode, episode_end, duration, container, video_codec, audio_codec, pix_fmt, size, added_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(path) DO NOTHING`,
		scanned.Path, scanned.Title, scanned.Year, scanned.Season, scanned.Episode, scanned.EpisodeEnd, scanned.Duration, scanned.Container,
		scanned.VideoCodec, scanned.AudioCodec, scanned.PixelFormat, scanned.Size, store.unix())
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

// [SetScannedMetadata] fills in details found by a lookup and records that
// the lookup happened. It only changes unpublished media, so an
// administrator's edits are never overwritten, and only the fields the
// lookup found: a field that is "" keeps the current value. For a published
// item it does nothing.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [id]: the media item.
//   - [scannedMetadata]: what the lookup found.
//
// Returns:
//   - [error]: [nil] on success, including when nothing matched, or the
//     database error.
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

// [ReplaceMetadata] is [Store.SetScannedMetadata] for an administrator's
// explicit "Refresh metadata": it applies to published media too.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [id]: the media item.
//   - [scannedMetadata]: what the lookup found; fields that are "" keep
//     their current value.
//
// Returns:
//   - [error]: [nil] on success, [ErrNotFound] if the media item does not
//     exist, or another database error.
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

// [MarkMetadataChecked] records that a lookup ran and found nothing to save,
// so [Store.MediaNeedingMetadata] does not offer the item again too soon.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [id]: the media item.
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) MarkMetadataChecked(ctx context.Context, id int64) error {
	_, err := store.database.ExecContext(ctx, `UPDATE media SET metadata_checked_at = ? WHERE id = ?`, store.unix(), id)
	return err
}

// [MediaNeedingMetadata] lists unpublished media missing a poster or a
// description of its own whose last lookup was before a given time, least
// recently checked first.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [checkedBefore]: only items last looked up before this are listed.
//   - [limit]: the most items to list.
//
// Returns:
//   - [[]Media]: the items, or [nil] if there are none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) MediaNeedingMetadata(ctx context.Context, checkedBefore time.Time, limit int) ([]Media, error) {
	return store.queryMedia(ctx, `WHERE m.published = 0 AND (m.poster = '' OR m.overview = '')
		AND m.metadata_checked_at < ? ORDER BY m.metadata_checked_at, m.id LIMIT ?`, checkedBefore.Unix(), limit)
}

// [H264MissingPixelFormat] finds H.264 media whose pixel format has not been
// recorded, because they were added before it was stored.
//
// Parameters:
//   - [ctx]: cancels the query.
//
// Returns:
//   - [map[string]int64]: each such file's path mapped to its media ID.
//   - [error]: [nil] on success, or the database error.
func (store *Store) H264MissingPixelFormat(ctx context.Context) (map[string]int64, error) {
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
	pathIDs := make(map[string]int64, len(rows))
	for _, entry := range rows {
		pathIDs[entry.path] = entry.id
	}
	return pathIDs, nil
}

// [SetPixelFormat] records a media item's video pixel format.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [id]: the media item.
//   - [pixelFormat]: the pixel format as ffprobe names it, such as
//     "yuv420p".
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) SetPixelFormat(ctx context.Context, id int64, pixelFormat string) error {
	_, err := store.database.ExecContext(ctx, `UPDATE media SET pix_fmt = ? WHERE id = ?`, pixelFormat, id)
	return err
}

// [MediaPaths] lists every file the library knows, published or not.
//
// Parameters:
//   - [ctx]: cancels the query.
//
// Returns:
//   - [map[string]int64]: each path, relative to the media folder, mapped to
//     its media ID; an empty map if there are none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) MediaPaths(ctx context.Context) (map[string]int64, error) {
	rows, err := store.database.QueryContext(ctx, `SELECT path, id FROM media`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	pathIDs := map[string]int64{}
	for rows.Next() {
		var path string
		var id int64
		if err := rows.Scan(&path, &id); err != nil {
			return nil, err
		}
		pathIDs[path] = id
	}
	return pathIDs, rows.Err()
}

// [DeleteMedia] removes a media item, such as when its file disappears.
// Its progress, watched marks, notifications and subtitle tracks go with it.
// Deleting an item that does not exist is not an error.
//
// Parameters:
//   - [ctx]: cancels the delete.
//   - [id]: the media item.
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) DeleteMedia(ctx context.Context, id int64) error {
	_, err := store.database.ExecContext(ctx, `DELETE FROM media WHERE id = ?`, id)
	return err
}

// [MediaByID] loads one media item, published or not. Callers that serve
// viewers must check [Media.Published] themselves.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [id]: the media item.
//
// Returns:
//   - [Media]: the media item.
//   - [error]: [nil] on success, [ErrNotFound] if it does not exist, or
//     another database error.
func (store *Store) MediaByID(ctx context.Context, id int64) (Media, error) {
	media, err := scanMedia(store.database.QueryRowContext(ctx, `SELECT `+mediaColumns+mediaFrom+`WHERE m.id = ?`, id))
	return media, notFound(err)
}

// [PendingMedia] lists the files waiting for an administrator to publish
// them, by path.
//
// Parameters:
//   - [ctx]: cancels the query.
//
// Returns:
//   - [[]Media]: the unpublished media, or [nil] if there are none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) PendingMedia(ctx context.Context) ([]Media, error) {
	return store.queryMedia(ctx, `WHERE m.published = 0 ORDER BY m.path`)
}

// [SetEpisodeEnd] records the last episode of a multi-episode file, for files
// scanned before the range was stored. It changes only an item with no range
// whose episode number is still the one the file name gives, so an
// administrator's renumbering is left alone.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [id]: the media item.
//   - [episode]: the first episode number the file name gives.
//   - [end]: the last episode number the file name gives.
//
// Returns:
//   - [error]: [nil] on success, including when nothing matched, or the
//     database error.
func (store *Store) SetEpisodeEnd(ctx context.Context, id int64, episode, end int) error {
	_, err := store.database.ExecContext(ctx, `UPDATE media SET episode_end = ? WHERE id = ? AND episode_end = 0 AND episode = ?`, end, id, episode)
	return err
}

// [CollectionEpisodes] lists the published media in a collection, by season,
// episode and title.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [collectionID]: the collection.
//
// Returns:
//   - [[]Media]: the media, or [nil] if there are none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) CollectionEpisodes(ctx context.Context, collectionID int64) ([]Media, error) {
	return store.queryMedia(ctx, `WHERE m.published = 1 AND m.collection_id = ? ORDER BY m.season, m.episode, m.title`, collectionID)
}

// [UpdateMedia] saves a media item's editable fields and, if asked,
// publishes it. Publishing an item in a collection for the first time
// notifies everyone subscribed to the collection, in the same transaction.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [id]: the media item.
//   - [update]: the new values; [canonicalCategory] tidies the category.
//   - [publish]: [true] to publish the item as well; publishing an item that
//     is already published changes nothing and notifies nobody.
//
// Returns:
//   - [error]: [nil] on success, including when the item does not exist, or
//     the database error.
func (store *Store) UpdateMedia(ctx context.Context, id int64, update MediaUpdate, publish bool) error {
	return store.withTransaction(ctx, func(transaction *sql.Tx) error {
		var collectionValue any
		if update.CollectionID != 0 {
			collectionValue = update.CollectionID
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
			update.Title, update.Year, update.Overview, update.Category, collectionValue, update.Season, update.Episode, update.EpisodeEnd, update.RemovePoster, update.Poster, update.Poster, id)
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
			return err // [nil] when already published or not in a collection: nobody to notify
		}
		_, err = transaction.ExecContext(ctx, `
			INSERT OR IGNORE INTO notifications (user_id, media_id, created_at)
			SELECT user_id, ?, ? FROM subscriptions WHERE collection_id = ?`, id, store.unix(), update.CollectionID)
		return err
	})
}

// [IncrementViews] counts one more play of a media item.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [id]: the media item.
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) IncrementViews(ctx context.Context, id int64) error {
	_, err := store.database.ExecContext(ctx, `UPDATE media SET views = views + 1 WHERE id = ?`, id)
	return err
}

// [PosterNames] lists the poster files that any media or collection uses,
// so the [posters.Sweeper] keeps them.
//
// Parameters:
//   - [ctx]: cancels the query.
//
// Returns:
//   - [collections.Set[string]]: the file names in use, relative to the
//     poster folder.
//   - [error]: [nil] on success, or the database error.
func (store *Store) PosterNames(ctx context.Context) (collections.Set[string], error) {
	names, err := queryAll(ctx, store.database, scanString,
		`SELECT poster FROM media WHERE poster <> '' UNION SELECT poster FROM collections WHERE poster <> ''`)
	if err != nil {
		return collections.Set[string]{}, err
	}
	return collections.NewSet(names...), nil
}

// [Categories] groups everything published into home-page rows. The media
// of a collection appear once, as the collection's tile, under the
// collection's category; a collection with no published media does not
// appear.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [userID]: the viewer, for [Tile.Watched], or 0 for anonymous.
//
// Returns:
//   - [[]Category]: the rows by name, regardless of letter case, each with
//     its tiles by title; [nil] if nothing is published.
//   - [error]: [nil] on success, or the database error.
func (store *Store) Categories(ctx context.Context, userID int64) ([]Category, error) {
	return store.categorized(ctx, userID, "", -1)
}

// [Search] is [Store.Categories] limited to standalone media and collections
// whose title contains some text.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [userID]: the viewer, for [Tile.Watched], or 0 for anonymous.
//   - [query]: the text to look for, matched as typed ("%" and "_" are not
//     wildcards); ASCII letters match regardless of case.
//   - [limit]: the most tiles to return in all.
//
// Returns:
//   - [[]Category]: the matching tiles in rows, as [Store.Categories]
//     returns them.
//   - [error]: [nil] on success, or the database error.
func (store *Store) Search(ctx context.Context, userID int64, query string, limit int) ([]Category, error) {
	return store.categorized(ctx, userID, query, limit)
}

// [categorized] lists published tiles whose title contains some text,
// grouped into rows by category, as [Store.Categories] and [Store.Search]
// describe.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [userID]: the viewer, for [Tile.Watched].
//   - [query]: the text to look for; "" matches everything.
//   - [limit]: the most tiles to return in all, or -1 for no limit.
//
// Returns:
//   - [[]Category]: the rows, or [nil] if nothing matched.
//   - [error]: [nil] on success, or the database error.
func (store *Store) categorized(ctx context.Context, userID int64, query string, limit int) ([]Category, error) {
	type row struct {
		category string
		tile     Tile
	}
	pattern := "%" + likeEscape(query) + "%"
	// The names of categories are compared without regard to letter case. Writes
	// already reuse an existing spelling ([canonicalCategory]); grouping here
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
	var categories []Category
	for _, entry := range rows {
		if len(categories) == 0 || !strings.EqualFold(categories[len(categories)-1].Name, entry.category) {
			categories = append(categories, Category{Name: entry.category})
		}
		categories[len(categories)-1].Tiles = append(categories[len(categories)-1].Tiles, entry.tile)
	}
	return categories, nil
}

// [RecentTiles] lists tiles for the most recently published media. Episodes
// appear as their collection's tile, placed by the collection's most
// recently published episode, so a busy show takes one tile.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [userID]: the viewer, for [Tile.Watched], or 0 for anonymous.
//   - [limit]: the most tiles to return.
//
// Returns:
//   - [[]Tile]: the tiles, newest first, or [nil] if nothing is published.
//   - [error]: [nil] on success, or the database error.
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

// [CategoryNames] lists the categories in use by media or collections, for
// the control panel's suggestions.
//
// Parameters:
//   - [ctx]: cancels the query.
//
// Returns:
//   - [[]string]: the names, sorted regardless of letter case; [nil] if
//     there are none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) CategoryNames(ctx context.Context) ([]string, error) {
	return queryAll(ctx, store.database, scanString, `
		SELECT category FROM media WHERE category <> ''
		UNION SELECT category FROM collections WHERE category <> ''
		ORDER BY 1 COLLATE NOCASE`)
}

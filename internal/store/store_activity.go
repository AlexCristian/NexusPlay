package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/AlexCristian/NexusPlay/internal/collections"
)

// [SaveProgress] records where a user is in a media item. When [finished]
// is [true] it instead deletes the saved position and marks the item
// watched; deciding when playback counts as finished is the caller's job.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [userID]: the viewer.
//   - [mediaID]: the media item.
//   - [position]: seconds from the start; ignored when [finished] is
//     [true].
//   - [finished]: [true] if the user reached the end.
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) SaveProgress(ctx context.Context, userID, mediaID int64, position float64, finished bool) error {
	return store.withTransaction(ctx, func(transaction *sql.Tx) error {
		if finished {
			if _, err := transaction.ExecContext(ctx, `DELETE FROM watch_progress WHERE user_id = ? AND media_id = ?`, userID, mediaID); err != nil {
				return err
			}
			_, err := transaction.ExecContext(ctx,
				`INSERT OR REPLACE INTO watched (user_id, media_id, watched_at) VALUES (?, ?, ?)`,
				userID, mediaID, store.unix())
			return err
		}
		_, err := transaction.ExecContext(ctx, `
			INSERT INTO watch_progress (user_id, media_id, position, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id, media_id) DO UPDATE SET position = excluded.position, updated_at = excluded.updated_at`,
			userID, mediaID, position, store.unix())
		return err
	})
}

// [Progress] gets where a user stopped in a media item.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [userID]: the viewer.
//   - [mediaID]: the media item.
//
// Returns:
//   - [float64]: the saved position in seconds, or 0 if none is saved.
//   - [error]: [nil] on success, or the database error.
func (store *Store) Progress(ctx context.Context, userID, mediaID int64) (float64, error) {
	var position float64
	err := store.database.QueryRowContext(ctx,
		`SELECT position FROM watch_progress WHERE user_id = ? AND media_id = ?`, userID, mediaID).Scan(&position)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return position, err
}

// [InProgress] lists the published media a user stopped partway through,
// most recently watched first.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [userID]: the viewer.
//   - [limit]: the most items to list.
//
// Returns:
//   - [[]Resume]: the items with their positions, or [nil] if there are
//     none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) InProgress(ctx context.Context, userID int64, limit int) ([]Resume, error) {
	return queryAll(ctx, store.database, func(row rowScanner) (Resume, error) {
		var resume Resume
		var err error
		resume.Media, err = scanMediaWith(row, &resume.Position)
		return resume, err
	}, `SELECT `+mediaColumns+`, p.position`+mediaFrom+`
		JOIN watch_progress p ON p.media_id = m.id
		WHERE p.user_id = ? AND m.published = 1 ORDER BY p.updated_at DESC LIMIT ?`, userID, limit)
}

// [IsWatched] reports whether a user has finished a media item.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [userID]: the viewer.
//   - [mediaID]: the media item.
//
// Returns:
//   - [bool]: [true] if the user finished it, [false] otherwise.
//   - [error]: [nil] on success, or the database error.
func (store *Store) IsWatched(ctx context.Context, userID, mediaID int64) (bool, error) {
	var watched bool
	err := store.database.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM watched WHERE user_id = ? AND media_id = ?)`, userID, mediaID).Scan(&watched)
	return watched, err
}

// [WatchedIn] lists which media of a collection a user has finished.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [userID]: the viewer.
//   - [collectionID]: the collection.
//
// Returns:
//   - [collections.Set[int64]]: the IDs of the finished media, published or
//     not.
//   - [error]: [nil] on success, or the database error.
func (store *Store) WatchedIn(ctx context.Context, userID, collectionID int64) (collections.Set[int64], error) {
	rows, err := store.database.QueryContext(ctx, `
		SELECT w.media_id FROM watched w JOIN media m ON m.id = w.media_id
		WHERE w.user_id = ? AND m.collection_id = ?`, userID, collectionID)
	if err != nil {
		return collections.Set[int64]{}, err
	}
	defer rows.Close()
	watched := collections.NewSet[int64]()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return collections.Set[int64]{}, err
		}
		watched.Add(id)
	}
	return watched, rows.Err()
}

// [SubscriptionCount] counts the collections a user is subscribed to.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [userID]: the viewer.
//
// Returns:
//   - [int]: the number of subscriptions.
//   - [error]: [nil] on success, or the database error.
func (store *Store) SubscriptionCount(ctx context.Context, userID int64) (int, error) {
	var count int
	err := store.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE user_id = ?`, userID).Scan(&count)
	return count, err
}

// [IsSubscribed] reports whether a user follows a collection.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [userID]: the viewer.
//   - [collectionID]: the collection.
//
// Returns:
//   - [bool]: [true] if subscribed, [false] otherwise.
//   - [error]: [nil] on success, or the database error.
func (store *Store) IsSubscribed(ctx context.Context, userID, collectionID int64) (bool, error) {
	var subscribed bool
	err := store.database.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM subscriptions WHERE user_id = ? AND collection_id = ?)`,
		userID, collectionID).Scan(&subscribed)
	return subscribed, err
}

// [ToggleSubscription] subscribes a user to a collection, or unsubscribes
// them if they already were.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [userID]: the viewer.
//   - [collectionID]: the collection.
//
// Returns:
//   - [bool]: [true] if the user is now subscribed, [false] if they are now
//     unsubscribed.
//   - [error]: [nil] on success, or the database error, such as for a
//     collection that does not exist.
func (store *Store) ToggleSubscription(ctx context.Context, userID, collectionID int64) (bool, error) {
	var subscribed bool
	err := store.withTransaction(ctx, func(transaction *sql.Tx) error {
		result, err := transaction.ExecContext(ctx, `DELETE FROM subscriptions WHERE user_id = ? AND collection_id = ?`, userID, collectionID)
		if err != nil {
			return err
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil || rowsAffected > 0 {
			return err // unsubscribed
		}
		subscribed = true
		_, err = transaction.ExecContext(ctx, `INSERT INTO subscriptions (user_id, collection_id) VALUES (?, ?)`, userID, collectionID)
		return err
	})
	return subscribed, err
}

// [Notifications] lists the published media a user was notified about,
// because they were published in a subscribed collection, newest first.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [userID]: the viewer.
//   - [limit]: the most items to list.
//
// Returns:
//   - [[]Media]: the media, or [nil] if there are none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) Notifications(ctx context.Context, userID int64, limit int) ([]Media, error) {
	return store.queryMedia(ctx, `JOIN notifications n ON n.media_id = m.id
		WHERE n.user_id = ? AND m.published = 1 ORDER BY n.created_at DESC LIMIT ?`, userID, limit)
}

// [ClearNotification] dismisses a notification once the user opens the
// media it is about. Clearing one that does not exist is not an error.
//
// Parameters:
//   - [ctx]: cancels the delete.
//   - [userID]: the viewer.
//   - [mediaID]: the media item.
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) ClearNotification(ctx context.Context, userID, mediaID int64) error {
	_, err := store.database.ExecContext(ctx, `DELETE FROM notifications WHERE user_id = ? AND media_id = ?`, userID, mediaID)
	return err
}

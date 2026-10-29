package store

import (
	"context"
	"database/sql"
	"errors"

	"github.com/AlexCristian/NexusPlay/internal/collections"
	_ "modernc.org/sqlite"
)

// SaveProgress records a playback position. A position of zero, or one
// close to the end, clears it and marks the media as watched.
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

// Progress returns the saved position for a media item, or 0.
func (store *Store) Progress(ctx context.Context, userID, mediaID int64) (float64, error) {
	var position float64
	err := store.database.QueryRowContext(ctx,
		`SELECT position FROM watch_progress WHERE user_id = ? AND media_id = ?`, userID, mediaID).Scan(&position)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return position, err
}

// InProgress lists what a user paused, most recent first.
func (store *Store) InProgress(ctx context.Context, userID int64, limit int) ([]Resume, error) {
	return queryAll(ctx, store.database, func(row rowScanner) (Resume, error) {
		var resume Resume
		var err error
		resume.Media, err = scanMediaWith(row, &resume.Position)
		return resume, err
	}, `SELECT `+mediaCols+`, p.position`+mediaFrom+`
		JOIN watch_progress p ON p.media_id = m.id
		WHERE p.user_id = ? AND m.published = 1 ORDER BY p.updated_at DESC LIMIT ?`, userID, limit)
}

// IsWatched reports whether a user finished a media item.
func (store *Store) IsWatched(ctx context.Context, userID, mediaID int64) (bool, error) {
	var watched bool
	err := store.database.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM watched WHERE user_id = ? AND media_id = ?)`, userID, mediaID).Scan(&watched)
	return watched, err
}

// WatchedIn returns the set of media in a collection a user finished.
func (store *Store) WatchedIn(ctx context.Context, userID, collectionID int64) (collections.Set[int64], error) {
	rows, err := store.database.QueryContext(ctx, `
		SELECT w.media_id FROM watched w JOIN media m ON m.id = w.media_id
		WHERE w.user_id = ? AND m.collection_id = ?`, userID, collectionID)
	if err != nil {
		return collections.Set[int64]{}, err
	}
	defer rows.Close()
	out := collections.NewSet[int64]()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return collections.Set[int64]{}, err
		}
		out.Add(id)
	}
	return out, rows.Err()
}

// SubscriptionCount returns how many collections a user is subscribed to.
func (store *Store) SubscriptionCount(ctx context.Context, userID int64) (int, error) {
	var count int
	err := store.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE user_id = ?`, userID).Scan(&count)
	return count, err
}

// IsSubscribed reports whether a user follows a collection.
func (store *Store) IsSubscribed(ctx context.Context, userID, collectionID int64) (bool, error) {
	var subscribed bool
	err := store.database.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM subscriptions WHERE user_id = ? AND collection_id = ?)`,
		userID, collectionID).Scan(&subscribed)
	return subscribed, err
}

// ToggleSubscription flips a subscription and returns the new state.
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

// Notifications lists new media in a user's subscriptions, newest first.
func (store *Store) Notifications(ctx context.Context, userID int64, limit int) ([]Media, error) {
	return store.queryMedia(ctx, `JOIN notifications n ON n.media_id = m.id
		WHERE n.user_id = ? AND m.published = 1 ORDER BY n.created_at DESC LIMIT ?`, userID, limit)
}

// ClearNotification dismisses a notification once the user opens that media.
func (store *Store) ClearNotification(ctx context.Context, userID, mediaID int64) error {
	_, err := store.database.ExecContext(ctx, `DELETE FROM notifications WHERE user_id = ? AND media_id = ?`, userID, mediaID)
	return err
}

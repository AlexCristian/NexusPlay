package store

import (
	"context"
	"database/sql"
	"errors"
)

// Resume is a partly watched item for the "Previously watching" list.
type Resume struct {
	Media    Media
	Position float64
}

// SaveProgress records a playback position. A position of zero, or one
// close to the end, clears it and marks the media as watched.
func (s *Store) SaveProgress(ctx context.Context, userID, mediaID int64, position float64, finished bool) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if finished {
			if _, err := tx.ExecContext(ctx, `DELETE FROM watch_progress WHERE user_id = ? AND media_id = ?`, userID, mediaID); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx,
				`INSERT OR REPLACE INTO watched (user_id, media_id, watched_at) VALUES (?, ?, ?)`,
				userID, mediaID, s.unix())
			return err
		}
		_, err := tx.ExecContext(ctx, `
			INSERT INTO watch_progress (user_id, media_id, position, updated_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(user_id, media_id) DO UPDATE SET position = excluded.position, updated_at = excluded.updated_at`,
			userID, mediaID, position, s.unix())
		return err
	})
}

// Progress returns the saved position for a media item, or 0.
func (s *Store) Progress(ctx context.Context, userID, mediaID int64) (float64, error) {
	var pos float64
	err := s.db.QueryRowContext(ctx,
		`SELECT position FROM watch_progress WHERE user_id = ? AND media_id = ?`, userID, mediaID).Scan(&pos)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return pos, err
}

// InProgress lists what a user paused, most recent first.
func (s *Store) InProgress(ctx context.Context, userID int64, limit int) ([]Resume, error) {
	return queryAll(ctx, s.db, func(r rowScanner) (Resume, error) {
		var res Resume
		var err error
		res.Media, err = scanMediaWith(r, &res.Position)
		return res, err
	}, `SELECT `+mediaCols+`, p.position`+mediaFrom+`
		JOIN watch_progress p ON p.media_id = m.id
		WHERE p.user_id = ? AND m.published = 1 ORDER BY p.updated_at DESC LIMIT ?`, userID, limit)
}

// IsWatched reports whether a user finished a media item.
func (s *Store) IsWatched(ctx context.Context, userID, mediaID int64) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM watched WHERE user_id = ? AND media_id = ?)`, userID, mediaID).Scan(&ok)
	return ok, err
}

// WatchedIn returns the set of media in a collection a user finished.
func (s *Store) WatchedIn(ctx context.Context, userID, collectionID int64) (map[int64]bool, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT w.media_id FROM watched w JOIN media m ON m.id = w.media_id
		WHERE w.user_id = ? AND m.collection_id = ?`, userID, collectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// SubscriptionCount returns how many collections a user is subscribed to.
func (s *Store) SubscriptionCount(ctx context.Context, userID int64) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM subscriptions WHERE user_id = ?`, userID).Scan(&n)
	return n, err
}

// IsSubscribed reports whether a user follows a collection.
func (s *Store) IsSubscribed(ctx context.Context, userID, collectionID int64) (bool, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM subscriptions WHERE user_id = ? AND collection_id = ?)`,
		userID, collectionID).Scan(&ok)
	return ok, err
}

// ToggleSubscription flips a subscription and returns the new state.
func (s *Store) ToggleSubscription(ctx context.Context, userID, collectionID int64) (bool, error) {
	var subscribed bool
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `DELETE FROM subscriptions WHERE user_id = ? AND collection_id = ?`, userID, collectionID)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil || n > 0 {
			return err // unsubscribed
		}
		subscribed = true
		_, err = tx.ExecContext(ctx, `INSERT INTO subscriptions (user_id, collection_id) VALUES (?, ?)`, userID, collectionID)
		return err
	})
	return subscribed, err
}

// Notifications lists new media in a user's subscriptions, newest first.
func (s *Store) Notifications(ctx context.Context, userID int64, limit int) ([]Media, error) {
	return s.queryMedia(ctx, `JOIN notifications n ON n.media_id = m.id
		WHERE n.user_id = ? AND m.published = 1 ORDER BY n.created_at DESC LIMIT ?`, userID, limit)
}

// ClearNotification dismisses a notification once the user opens that media.
func (s *Store) ClearNotification(ctx context.Context, userID, mediaID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM notifications WHERE user_id = ? AND media_id = ?`, userID, mediaID)
	return err
}

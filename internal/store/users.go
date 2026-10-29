package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// User is a registered account.
type User struct {
	ID           int64
	Email        string
	Nickname     string
	PasswordHash string
	IsAdmin      bool
	CreatedAt    time.Time
}

// Session is a logged-in browser session.
type Session struct {
	UserID    int64
	CSRFToken string
	ExpiresAt time.Time
}

// ErrDuplicate is returned when an email or nickname is already taken.
var ErrDuplicate = errors.New("already exists")

// ErrUserLimit is returned by CreateUser when the account limit is reached.
var ErrUserLimit = errors.New("account limit reached")

const userCols = `id, email, nickname, password_hash, is_admin, created_at`

func scanUser(row rowScanner) (User, error) {
	var user User
	var created int64
	err := row.Scan(&user.ID, &user.Email, &user.Nickname, &user.PasswordHash, &user.IsAdmin, &created)
	user.CreatedAt = time.Unix(created, 0)
	return user, err
}

// CreateUser inserts a user. The first account ever created becomes the
// administrator, so a fresh install needs no separate setup step. It
// returns ErrDuplicate if the email or nickname is taken (compared without
// regard to letter case), and ErrUserLimit if maxUsers accounts already
// exist. maxUsers 0 means no limit. The count and the insert are one
// transaction, so two people registering at once cannot both take the last
// place.
func (store *Store) CreateUser(ctx context.Context, email, nickname, passwordHash string, maxUsers int) (User, error) {
	var user User
	err := store.withTransaction(ctx, func(transaction *sql.Tx) error {
		var count int
		if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
			return err
		}
		if maxUsers > 0 && count >= maxUsers {
			return ErrUserLimit
		}
		result, err := transaction.ExecContext(ctx,
			`INSERT INTO users (email, nickname, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?, ?)`,
			email, nickname, passwordHash, count == 0, store.unix())
		if err != nil {
			if isUniqueViolation(err) {
				return ErrDuplicate
			}
			return err
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		user, err = scanUser(transaction.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
		return err
	})
	return user, err
}

// UserCount returns how many accounts exist.
func (store *Store) UserCount(ctx context.Context) (int, error) {
	var count int
	err := store.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// UserByEmail looks a user up by email, case-insensitively, or returns
// ErrNotFound.
func (store *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	user, err := scanUser(store.database.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, email))
	return user, notFound(err)
}

// UserByID looks a user up by ID, or returns ErrNotFound.
func (store *Store) UserByID(ctx context.Context, id int64) (User, error) {
	user, err := scanUser(store.database.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
	return user, notFound(err)
}

// UpdatePasswordHash replaces a user's stored hash (used to upgrade hash parameters).
func (store *Store) UpdatePasswordHash(ctx context.Context, id int64, hash string) error {
	_, err := store.database.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return err
}

// CreateSession stores a session keyed by the hash of its token.
func (store *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, csrf string, expires time.Time) error {
	_, err := store.database.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at) VALUES (?, ?, ?, ?)`,
		tokenHash, userID, csrf, expires.Unix())
	return err
}

// SessionUser returns the session and its user. It returns ErrNotFound when
// there is no such session or it has expired.
func (store *Store) SessionUser(ctx context.Context, tokenHash string) (Session, User, error) {
	var sess Session
	var exp int64
	row := store.database.QueryRowContext(ctx, `
		SELECT s.user_id, s.csrf_token, s.expires_at,
		       u.id, u.email, u.nickname, u.password_hash, u.is_admin, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, store.unix())
	var user User
	var created int64
	err := row.Scan(&sess.UserID, &sess.CSRFToken, &exp,
		&user.ID, &user.Email, &user.Nickname, &user.PasswordHash, &user.IsAdmin, &created)
	if err != nil {
		return sess, user, notFound(err)
	}
	sess.ExpiresAt = time.Unix(exp, 0)
	user.CreatedAt = time.Unix(created, 0)
	return sess, user, nil
}

// DeleteSession logs a session out.
func (store *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := store.database.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteExpiredSessions removes stale sessions.
func (store *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := store.database.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, store.unix())
	return err
}

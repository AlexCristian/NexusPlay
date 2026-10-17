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
	var u User
	var created int64
	err := row.Scan(&u.ID, &u.Email, &u.Nickname, &u.PasswordHash, &u.IsAdmin, &created)
	u.CreatedAt = time.Unix(created, 0)
	return u, err
}

// CreateUser inserts a user. The first account ever created becomes the
// administrator, so a fresh install needs no separate setup step. It
// returns ErrDuplicate if the email or nickname is taken (compared without
// regard to letter case), and ErrUserLimit if maxUsers accounts already
// exist. maxUsers 0 means no limit. The count and the insert are one
// transaction, so two people registering at once cannot both take the last
// place.
func (s *Store) CreateUser(ctx context.Context, email, nickname, passwordHash string, maxUsers int) (User, error) {
	var u User
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
			return err
		}
		if maxUsers > 0 && count >= maxUsers {
			return ErrUserLimit
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO users (email, nickname, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?, ?)`,
			email, nickname, passwordHash, count == 0, s.unix())
		if err != nil {
			if isUniqueViolation(err) {
				return ErrDuplicate
			}
			return err
		}
		id, err := res.LastInsertId()
		if err != nil {
			return err
		}
		u, err = scanUser(tx.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
		return err
	})
	return u, err
}

// UserCount returns how many accounts exist.
func (s *Store) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// UserByEmail looks a user up by email, case-insensitively, or returns
// ErrNotFound.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, email))
	return u, notFound(err)
}

// UserByID looks a user up by ID, or returns ErrNotFound.
func (s *Store) UserByID(ctx context.Context, id int64) (User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
	return u, notFound(err)
}

// UpdatePasswordHash replaces a user's stored hash (used to upgrade hash parameters).
func (s *Store) UpdatePasswordHash(ctx context.Context, id int64, hash string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return err
}

// CreateSession stores a session keyed by the hash of its token.
func (s *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, csrf string, expires time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at) VALUES (?, ?, ?, ?)`,
		tokenHash, userID, csrf, expires.Unix())
	return err
}

// SessionUser returns the session and its user. It returns ErrNotFound when
// there is no such session or it has expired.
func (s *Store) SessionUser(ctx context.Context, tokenHash string) (Session, User, error) {
	var sess Session
	var exp int64
	row := s.db.QueryRowContext(ctx, `
		SELECT s.user_id, s.csrf_token, s.expires_at,
		       u.id, u.email, u.nickname, u.password_hash, u.is_admin, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, s.unix())
	var u User
	var created int64
	err := row.Scan(&sess.UserID, &sess.CSRFToken, &exp,
		&u.ID, &u.Email, &u.Nickname, &u.PasswordHash, &u.IsAdmin, &created)
	if err != nil {
		return sess, u, notFound(err)
	}
	sess.ExpiresAt = time.Unix(exp, 0)
	u.CreatedAt = time.Unix(created, 0)
	return sess, u, nil
}

// DeleteSession logs a session out.
func (s *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// DeleteExpiredSessions removes stale sessions.
func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, s.unix())
	return err
}

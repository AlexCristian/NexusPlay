package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
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

const userCols = `id, email, nickname, password_hash, is_admin, created_at`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var created int64
	err := row.Scan(&u.ID, &u.Email, &u.Nickname, &u.PasswordHash, &u.IsAdmin, &created)
	u.CreatedAt = time.Unix(created, 0)
	return u, err
}

// CreateUser inserts a user. The first account ever created becomes the
// administrator, so a fresh install needs no separate setup step.
func (s *Store) CreateUser(ctx context.Context, email, nickname, passwordHash string) (User, error) {
	var u User
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx,
			`INSERT INTO users (email, nickname, password_hash, is_admin, created_at) VALUES (?, ?, ?, ?, ?)`,
			email, nickname, passwordHash, count == 0, s.unix())
		if err != nil {
			if strings.Contains(err.Error(), "UNIQUE") {
				return ErrDuplicate
			}
			return err
		}
		id, _ := res.LastInsertId()
		u, err = scanUser(tx.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE id = ?`, id))
		return err
	})
	return u, err
}

// HasUsers reports whether any account exists yet.
func (s *Store) HasUsers(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n > 0, err
}

// UserByEmail looks a user up by email, case-insensitively.
func (s *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	u, err := scanUser(s.db.QueryRowContext(ctx, `SELECT `+userCols+` FROM users WHERE email = ?`, email))
	return u, notFound(err)
}

// UserByID looks a user up by ID.
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

// SessionUser returns the session and its user, if the session is still valid.
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

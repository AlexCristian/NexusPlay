package store

import (
	"context"
	"database/sql"
	"time"
)

// [CreateUser] adds an account. The first account ever created becomes the
// administrator, so a fresh install needs no separate setup step. The count
// and the insert happen in one transaction that takes the write lock first
// (see [dataSourceName]), so two people registering at once cannot both
// take the last place.
//
// Parameters:
//   - [ctx]: cancels the insert.
//   - [email]: the e-mail address.
//   - [nickname]: the display name.
//   - [passwordHash]: the password's [auth.HashPassword] hash.
//   - [maxUsers]: how many accounts may exist, or 0 for no limit.
//
// Returns:
//   - [User]: the new account, or a zero [User] if [error] is not [nil].
//   - [error]: [nil] on success; [ErrDuplicate] if the e-mail or nickname is
//     taken, regardless of letter case; [ErrUserLimit] if [maxUsers]
//     accounts already exist; otherwise the database error.
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
		user, err = scanUser(transaction.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
		return err
	})
	return user, err
}

// [UserCount] counts the accounts.
//
// Parameters:
//   - [ctx]: cancels the query.
//
// Returns:
//   - [int]: the number of accounts.
//   - [error]: [nil] on success, or the database error.
func (store *Store) UserCount(ctx context.Context) (int, error) {
	var count int
	err := store.database.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

// [UserByEmail] finds an account by e-mail address, regardless of letter
// case.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [email]: the e-mail address.
//
// Returns:
//   - [User]: the account.
//   - [error]: [nil] on success, [ErrNotFound] if no account has that
//     address, or another database error.
func (store *Store) UserByEmail(ctx context.Context, email string) (User, error) {
	user, err := scanUser(store.database.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE email = ?`, email))
	return user, notFound(err)
}

// [UserByID] finds an account by ID.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [id]: the account.
//
// Returns:
//   - [User]: the account.
//   - [error]: [nil] on success, [ErrNotFound] if it does not exist, or
//     another database error.
func (store *Store) UserByID(ctx context.Context, id int64) (User, error) {
	user, err := scanUser(store.database.QueryRowContext(ctx, `SELECT `+userColumns+` FROM users WHERE id = ?`, id))
	return user, notFound(err)
}

// [UpdatePasswordHash] replaces an account's stored password hash, such as
// to upgrade its hashing parameters.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [id]: the account.
//   - [hash]: the new [auth.HashPassword] hash.
//
// Returns:
//   - [error]: [nil] on success, including when the account does not exist,
//     or the database error.
func (store *Store) UpdatePasswordHash(ctx context.Context, id int64, hash string) error {
	_, err := store.database.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return err
}

// [CreateSession] saves a new sign-in session, keyed by its token's hash.
//
// Parameters:
//   - [ctx]: cancels the insert.
//   - [tokenHash]: the session token's [auth.HashToken] hash.
//   - [userID]: the signed-in account.
//   - [csrf]: the session's CSRF token.
//   - [expires]: when the session stops being valid, to the second.
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) CreateSession(ctx context.Context, tokenHash string, userID int64, csrf string, expires time.Time) error {
	_, err := store.database.ExecContext(ctx,
		`INSERT INTO sessions (token_hash, user_id, csrf_token, expires_at) VALUES (?, ?, ?, ?)`,
		tokenHash, userID, csrf, expires.Unix())
	return err
}

// [SessionUser] finds a session that has not expired, and its account.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [tokenHash]: the session token's [auth.HashToken] hash.
//
// Returns:
//   - [Session]: the session, or a zero [Session] if [error] is not [nil].
//   - [User]: the account, or a zero [User] if [error] is not [nil].
//   - [error]: [nil] on success, [ErrNotFound] if no live session has that
//     hash, or another database error.
func (store *Store) SessionUser(ctx context.Context, tokenHash string) (Session, User, error) {
	var session Session
	var expiresAt int64
	row := store.database.QueryRowContext(ctx, `
		SELECT s.user_id, s.csrf_token, s.expires_at,
		       u.id, u.email, u.nickname, u.password_hash, u.is_admin, u.created_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, store.unix())
	var user User
	var created int64
	err := row.Scan(&session.UserID, &session.CSRFToken, &expiresAt,
		&user.ID, &user.Email, &user.Nickname, &user.PasswordHash, &user.IsAdmin, &created)
	if err != nil {
		return session, user, notFound(err)
	}
	session.ExpiresAt = time.Unix(expiresAt, 0)
	user.CreatedAt = time.Unix(created, 0)
	return session, user, nil
}

// [DeleteSession] removes a session, signing it out. Removing one that does
// not exist is not an error.
//
// Parameters:
//   - [ctx]: cancels the delete.
//   - [tokenHash]: the session token's [auth.HashToken] hash.
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := store.database.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

// [DeleteExpiredSessions] removes every session that has expired.
//
// Parameters:
//   - [ctx]: cancels the delete.
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) DeleteExpiredSessions(ctx context.Context) error {
	_, err := store.database.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, store.unix())
	return err
}

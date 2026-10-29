package auth

import (
	"context"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [SessionStore] is the database as [Sessions] uses it; [store.Store]
// implements it.
type SessionStore interface {
	// [CreateSession] saves a new login session.
	//
	// Parameters:
	//   - [ctx]: cancels the insert.
	//   - [tokenHash]: the session token's [HashToken] hash; the token itself is
	//     never stored.
	//   - [userID]: the signed-in account.
	//   - [csrf]: the session's CSRF token, which forms must echo back.
	//   - [expires]: when the session stops being valid.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	CreateSession(ctx context.Context, tokenHash string, userID int64, csrf string, expires time.Time) error
	// [SessionUser] finds a session that has not expired, and its account.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [tokenHash]: the session token's [HashToken] hash.
	//
	// Returns:
	//   - [store.Session]: the session, including its CSRF token.
	//   - [store.User]: the account the session belongs to.
	//   - [error]: [nil] on success, [store.ErrNotFound] if no live session has
	//     that hash, or another database error.
	SessionUser(ctx context.Context, tokenHash string) (store.Session, store.User, error)
	// [DeleteSession] removes a session, signing it out. Removing one that does
	// not exist is not an error.
	//
	// Parameters:
	//   - [ctx]: cancels the delete.
	//   - [tokenHash]: the session token's [HashToken] hash.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	DeleteSession(ctx context.Context, tokenHash string) error
	// [DeleteExpiredSessions] removes every session that has expired.
	//
	// Parameters:
	//   - [ctx]: cancels the delete.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	DeleteExpiredSessions(ctx context.Context) error
}

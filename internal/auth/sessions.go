package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [ErrNoSession] is returned by [Sessions.Lookup] when a session token is
// empty, unknown or expired.
var ErrNoSession = errors.New("no such session")

// [Sessions] creates and checks login sessions. Tokens are stored only as
// SHA-256 hashes (see [HashToken]), so a leaked database does not leak live
// sessions, and each session has its own CSRF token. Create one with
// [NewSessions]. It is safe for concurrent use.
type Sessions struct {
	// [store] saves and finds sessions.
	store SessionStore
	// [lifetime] is how long a new session stays valid.
	lifetime time.Duration
	// [log] records failed clean-ups of expired sessions.
	log *slog.Logger
	// [now] tells the time, for expiry. Tests replace it.
	now func() time.Time
}

// [NewSessions] creates the session manager. Expired sessions are only
// deleted while [Sessions.Run] runs, though [Sessions.Lookup] never accepts
// one.
//
// Parameters:
//   - [database]: saves and finds sessions; normally the [store.Store].
//   - [lifetime]: how long each new session stays valid.
//   - [log]: where failed clean-ups are logged.
//
// Returns:
//   - [*Sessions]: the session manager.
func NewSessions(database SessionStore, lifetime time.Duration, log *slog.Logger) *Sessions {
	return &Sessions{store: database, lifetime: lifetime, log: log, now: time.Now}
}

// [Start] signs a user in: it creates a session with a new token and a new
// CSRF token, valid for the configured lifetime.
//
// Parameters:
//   - [ctx]: cancels the database insert.
//   - [userID]: the account to sign in.
//
// Returns:
//   - [string] (token): the session token to put in the cookie, or "" if
//     [error] is not [nil].
//   - [time.Time] (expires): when the session expires, for the cookie's
//     expiry, or the zero time if [error] is not [nil].
//   - [error]: [nil] on success, or the random-source or database error.
func (sessions *Sessions) Start(ctx context.Context, userID int64) (token string, expires time.Time, err error) {
	token, err = NewToken()
	if err != nil {
		return "", time.Time{}, err
	}
	csrf, err := NewToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires = sessions.now().Add(sessions.lifetime)
	if err := sessions.store.CreateSession(ctx, HashToken(token), userID, csrf, expires); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// [Lookup] finds who a session token belongs to.
//
// Parameters:
//   - [ctx]: cancels the database query.
//   - [token]: the token from the session cookie, or "" if there is none.
//
// Returns:
//   - [Viewer]: the signed-in viewer with their account and the session's
//     CSRF token, or the anonymous [Viewer] if [error] is not [nil].
//   - [error]: [nil] on success; [ErrNoSession] if [token] is "" or no live
//     session has it; otherwise the database error.
func (sessions *Sessions) Lookup(ctx context.Context, token string) (Viewer, error) {
	if token == "" {
		return Viewer{}, ErrNoSession
	}
	session, user, err := sessions.store.SessionUser(ctx, HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return Viewer{}, ErrNoSession
	}
	if err != nil {
		return Viewer{}, err
	}
	return Viewer{User: &user, CSRF: session.CSRFToken}, nil
}

// [End] signs a session out by deleting it. Ending a session that does not
// exist is not an error.
//
// Parameters:
//   - [ctx]: cancels the database delete.
//   - [token]: the token from the session cookie.
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (sessions *Sessions) End(ctx context.Context, token string) error {
	return sessions.store.DeleteSession(ctx, HashToken(token))
}

// [Run] deletes expired sessions every hour until [ctx] ends, logging
// failures. It blocks, so call it in its own goroutine. It does not clean up
// at start.
//
// Parameters:
//   - [ctx]: stops the clean-up loop when it is cancelled.
//
// Returns:
//   - [error]: always [nil], once [ctx] ends.
func (sessions *Sessions) Run(ctx context.Context) error {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := sessions.store.DeleteExpiredSessions(ctx); err != nil && ctx.Err() == nil {
				sessions.log.Warn("could not delete expired sessions", "err", err)
			}
		}
	}
}

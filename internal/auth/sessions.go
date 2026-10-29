package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// ErrNoSession means a session token is unknown or expired.
var ErrNoSession = errors.New("no such session")

// Sessions creates and checks login sessions. Tokens are stored only as
// SHA-256 hashes, so a leaked database does not leak live sessions; each
// session has its own CSRF token. It is safe for concurrent use.
type Sessions struct {
	store    SessionStore
	lifetime time.Duration
	log      *slog.Logger
	now      func() time.Time
}

// NewSessions returns sessions that last lifetime.
func NewSessions(database SessionStore, lifetime time.Duration, log *slog.Logger) *Sessions {
	return &Sessions{store: database, lifetime: lifetime, log: log, now: time.Now}
}

// Start creates a session for a user and returns the token to put in the
// cookie, and when it expires.
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

// Lookup returns the viewer a session token belongs to, or ErrNoSession.
func (sessions *Sessions) Lookup(ctx context.Context, token string) (Viewer, error) {
	if token == "" {
		return Viewer{}, ErrNoSession
	}
	sess, user, err := sessions.store.SessionUser(ctx, HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return Viewer{}, ErrNoSession
	}
	if err != nil {
		return Viewer{}, err
	}
	return Viewer{User: &user, CSRF: sess.CSRFToken}, nil
}

// End deletes a session (logging out).
func (sessions *Sessions) End(ctx context.Context, token string) error {
	return sessions.store.DeleteSession(ctx, HashToken(token))
}

// Run deletes expired sessions every hour until ctx ends.
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

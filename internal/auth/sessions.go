package auth

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// SessionStore is the database as Sessions uses it; *store.Store implements it.
type SessionStore interface {
	CreateSession(ctx context.Context, tokenHash string, userID int64, csrf string, expires time.Time) error
	SessionUser(ctx context.Context, tokenHash string) (store.Session, store.User, error)
	DeleteSession(ctx context.Context, tokenHash string) error
	DeleteExpiredSessions(ctx context.Context) error
}

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
func NewSessions(st SessionStore, lifetime time.Duration, log *slog.Logger) *Sessions {
	return &Sessions{store: st, lifetime: lifetime, log: log, now: time.Now}
}

// Start creates a session for a user and returns the token to put in the
// cookie, and when it expires.
func (s *Sessions) Start(ctx context.Context, userID int64) (token string, expires time.Time, err error) {
	token, err = NewToken()
	if err != nil {
		return "", time.Time{}, err
	}
	csrf, err := NewToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expires = s.now().Add(s.lifetime)
	if err := s.store.CreateSession(ctx, HashToken(token), userID, csrf, expires); err != nil {
		return "", time.Time{}, err
	}
	return token, expires, nil
}

// Lookup returns the viewer a session token belongs to, or ErrNoSession.
func (s *Sessions) Lookup(ctx context.Context, token string) (Viewer, error) {
	if token == "" {
		return Viewer{}, ErrNoSession
	}
	sess, u, err := s.store.SessionUser(ctx, HashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return Viewer{}, ErrNoSession
	}
	if err != nil {
		return Viewer{}, err
	}
	return Viewer{User: &u, CSRF: sess.CSRFToken}, nil
}

// End deletes a session (logging out).
func (s *Sessions) End(ctx context.Context, token string) error {
	return s.store.DeleteSession(ctx, HashToken(token))
}

// Run deletes expired sessions every hour until ctx ends.
func (s *Sessions) Run(ctx context.Context) error {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			if err := s.store.DeleteExpiredSessions(ctx); err != nil && ctx.Err() == nil {
				s.log.Warn("could not delete expired sessions", "err", err)
			}
		}
	}
}

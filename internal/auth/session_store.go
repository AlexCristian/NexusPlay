package auth

import (
	"context"
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

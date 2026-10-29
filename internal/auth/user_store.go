package auth

import (
	"context"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// UserStore is the database as Accounts uses it; *store.Store implements it.
type UserStore interface {
	CreateUser(ctx context.Context, email, nickname, passwordHash string, maxUsers int) (store.User, error)
	UserByEmail(ctx context.Context, email string) (store.User, error)
	UpdatePasswordHash(ctx context.Context, id int64, hash string) error
	UserCount(ctx context.Context) (int, error)
}

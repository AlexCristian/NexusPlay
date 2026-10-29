package store

import (
	"time"

	_ "modernc.org/sqlite"
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

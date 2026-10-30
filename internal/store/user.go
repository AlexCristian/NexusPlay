package store

import (
	"time"
)

// [User] is a registered account.
type User struct {
	// [ID] identifies the account; IDs start at 1.
	ID int64
	// [Email] is the address the account signs in with, unique regardless of
	// ASCII letter case.
	Email string
	// [Nickname] is the name shown to others, unique regardless of ASCII letter case.
	Nickname string
	// [PasswordHash] is the password's Argon2id hash, made by
	// [auth.HashPassword].
	PasswordHash string
	// [IsAdmin] is [true] for the administrator: the first account created.
	IsAdmin bool
	// [CreatedAt] is when the account was registered.
	CreatedAt time.Time
}

package store

import (
	"errors"
	"time"
)

// ErrDuplicate is returned when an email or nickname is already taken.
var ErrDuplicate = errors.New("already exists")

// ErrUserLimit is returned by CreateUser when the account limit is reached.
var ErrUserLimit = errors.New("account limit reached")

const userCols = `id, email, nickname, password_hash, is_admin, created_at`

func scanUser(row rowScanner) (User, error) {
	var user User
	var created int64
	err := row.Scan(&user.ID, &user.Email, &user.Nickname, &user.PasswordHash, &user.IsAdmin, &created)
	user.CreatedAt = time.Unix(created, 0)
	return user, err
}

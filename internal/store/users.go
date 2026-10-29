package store

import (
	"errors"
	"time"
)

// [ErrDuplicate] is returned when a write would repeat a value that must be
// unique: an e-mail, nickname or collection name, compared without regard
// to letter case.
var ErrDuplicate = errors.New("already exists")

// [ErrUserLimit] is returned by [Store.CreateUser] when the account limit is
// reached.
var ErrUserLimit = errors.New("account limit reached")

// [userColumns] are the columns every user query selects, in the order
// [scanUser] reads them.
const userColumns = `id, email, nickname, password_hash, is_admin, created_at`

// [scanUser] reads one row of [userColumns].
//
// Parameters:
//   - [row]: the row to read.
//
// Returns:
//   - [User]: the account.
//   - [error]: [nil] on success, or the scan error ([sql.ErrNoRows] for an
//     [*sql.Row] that matched nothing).
func scanUser(row rowScanner) (User, error) {
	var user User
	var created int64
	err := row.Scan(&user.ID, &user.Email, &user.Nickname, &user.PasswordHash, &user.IsAdmin, &created)
	user.CreatedAt = time.Unix(created, 0)
	return user, err
}

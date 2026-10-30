package store

import (
	"time"
)

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

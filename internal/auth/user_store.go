package auth

import (
	"context"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [UserStore] is the database as [Accounts] uses it; [store.Store]
// implements it.
type UserStore interface {
	// [CreateUser] adds an account. The first account becomes the
	// administrator. Counting the existing accounts and inserting the new one
	// happen in one transaction.
	//
	// Parameters:
	//   - [ctx]: cancels the insert.
	//   - [email]: the e-mail address, already validated.
	//   - [nickname]: the display name, already validated.
	//   - [passwordHash]: the password's [HashPassword] hash.
	//   - [maxUsers]: how many accounts may exist, or 0 for no limit.
	//
	// Returns:
	//   - [store.User]: the new account.
	//   - [error]: [nil] on success; [store.ErrDuplicate] if the e-mail or
	//     nickname is taken; [store.ErrUserLimit] if [maxUsers] accounts already
	//     exist; otherwise the database error.
	CreateUser(ctx context.Context, email, nickname, passwordHash string, maxUsers int) (store.User, error)
	// [UserByEmail] finds an account by its e-mail address, regardless of ASCII
	// letter case.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [email]: the e-mail address.
	//
	// Returns:
	//   - [store.User]: the account.
	//   - [error]: [nil] on success, [store.ErrNotFound] if no account has that
	//     e-mail, or another database error.
	UserByEmail(ctx context.Context, email string) (store.User, error)
	// [UpdatePasswordHash] replaces an account's stored password hash.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [id]: the account.
	//   - [hash]: the new [HashPassword] hash.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	UpdatePasswordHash(ctx context.Context, id int64, hash string) error
	// [UserCount] counts the accounts.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//
	// Returns:
	//   - [int]: the number of accounts.
	//   - [error]: [nil] on success, or the database error.
	UserCount(ctx context.Context) (int, error)
}

package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [ErrInvalidCredentials] is returned by [Accounts.Login] when the e-mail or
// the password is wrong. It never says which, so the login form does not
// reveal who has an account.
var ErrInvalidCredentials = errors.New("wrong e-mail or password")

// [ErrRegistrationClosed] is returned by [Accounts.Register] when the install
// already has as many accounts as NEXUSPLAY_MAX_ACCOUNTS allows.
var ErrRegistrationClosed = errors.New("registration is closed")

// [Accounts] holds the rules for creating accounts and logging in. Create one
// with [NewAccounts]. It is safe for concurrent use.
type Accounts struct {
	// [users] stores and finds accounts.
	users UserStore
	// [maxAccounts] is how many accounts may exist, or 0 for no limit.
	maxAccounts int
	// [log] records registrations and failed hash upgrades.
	log *slog.Logger
	// [verify] checks a password against a hash: [VerifyPassword], replaced in
	// tests.
	verify func(password, hash string) (bool, error)

	// [dummyHash] is checked against when an e-mail is unknown, so failed logins
	// take as long whether or not the e-mail is registered.
	dummyHash string
}

// [NewAccounts] creates the account rules. Hashing the timing-equalizer
// password makes this take about as long as one login check.
//
// Parameters:
//   - [users]: stores and finds accounts; normally the [store.Store].
//   - [maxAccounts]: how many accounts may exist, or 0 for no limit.
//   - [log]: where registrations and failed hash upgrades are logged.
//
// Returns:
//   - [*Accounts]: the account rules, or [nil] if [error] is not [nil].
//   - [error]: [nil] on success, or an error only if the system's random
//     source is broken.
func NewAccounts(users UserStore, maxAccounts int, log *slog.Logger) (*Accounts, error) {
	hash, err := HashPassword("nexusplay-timing-equalizer")
	if err != nil {
		return nil, fmt.Errorf("creating the timing-equalizer hash: %w", err)
	}
	return &Accounts{users: users, maxAccounts: maxAccounts, log: log, verify: VerifyPassword, dummyHash: hash}, nil
}

// [RegistrationStatus] tells the sign-up form whether registering is open and
// whether the next account would become the administrator.
//
// Parameters:
//   - [ctx]: cancels the database query.
//
// Returns:
//   - [RegistrationStatus]: the current status. When [error] is not [nil] it
//     reads as if there were no accounts and must not be used.
//   - [error]: [nil] on success, or the database error from counting
//     accounts.
func (accounts *Accounts) RegistrationStatus(ctx context.Context) (RegistrationStatus, error) {
	userCount, err := accounts.users.UserCount(ctx)
	return RegistrationStatus{
		FirstUser:   userCount == 0,
		Closed:      accounts.maxAccounts > 0 && userCount >= accounts.maxAccounts,
		MaxAccounts: accounts.maxAccounts,
	}, err
}

// [Register] validates a sign-up and creates the account. Spaces around the
// e-mail and nickname are trimmed first. The first account ever created
// becomes the administrator, and each registration is logged.
//
// Parameters:
//   - [ctx]: cancels the database work.
//   - [registration]: what the sign-up form submitted.
//
// Returns:
//   - [store.User]: the new account, or a zero [store.User] if [error] is not
//     [nil].
//   - [error]: [nil] on success; a [*ValidationError] naming the field for
//     bad input (see [validate]); [store.ErrDuplicate] when the e-mail or
//     nickname is taken; [ErrRegistrationClosed] when the account limit is
//     reached; otherwise the hashing or database error.
func (accounts *Accounts) Register(ctx context.Context, registration Registration) (store.User, error) {
	registration.Email = strings.TrimSpace(registration.Email)
	registration.Nickname = strings.TrimSpace(registration.Nickname)
	if err := validate(registration); err != nil {
		return store.User{}, err
	}
	hash, err := HashPassword(registration.Password)
	if err != nil {
		return store.User{}, err
	}
	user, err := accounts.users.CreateUser(ctx, registration.Email, registration.Nickname, hash, accounts.maxAccounts)
	if errors.Is(err, store.ErrUserLimit) {
		return store.User{}, ErrRegistrationClosed
	}
	if err != nil {
		return store.User{}, err
	}
	accounts.log.Info("user registered", "user", user.ID, "admin", user.IsAdmin)
	return user, nil
}

// [validate] checks a sign-up's fields: the e-mail must be a plain address
// (no display name) of at most 254 bytes, the nickname 2 to 40 characters,
// and the password at least 8 characters and at most 1024 bytes.
//
// Parameters:
//   - [registration]: the sign-up, with spaces around the e-mail and
//     nickname already trimmed.
//
// Returns:
//   - [error]: [nil] if every field is valid, or a [*ValidationError] for
//     the first field that is not, with a message to show the user.
func validate(registration Registration) error {
	if address, err := mail.ParseAddress(registration.Email); err != nil || address.Address != registration.Email || len(registration.Email) > 254 {
		return &ValidationError{"email", "Enter a valid e-mail address."}
	}
	if length := utf8.RuneCountInString(registration.Nickname); length < 2 || length > 40 {
		return &ValidationError{"nickname", "Your nickname must be between 2 and 40 characters."}
	}
	if utf8.RuneCountInString(registration.Password) < 8 || len(registration.Password) > 1024 {
		return &ValidationError{"password", "Your password must be at least 8 characters and at most 1024 bytes long."}
	}
	return nil
}

// [Login] checks an e-mail and password and returns the account they belong
// to. Spaces around the e-mail are trimmed. An unknown e-mail still costs one
// password check, so the response time does not reveal who has an account. A
// correct password stored with weaker hashing parameters is re-hashed on the
// way (see [Accounts.rehash]).
//
// Parameters:
//   - [ctx]: cancels the database work.
//   - [email]: the e-mail address typed in.
//   - [password]: the password typed in.
//
// Returns:
//   - [store.User]: the account, or a zero [store.User] if [error] is not
//     [nil].
//   - [error]: [nil] on success; [ErrInvalidCredentials] if the e-mail is
//     unknown or the password is wrong; otherwise a database error, or an
//     error naming the user if their stored hash cannot be read.
func (accounts *Accounts) Login(ctx context.Context, email, password string) (store.User, error) {
	user, err := accounts.users.UserByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, store.ErrNotFound) {
		accounts.verify(password, accounts.dummyHash) // spend the same time as a real check
		return store.User{}, ErrInvalidCredentials
	}
	if err != nil {
		return store.User{}, err
	}
	needsRehash, err := accounts.verify(password, user.PasswordHash)
	switch {
	case errors.Is(err, ErrMismatch):
		return store.User{}, ErrInvalidCredentials
	case err != nil:
		// A stored hash we cannot read is our problem, not a wrong password.
		return store.User{}, fmt.Errorf("checking the password of user %d: %w", user.ID, err)
	}
	if needsRehash {
		accounts.rehash(ctx, user.ID, password)
	}
	return user, nil
}

// [rehash] replaces a user's stored hash with one made with the current
// parameters. It is best effort: the password was right, so the login
// succeeds either way; a failure is logged and the next login tries again.
//
// Parameters:
//   - [ctx]: cancels the database update.
//   - [userID]: the account whose hash is upgraded.
//   - [password]: the password just verified for that account.
func (accounts *Accounts) rehash(ctx context.Context, userID int64, password string) {
	hash, err := HashPassword(password)
	if err == nil {
		err = accounts.users.UpdatePasswordHash(ctx, userID, hash)
	}
	if err != nil {
		accounts.log.Warn("could not upgrade a password hash", "user", userID, "err", err)
	}
}

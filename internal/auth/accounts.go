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

// ErrInvalidCredentials means the email or the password is wrong. It never
// says which, so the login form does not reveal who has an account.
var ErrInvalidCredentials = errors.New("wrong e-mail or password")

// ErrRegistrationClosed means the install has as many accounts as
// NEXUSPLAY_MAX_ACCOUNTS allows.
var ErrRegistrationClosed = errors.New("registration is closed")

// Accounts holds the rules for creating accounts and logging in. It is safe
// for concurrent use.
type Accounts struct {
	users       UserStore
	maxAccounts int // 0: no limit
	log         *slog.Logger
	verify      func(password, hash string) (bool, error) // VerifyPassword; replaced in tests

	// dummyHash is checked against when an email is unknown, so failed logins
	// take as long whether or not the email is registered.
	dummyHash string
}

// NewAccounts returns the account rules. At most maxAccounts accounts can
// exist (0 means no limit). It fails only if the system's random source is
// broken.
func NewAccounts(users UserStore, maxAccounts int, log *slog.Logger) (*Accounts, error) {
	hash, err := HashPassword("nexusplay-timing-equalizer")
	if err != nil {
		return nil, fmt.Errorf("creating the timing-equalizer hash: %w", err)
	}
	return &Accounts{users: users, maxAccounts: maxAccounts, log: log, verify: VerifyPassword, dummyHash: hash}, nil
}

// RegistrationStatus reports whether registering is possible and whether
// the next account would be the administrator.
func (accounts *Accounts) RegistrationStatus(ctx context.Context) (RegistrationStatus, error) {
	userCount, err := accounts.users.UserCount(ctx)
	return RegistrationStatus{
		FirstUser:   userCount == 0,
		Closed:      accounts.maxAccounts > 0 && userCount >= accounts.maxAccounts,
		MaxAccounts: accounts.maxAccounts,
	}, err
}

// Register validates a registration and creates the account. The first
// account becomes the administrator. It returns *ValidationError for bad
// input, store.ErrDuplicate when the email or nickname is taken, and
// ErrRegistrationClosed when the account limit is reached.
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

func validate(registration Registration) error {
	if address, err := mail.ParseAddress(registration.Email); err != nil || address.Address != registration.Email || len(registration.Email) > 254 {
		return &ValidationError{"email", "Enter a valid e-mail address."}
	}
	if length := utf8.RuneCountInString(registration.Nickname); length < 2 || length > 40 {
		return &ValidationError{"nickname", "Your nickname must be between 2 and 40 characters."}
	}
	if utf8.RuneCountInString(registration.Password) < 8 || len(registration.Password) > 1024 {
		return &ValidationError{"password", "Your password must be at least 8 characters long."}
	}
	return nil
}

// Login checks an email and password and returns the user, or
// ErrInvalidCredentials. A correct password stored with weaker hashing
// parameters is re-hashed on the way.
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

// rehash upgrades a stored hash. It is best effort: the password was right,
// so the login succeeds either way, and the next login tries again.
func (accounts *Accounts) rehash(ctx context.Context, userID int64, password string) {
	hash, err := HashPassword(password)
	if err == nil {
		err = accounts.users.UpdatePasswordHash(ctx, userID, hash)
	}
	if err != nil {
		accounts.log.Warn("could not upgrade a password hash", "user", userID, "err", err)
	}
}

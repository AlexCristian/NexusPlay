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

// Viewer is who is making a request. User is nil for anonymous visitors.
type Viewer struct {
	User *store.User
	CSRF string // the session's CSRF token; "" when anonymous
}

// ID returns the user's ID, or 0 for anonymous visitors (no user has ID 0).
func (v Viewer) ID() int64 {
	if v.User == nil {
		return 0
	}
	return v.User.ID
}

// IsAdmin reports whether the viewer administers this install.
func (v Viewer) IsAdmin() bool { return v.User != nil && v.User.IsAdmin }

// UserStore is the database as Accounts uses it; *store.Store implements it.
type UserStore interface {
	CreateUser(ctx context.Context, email, nickname, passwordHash string, maxUsers int) (store.User, error)
	UserByEmail(ctx context.Context, email string) (store.User, error)
	UpdatePasswordHash(ctx context.Context, id int64, hash string) error
	UserCount(ctx context.Context) (int, error)
}

// ErrInvalidCredentials means the email or the password is wrong. It never
// says which, so the login form does not reveal who has an account.
var ErrInvalidCredentials = errors.New("wrong e-mail or password")

// ErrRegistrationClosed means the install has as many accounts as
// NEXUSPLAY_MAX_ACCOUNTS allows.
var ErrRegistrationClosed = errors.New("registration is closed")

// ValidationError is input the user can fix; Msg is shown to them.
type ValidationError struct {
	Field string
	Msg   string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Msg }

// Registration is what the sign-up form submits.
type Registration struct {
	Email, Nickname, Password string
}

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
	h, err := HashPassword("nexusplay-timing-equalizer")
	if err != nil {
		return nil, fmt.Errorf("creating the timing-equalizer hash: %w", err)
	}
	return &Accounts{users: users, maxAccounts: maxAccounts, log: log, verify: VerifyPassword, dummyHash: h}, nil
}

// RegistrationStatus tells the sign-up form what to say.
type RegistrationStatus struct {
	FirstUser   bool // the next account becomes the administrator
	Closed      bool // the account limit is reached
	MaxAccounts int  // the limit; 0 for none
}

// RegistrationStatus reports whether registering is possible and whether
// the next account would be the administrator.
func (a *Accounts) RegistrationStatus(ctx context.Context) (RegistrationStatus, error) {
	n, err := a.users.UserCount(ctx)
	return RegistrationStatus{
		FirstUser:   n == 0,
		Closed:      a.maxAccounts > 0 && n >= a.maxAccounts,
		MaxAccounts: a.maxAccounts,
	}, err
}

// Register validates a registration and creates the account. The first
// account becomes the administrator. It returns *ValidationError for bad
// input, store.ErrDuplicate when the email or nickname is taken, and
// ErrRegistrationClosed when the account limit is reached.
func (a *Accounts) Register(ctx context.Context, in Registration) (store.User, error) {
	in.Email = strings.TrimSpace(in.Email)
	in.Nickname = strings.TrimSpace(in.Nickname)
	if err := validate(in); err != nil {
		return store.User{}, err
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return store.User{}, err
	}
	u, err := a.users.CreateUser(ctx, in.Email, in.Nickname, hash, a.maxAccounts)
	if errors.Is(err, store.ErrUserLimit) {
		return store.User{}, ErrRegistrationClosed
	}
	if err != nil {
		return store.User{}, err
	}
	a.log.Info("user registered", "user", u.ID, "admin", u.IsAdmin)
	return u, nil
}

func validate(in Registration) error {
	if addr, err := mail.ParseAddress(in.Email); err != nil || addr.Address != in.Email || len(in.Email) > 254 {
		return &ValidationError{"email", "Enter a valid e-mail address."}
	}
	if n := utf8.RuneCountInString(in.Nickname); n < 2 || n > 40 {
		return &ValidationError{"nickname", "Your nickname must be between 2 and 40 characters."}
	}
	if utf8.RuneCountInString(in.Password) < 8 || len(in.Password) > 1024 {
		return &ValidationError{"password", "Your password must be at least 8 characters long."}
	}
	return nil
}

// Login checks an email and password and returns the user, or
// ErrInvalidCredentials. A correct password stored with weaker hashing
// parameters is re-hashed on the way.
func (a *Accounts) Login(ctx context.Context, email, password string) (store.User, error) {
	u, err := a.users.UserByEmail(ctx, strings.TrimSpace(email))
	if errors.Is(err, store.ErrNotFound) {
		a.verify(password, a.dummyHash) // spend the same time as a real check
		return store.User{}, ErrInvalidCredentials
	}
	if err != nil {
		return store.User{}, err
	}
	needsRehash, err := a.verify(password, u.PasswordHash)
	switch {
	case errors.Is(err, ErrMismatch):
		return store.User{}, ErrInvalidCredentials
	case err != nil:
		// A stored hash we cannot read is our problem, not a wrong password.
		return store.User{}, fmt.Errorf("checking the password of user %d: %w", u.ID, err)
	}
	if needsRehash {
		a.rehash(ctx, u.ID, password)
	}
	return u, nil
}

// rehash upgrades a stored hash. It is best effort: the password was right,
// so the login succeeds either way, and the next login tries again.
func (a *Accounts) rehash(ctx context.Context, userID int64, password string) {
	h, err := HashPassword(password)
	if err == nil {
		err = a.users.UpdatePasswordHash(ctx, userID, h)
	}
	if err != nil {
		a.log.Warn("could not upgrade a password hash", "user", userID, "err", err)
	}
}

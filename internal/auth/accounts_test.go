package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

func newTestStore(t *testing.T) *store.Store {
	t.Helper()
	database, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func newAccounts(t *testing.T, store *store.Store) *Accounts {
	t.Helper()
	accounts, err := NewAccounts(store, 0, quiet)
	if err != nil {
		t.Fatal(err)
	}
	return accounts
}

func TestRegisterFirstUserIsAdmin(t *testing.T) {
	ctx := context.Background()
	accounts := newAccounts(t, newTestStore(t))
	if registrationStatus, _ := accounts.RegistrationStatus(ctx); !registrationStatus.FirstUser || registrationStatus.Closed {
		t.Errorf("status on an empty install = %+v", registrationStatus)
	}
	alex, err := accounts.Register(ctx, Registration{Email: " alex@example.com ", Nickname: "alex", Password: "long enough"})
	if err != nil || !alex.IsAdmin || alex.Email != "alex@example.com" {
		t.Fatalf("first account = %+v, %v; want an admin with a trimmed email", alex, err)
	}
	sam, err := accounts.Register(ctx, Registration{Email: "sam@example.com", Nickname: "sam", Password: "long enough"})
	if err != nil || sam.IsAdmin {
		t.Errorf("second account = %+v, %v; want a regular user", sam, err)
	}
	if _, err := accounts.Register(ctx, Registration{Email: "ALEX@example.com", Nickname: "other", Password: "long enough"}); !errors.Is(err, store.ErrDuplicate) {
		t.Errorf("duplicate email: %v", err)
	}
}

func TestRegisterValidation(t *testing.T) {
	accounts := newAccounts(t, newTestStore(t))
	for _, testCase := range []struct {
		input Registration
		field string
	}{
		{Registration{Email: "not an email", Nickname: "alex", Password: "long enough"}, "email"},
		{Registration{Email: "Alex <a@x.com>", Nickname: "alex", Password: "long enough"}, "email"},
		{Registration{Email: "a@x.com", Nickname: "a", Password: "long enough"}, "nickname"},
		{Registration{Email: "a@x.com", Nickname: strings.Repeat("n", 41), Password: "long enough"}, "nickname"},
		{Registration{Email: "a@x.com", Nickname: "alex", Password: "short"}, "password"},
		{Registration{Email: "a@x.com", Nickname: "alex", Password: strings.Repeat("p", 1025)}, "password"},
	} {
		_, err := accounts.Register(context.Background(), testCase.input)
		var invalid *ValidationError
		if !errors.As(err, &invalid) || invalid.Field != testCase.field {
			t.Errorf("Register(%+v) = %v, want a validation error on %s", testCase.input, err, testCase.field)
		}
	}
}

func TestLogin(t *testing.T) {
	ctx := context.Background()
	accounts := newAccounts(t, newTestStore(t))
	accounts.Register(ctx, Registration{Email: "alex@example.com", Nickname: "alex", Password: "long enough"})
	if user, err := accounts.Login(ctx, "ALEX@example.com ", "long enough"); err != nil || user.Nickname != "alex" {
		t.Errorf("Login = %+v, %v", user, err)
	}
	if _, err := accounts.Login(ctx, "alex@example.com", "wrong password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("wrong password: %v", err)
	}
	if _, err := accounts.Login(ctx, "nobody@example.com", "long enough"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("unknown email: %v", err)
	}
}

// An unknown email still runs a password check, so it takes as long as a
// real login and does not reveal which emails are registered.
func TestLoginUnknownEmailStillHashes(t *testing.T) {
	accounts := newAccounts(t, newTestStore(t))
	var checked []string
	accounts.verify = func(password, hash string) (bool, error) {
		checked = append(checked, hash)
		return false, ErrMismatch
	}
	accounts.Login(context.Background(), "nobody@example.com", "whatever")
	if len(checked) != 1 || checked[0] != accounts.dummyHash {
		t.Errorf("checked hashes = %v, want the dummy hash once", checked)
	}
}

// A correct password stored with weaker hashing parameters is upgraded.
func TestLoginRehashesWeakHash(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	accounts := newAccounts(t, store)
	salt := []byte("saltsaltsaltsalt")
	weak := fmt.Sprintf("$argon2id$v=19$m=4096,t=1,p=1$%s$%s",
		b64(salt), b64(argon2.IDKey([]byte("old password"), salt, 1, 4096, 1, 32)))
	user, err := store.CreateUser(ctx, "old@example.com", "old", weak, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.Login(ctx, "old@example.com", "old password"); err != nil {
		t.Fatal(err)
	}
	after, _ := store.UserByID(ctx, user.ID)
	if after.PasswordHash == weak || !strings.Contains(after.PasswordHash, "m=19456,t=2") {
		t.Errorf("hash after login = %q, want it upgraded", after.PasswordHash)
	}
	if _, err := accounts.Login(ctx, "old@example.com", "old password"); err != nil {
		t.Errorf("login with the upgraded hash: %v", err)
	}
}

// A stored hash that cannot be read is an error, not "wrong password".
func TestLoginCorruptHashIsAnError(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	store.CreateUser(ctx, "x@example.com", "x", "not-a-hash", 0)
	_, err := newAccounts(t, store).Login(ctx, "x@example.com", "anything")
	if err == nil || errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("err = %v, want a real error", err)
	}
}

func TestSessions(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	user, _ := store.CreateUser(ctx, "a@example.com", "a", "hash", 0)
	sessions := NewSessions(store, time.Hour, quiet)

	token, expires, err := sessions.Start(ctx, user.ID)
	if err != nil || token == "" || expires.Before(time.Now().Add(59*time.Minute)) {
		t.Fatalf("Start = %q, %v, %v", token, expires, err)
	}
	value, err := sessions.Lookup(ctx, token)
	if err != nil || value.ID() != user.ID || value.CSRF == "" || !value.IsAdmin() {
		t.Errorf("Lookup = %+v, %v", value, err)
	}
	for _, bad := range []string{"", "made-up-token"} {
		if _, err := sessions.Lookup(ctx, bad); !errors.Is(err, ErrNoSession) {
			t.Errorf("Lookup(%q) = %v, want ErrNoSession", bad, err)
		}
	}
	if err := sessions.End(ctx, token); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Lookup(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Errorf("after End: %v", err)
	}
	if (Viewer{}).ID() != 0 || (Viewer{}).IsAdmin() {
		t.Error("an anonymous viewer has an ID or is an admin")
	}
}

// Sessions stop working when they expire.
func TestSessionsExpire(t *testing.T) {
	ctx := context.Background()
	store := newTestStore(t)
	user, _ := store.CreateUser(ctx, "a@example.com", "a", "hash", 0)
	sessions := NewSessions(store, time.Hour, quiet)
	sessions.now = func() time.Time { return time.Now().Add(-2 * time.Hour) } // started two hours ago
	token, _, _ := sessions.Start(ctx, user.ID)
	if _, err := sessions.Lookup(ctx, token); !errors.Is(err, ErrNoSession) {
		t.Errorf("expired session: %v, want ErrNoSession", err)
	}
}

func b64(data []byte) string { return base64.RawStdEncoding.EncodeToString(data) }

// With a limit of two, the third registration is refused and the status
// says registration is closed.
func TestRegisterAccountLimit(t *testing.T) {
	ctx := context.Background()
	accounts, err := NewAccounts(newTestStore(t), 2, quiet)
	if err != nil {
		t.Fatal(err)
	}
	for _, nick := range []string{"ann", "bob"} {
		if _, err := accounts.Register(ctx, Registration{Email: nick + "@example.com", Nickname: nick, Password: "long enough"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := accounts.Register(ctx, Registration{Email: "cat@example.com", Nickname: "cat", Password: "long enough"}); !errors.Is(err, ErrRegistrationClosed) {
		t.Errorf("third account: %v, want ErrRegistrationClosed", err)
	}
	if registrationStatus, _ := accounts.RegistrationStatus(ctx); !registrationStatus.Closed || registrationStatus.MaxAccounts != 2 {
		t.Errorf("status = %+v, want closed at 2", registrationStatus)
	}
}

package auth

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"testing"

	"golang.org/x/crypto/argon2"
)

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("unexpected hash format %q", h)
	}
	rehash, err := VerifyPassword("correct horse battery staple", h)
	if err != nil || rehash {
		t.Errorf("verify: rehash=%v err=%v", rehash, err)
	}
	if _, err := VerifyPassword("wrong", h); !errors.Is(err, ErrMismatch) {
		t.Errorf("wrong password: got %v, want ErrMismatch", err)
	}
}

func TestHashesAreSalted(t *testing.T) {
	a, _ := HashPassword("same")
	b, _ := HashPassword("same")
	if a == b {
		t.Error("two hashes of the same password are identical")
	}
}

func TestWeakParametersNeedRehash(t *testing.T) {
	salt := []byte("saltsaltsaltsalt")
	key := argon2.IDKey([]byte("pw"), salt, 1, 4096, 1, 32)
	b64 := base64.RawStdEncoding
	weak := fmt.Sprintf("$argon2id$v=19$m=4096,t=1,p=1$%s$%s", b64.EncodeToString(salt), b64.EncodeToString(key))
	rehash, err := VerifyPassword("pw", weak)
	if err != nil {
		t.Fatal(err)
	}
	if !rehash {
		t.Error("a hash with weaker parameters should be flagged for rehashing")
	}
}

func TestRejectsOtherFormats(t *testing.T) {
	// The old Java app stored unsalted SHA-1 hex digests; they must never verify.
	if _, err := VerifyPassword("password", "5baa61e4c9b93f3f0682250b6cf8331b7ee68fd8"); err == nil {
		t.Error("SHA-1 digest accepted")
	}
}

func TestTokens(t *testing.T) {
	a, _ := NewToken()
	b, _ := NewToken()
	if a == b || len(a) < 40 {
		t.Errorf("weak tokens %q %q", a, b)
	}
	// HashToken is SHA-256 in hex; compare against the standard test vector.
	if got, want := HashToken("abc"), "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad"; got != want {
		t.Errorf("HashToken(%q) = %s, want %s", "abc", got, want)
	}
	if HashToken(a) == HashToken(b) {
		t.Error("different tokens hashed to the same value")
	}
}

package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters, following the OWASP recommendation of 19 MiB memory,
// 2 iterations and 1 degree of parallelism.
const (
	// [argonTime] is the number of Argon2id passes over memory, following
	// OWASP's recommendation.
	argonTime = 2
	// [argonMemory] is the memory Argon2id uses per hash, in KiB: 19 MiB,
	// following OWASP's recommendation.
	argonMemory = 19 * 1024
	// [argonThreads] is Argon2id's degree of parallelism.
	argonThreads = 1
	// [argonKeyLength] is the length of the derived key, in bytes.
	argonKeyLength = 32
	// [saltLength] is the length of each hash's random salt, in bytes.
	saltLength = 16
)

// [tokenLength] is how many random bytes a token from [NewToken] holds: 256
// bits.
const tokenLength = 32

// [maximumArgonMemory] is the most memory, in KiB (1 GiB), a stored hash may
// ask for. A larger value can only come from a damaged hash, and would make
// checking it allocate that much.
const maximumArgonMemory = 1 << 20

// [minimumKeyLength] is the shortest derived key, in bytes, a stored hash may
// hold. A shorter one can only come from a damaged hash; an empty one would
// crash the Argon2 library.
const minimumKeyLength = 16

// [HashPassword] hashes a password for storage with Argon2id and a new random
// salt, so hashing the same password twice gives different results.
//
// Parameters:
//   - [password]: the password to hash.
//
// Returns:
//   - [string]: the hash in the PHC string format, such as
//     "$argon2id$v=19$m=19456,t=2,p=1$<salt>$<key>", which records its own
//     parameters for [VerifyPassword]; or "" if [error] is not [nil].
//   - [error]: [nil] on success, or an error only if the system's random
//     source is broken.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLength)
	encoding := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, encoding.EncodeToString(salt), encoding.EncodeToString(key)), nil
}

// [VerifyPassword] checks a password against a hash made by [HashPassword],
// using the parameters recorded in the hash. The comparison takes the same
// time however many bytes match.
//
// Parameters:
//   - [password]: the password to check.
//   - [encoded]: the stored hash in the PHC string format.
//
// Returns:
//   - [bool] ([needsRehash]): [true] if the password matched but the hash was
//     made with less memory or fewer passes than the current parameters, so
//     it should be replaced; [false] otherwise.
//   - [error]: [nil] if the password matches; [ErrMismatch] if it does not;
//     another error if [encoded] is not an Argon2id hash of the current
//     version, its parts are not valid base64, or its parameters are out of
//     range (fewer than 1 pass or 1 thread, more than [maximumArgonMemory],
//     or a key shorter than [minimumKeyLength]).
func VerifyPassword(password, encoded string) (needsRehash bool, err error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("unsupported hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported argon2 version")
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return false, errors.New("bad argon2 parameters")
	}
	if iterations < 1 || parallelism < 1 || memory > maximumArgonMemory {
		return false, errors.New("argon2 parameters out of range")
	}
	encoding := base64.RawStdEncoding
	salt, err := encoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := encoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	if len(want) < minimumKeyLength {
		return false, errors.New("argon2 key too short")
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallelism, uint32(len(want)))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return false, ErrMismatch
	}
	return memory < argonMemory || iterations < argonTime, nil
}

// [NewToken] makes a random token for a session cookie or a CSRF form field.
//
// Returns:
//   - [string]: 32 random bytes (256 bits) in URL-safe base64 without
//     padding, 43 characters long; or "" if [error] is not [nil].
//   - [error]: [nil] on success, or an error only if the system's random
//     source is broken.
func NewToken() (string, error) {
	randomBytes := make([]byte, tokenLength)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

// [HashToken] turns a session token into the form the database stores: its
// SHA-256 hash. Only the browser holds the token itself, so a leaked
// database does not leak live sessions.
//
// Parameters:
//   - [token]: a token from [NewToken].
//
// Returns:
//   - [string]: the token's SHA-256 hash in lowercase hexadecimal, 64
//     characters long.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

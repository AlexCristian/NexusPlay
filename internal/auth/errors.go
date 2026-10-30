package auth

import "errors"

// [ErrInvalidCredentials] is returned by [Accounts.Login] when the e-mail or
// the password is wrong. It never says which, so the login form does not
// reveal who has an account.
var ErrInvalidCredentials = errors.New("wrong e-mail or password")

// [ErrRegistrationClosed] is returned by [Accounts.Register] when the install
// already has as many accounts as NEXUSPLAY_MAX_ACCOUNTS allows.
var ErrRegistrationClosed = errors.New("registration is closed")

// [ErrMismatch] is returned by [VerifyPassword] when a password does not
// match its hash.
var ErrMismatch = errors.New("password does not match")

// [ErrNoSession] is returned by [Sessions.Lookup] when a session token is
// empty, unknown or expired.
var ErrNoSession = errors.New("no such session")

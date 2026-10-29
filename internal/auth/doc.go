// Package auth owns identity: password hashing, session tokens, the rules
// for registering and logging in, and the Viewer every request carries. It
// knows nothing about HTTP; the web package turns its results into cookies
// and pages.
package auth

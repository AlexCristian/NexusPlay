package store

import (
	"time"
)

// [Session] is a signed-in browser session, as [Store.SessionUser] returns
// it.
type Session struct {
	// [UserID] is the signed-in account.
	UserID int64
	// [CSRFToken] is the session's CSRF token, which every form must send back.
	CSRFToken string
	// [ExpiresAt] is when the session stops being valid.
	ExpiresAt time.Time
}

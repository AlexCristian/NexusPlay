package store

import (
	"time"

	_ "modernc.org/sqlite"
)

// Session is a logged-in browser session.
type Session struct {
	UserID    int64
	CSRFToken string
	ExpiresAt time.Time
}

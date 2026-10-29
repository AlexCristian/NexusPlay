package auth

import (
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// Viewer is who is making a request. User is nil for anonymous visitors.
type Viewer struct {
	User *store.User
	CSRF string // the session's CSRF token; "" when anonymous
}

// ID returns the user's ID, or 0 for anonymous visitors (no user has ID 0).
func (viewer Viewer) ID() int64 {
	if viewer.User == nil {
		return 0
	}
	return viewer.User.ID
}

// IsAdmin reports whether the viewer administers this install.
func (viewer Viewer) IsAdmin() bool { return viewer.User != nil && viewer.User.IsAdmin }

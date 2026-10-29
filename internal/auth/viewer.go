package auth

import (
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [Viewer] is who is making a request: a signed-in account, or an anonymous
// visitor (the zero [Viewer]).
type Viewer struct {
	// [User] is the signed-in account, or [nil] for an anonymous visitor.
	User *store.User
	// [CSRF] is the session's CSRF token, which every form must send back, or ""
	// for an anonymous visitor.
	CSRF string
}

// [ID] gets the viewer's account ID.
//
// Returns:
//   - [int64]: the account's ID, or 0 for an anonymous visitor (no account
//     has ID 0).
func (viewer Viewer) ID() int64 {
	if viewer.User == nil {
		return 0
	}
	return viewer.User.ID
}

// [IsAdmin] reports whether the viewer administers this install.
//
// Returns:
//   - [bool]: [true] for a signed-in administrator, [false] for anyone else,
//     including anonymous visitors.
func (viewer Viewer) IsAdmin() bool { return viewer.User != nil && viewer.User.IsAdmin }

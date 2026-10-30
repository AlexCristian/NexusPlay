package store

import "errors"

// [ErrNotFound] is returned when a lookup of one row matches none.
var ErrNotFound = errors.New("not found")

// Errors returned by the subtitle voting flow.
var (
	// [ErrAlreadyVoted] is returned when a user proposes or votes on a line they
	// already voted on: each user gets one vote per line.
	ErrAlreadyVoted = errors.New("you have already voted on this line")
	// [ErrUnchanged] is returned when a proposed correction is the line's
	// current text.
	ErrUnchanged = errors.New("the proposed text is the same as the current line")
)

// [ErrDuplicate] is returned when a write would repeat a value that must be
// unique: an e-mail, nickname or collection name, compared without regard
// to ASCII letter case.
var ErrDuplicate = errors.New("already exists")

// [ErrUserLimit] is returned by [Store.CreateUser] when the account limit is
// reached.
var ErrUserLimit = errors.New("account limit reached")

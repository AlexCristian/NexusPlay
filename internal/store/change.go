package store

import (
	_ "modernc.org/sqlite"
)

// Change is a proposed correction to a cue, with its vote count.
type Change struct {
	ID           int64
	CueID        int64
	OriginalText string
	ProposedText string
	Votes        int
	VotedByMe    bool
}

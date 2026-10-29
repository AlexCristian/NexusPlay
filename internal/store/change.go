package store

// [Change] is a proposed correction to a subtitle line, with its votes.
type Change struct {
	// [ID] identifies the proposal, for voting.
	ID int64
	// [CueID] is the line the proposal corrects.
	CueID int64
	// [OriginalText] is the line's current text.
	OriginalText string
	// [ProposedText] is the correction.
	ProposedText string
	// [Votes] counts the votes for the proposal, the proposer's included.
	Votes int
	// [VotedByMe] is [true] when the user who asked has voted for this
	// proposal.
	VotedByMe bool
}

package store

import (
	"context"
	"database/sql"
	"errors"
)

// Errors returned by the subtitle voting flow.
var (
	// [ErrAlreadyVoted] is returned when a user proposes or votes on a line they
	// already voted on: each user gets one vote per line.
	ErrAlreadyVoted = errors.New("you have already voted on this line")
	// [ErrUnchanged] is returned when a proposed correction is the line's
	// current text.
	ErrUnchanged = errors.New("the proposed text is the same as the current line")
)

// [checkNotVoted] makes sure a user has not voted on any proposal for a
// line.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [transaction]: the voting transaction.
//   - [cueID]: the line.
//   - [userID]: the voter.
//
// Returns:
//   - [error]: [nil] if the user has not voted on the line,
//     [ErrAlreadyVoted] if they have, or the database error.
func checkNotVoted(ctx context.Context, transaction *sql.Tx, cueID, userID int64) error {
	var voted bool
	err := transaction.QueryRowContext(ctx, `
		SELECT EXISTS (SELECT 1 FROM subtitle_votes v JOIN subtitle_changes ch ON ch.id = v.change_id
		               WHERE ch.cue_id = ? AND v.user_id = ?)`, cueID, userID).Scan(&voted)
	if err != nil {
		return err
	}
	if voted {
		return ErrAlreadyVoted
	}
	return nil
}

// [voteInTransaction] adds a user's vote to a proposal and, once the
// proposal has [votesRequired] votes, replaces the line's text with it and
// deletes every proposal for the line, its votes included. The caller must
// have checked [checkNotVoted].
//
// Parameters:
//   - [ctx]: cancels the queries.
//   - [transaction]: the voting transaction.
//   - [changeID]: the proposal voted for.
//   - [cueID]: the proposal's line.
//   - [userID]: the voter.
//   - [votesRequired]: how many votes apply a proposal.
//
// Returns:
//   - [bool]: [true] if this vote applied the proposal, [false] otherwise.
//   - [error]: [nil] on success, or the database error.
func voteInTransaction(ctx context.Context, transaction *sql.Tx, changeID, cueID, userID int64, votesRequired int) (bool, error) {
	if _, err := transaction.ExecContext(ctx, `INSERT INTO subtitle_votes (change_id, user_id) VALUES (?, ?)`, changeID, userID); err != nil {
		return false, err
	}
	var votes int
	if err := transaction.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitle_votes WHERE change_id = ?`, changeID).Scan(&votes); err != nil {
		return false, err
	}
	if votes < votesRequired {
		return false, nil
	}
	if _, err := transaction.ExecContext(ctx, `
		UPDATE subtitle_cues SET text = (SELECT proposed_text FROM subtitle_changes WHERE id = ?) WHERE id = ?`,
		changeID, cueID); err != nil {
		return false, err
	}
	_, err := transaction.ExecContext(ctx, `DELETE FROM subtitle_changes WHERE cue_id = ?`, cueID)
	return err == nil, err
}

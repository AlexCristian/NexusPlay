package store

import (
	"context"
	"database/sql"
	"errors"
)

// Errors returned by the subtitle voting flow.
var (
	ErrAlreadyVoted = errors.New("you have already voted on this line")
	ErrUnchanged    = errors.New("the proposed text is the same as the current line")
)

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

func voteTx(ctx context.Context, transaction *sql.Tx, changeID, cueID, userID int64, votesRequired int) (bool, error) {
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

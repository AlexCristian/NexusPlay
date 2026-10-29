package store

import (
	"context"
	"database/sql"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/collections"
)

// [AddSubtitle] stores a subtitle track and its lines in one transaction. A
// track with a [NewSubtitle.Source] is stored only once per media item:
// adding it again changes nothing.
//
// Parameters:
//   - [ctx]: cancels the inserts.
//   - [subtitle]: the track and its lines.
//
// Returns:
//   - [int64] ([id]): the new track's ID, or 0 if it was not added.
//   - [bool] ([inserted]): [true] if the track was added, [false] if a track
//     from the same source already existed.
//   - [error] ([err]): [nil] on success, or the database error, such as for
//     a media item that does not exist.
func (store *Store) AddSubtitle(ctx context.Context, subtitle NewSubtitle) (id int64, inserted bool, err error) {
	err = store.withTransaction(ctx, func(transaction *sql.Tx) error {
		result, err := transaction.ExecContext(ctx,
			`INSERT INTO subtitles (media_id, language, label, source, created_at) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT DO NOTHING`,
			subtitle.MediaID, subtitle.Language, subtitle.Label, subtitle.Source, store.unix())
		if err != nil {
			return err
		}
		rowsAffected, err := result.RowsAffected()
		if err != nil || rowsAffected == 0 {
			return err // already imported from this source
		}
		if id, err = result.LastInsertId(); err != nil {
			return err
		}
		inserted = true
		statement, err := transaction.PrepareContext(ctx,
			`INSERT INTO subtitle_cues (subtitle_id, idx, start_ms, end_ms, text) VALUES (?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer statement.Close()
		for i, cue := range subtitle.Cues {
			if _, err := statement.ExecContext(ctx, id, i, cue.StartMS, cue.EndMS, cue.Text); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, false, err
	}
	return id, inserted, nil
}

// [SubtitleSources] lists the sidecar files whose tracks have been imported,
// so the scanner does not import them again.
//
// Parameters:
//   - [ctx]: cancels the query.
//
// Returns:
//   - [collections.Set[string]]: the sidecar paths, relative to the media
//     folder.
//   - [error]: [nil] on success, or the database error.
func (store *Store) SubtitleSources(ctx context.Context) (collections.Set[string], error) {
	sources, err := queryAll(ctx, store.database, scanString, `SELECT source FROM subtitles WHERE source <> ''`)
	if err != nil {
		return collections.Set[string]{}, err
	}
	return collections.NewSet(sources...), nil
}

// [UpdateSubtitleLabel] changes a track's language and the name shown in the
// player.
//
// Parameters:
//   - [ctx]: cancels the update.
//   - [id]: the track.
//   - [language]: the new language code, such as "en".
//   - [label]: the new name, such as "English (SDH)".
//
// Returns:
//   - [error]: [nil] on success, including when the track does not exist, or
//     the database error.
func (store *Store) UpdateSubtitleLabel(ctx context.Context, id int64, language, label string) error {
	_, err := store.database.ExecContext(ctx, `UPDATE subtitles SET language = ?, label = ? WHERE id = ?`, language, label, id)
	return err
}

// [DeleteSubtitle] removes a track with its lines, proposals and votes.
// Deleting one that does not exist is not an error.
//
// Parameters:
//   - [ctx]: cancels the delete.
//   - [id]: the track.
//
// Returns:
//   - [error]: [nil] on success, or the database error.
func (store *Store) DeleteSubtitle(ctx context.Context, id int64) error {
	_, err := store.database.ExecContext(ctx, `DELETE FROM subtitles WHERE id = ?`, id)
	return err
}

// [Subtitles] lists a media item's tracks, by label.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [mediaID]: the media item.
//
// Returns:
//   - [[]Subtitle]: the tracks, or [nil] if there are none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) Subtitles(ctx context.Context, mediaID int64) ([]Subtitle, error) {
	return queryAll(ctx, store.database, func(row rowScanner) (Subtitle, error) {
		var subtitle Subtitle
		err := row.Scan(&subtitle.ID, &subtitle.MediaID, &subtitle.Language, &subtitle.Label, &subtitle.Source)
		return subtitle, err
	}, `SELECT id, media_id, language, label, source FROM subtitles WHERE media_id = ? ORDER BY label`, mediaID)
}

// [SubtitleByID] loads one track.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [id]: the track.
//
// Returns:
//   - [Subtitle]: the track.
//   - [error]: [nil] on success, [ErrNotFound] if it does not exist, or
//     another database error.
func (store *Store) SubtitleByID(ctx context.Context, id int64) (Subtitle, error) {
	var subtitle Subtitle
	err := store.database.QueryRowContext(ctx,
		`SELECT id, media_id, language, label, source FROM subtitles WHERE id = ?`, id).
		Scan(&subtitle.ID, &subtitle.MediaID, &subtitle.Language, &subtitle.Label, &subtitle.Source)
	return subtitle, notFound(err)
}

// [Cues] lists a track's lines in order.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [subtitleID]: the track.
//
// Returns:
//   - [[]Cue]: the lines with their current text, or [nil] if there are
//     none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) Cues(ctx context.Context, subtitleID int64) ([]Cue, error) {
	return queryAll(ctx, store.database, func(row rowScanner) (Cue, error) {
		var cue Cue
		err := row.Scan(&cue.ID, &cue.StartMS, &cue.EndMS, &cue.Text)
		return cue, err
	}, `SELECT id, start_ms, end_ms, text FROM subtitle_cues WHERE subtitle_id = ? ORDER BY idx`, subtitleID)
}

// [CueAt] finds the line showing at a playback time, or else the last one
// that started before it.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [subtitleID]: the track.
//   - [milliseconds]: the playback time, in milliseconds from the start.
//
// Returns:
//   - [Cue]: the line that started most recently at or before
//     [milliseconds].
//   - [error]: [nil] on success, [ErrNotFound] if no line starts at or
//     before [milliseconds], or another database error.
func (store *Store) CueAt(ctx context.Context, subtitleID, milliseconds int64) (Cue, error) {
	var cue Cue
	err := store.database.QueryRowContext(ctx, `
		SELECT id, start_ms, end_ms, text FROM subtitle_cues
		WHERE subtitle_id = ? AND start_ms <= ? ORDER BY start_ms DESC, idx DESC LIMIT 1`, subtitleID, milliseconds).
		Scan(&cue.ID, &cue.StartMS, &cue.EndMS, &cue.Text)
	return cue, notFound(err)
}

// [CueByID] loads one line and tells which track it belongs to.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [id]: the line.
//
// Returns:
//   - [Cue]: the line.
//   - [int64]: the ID of its track.
//   - [error]: [nil] on success, [ErrNotFound] if it does not exist, or
//     another database error.
func (store *Store) CueByID(ctx context.Context, id int64) (Cue, int64, error) {
	var cue Cue
	var subtitleID int64
	err := store.database.QueryRowContext(ctx,
		`SELECT id, start_ms, end_ms, text, subtitle_id FROM subtitle_cues WHERE id = ?`, id).
		Scan(&cue.ID, &cue.StartMS, &cue.EndMS, &cue.Text, &subtitleID)
	return cue, subtitleID, notFound(err)
}

// [CueForChange] finds the line a proposal corrects.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [changeID]: the proposal.
//
// Returns:
//   - [int64]: the line's ID.
//   - [error]: [nil] on success; [ErrNotFound] when the proposal no longer
//     exists, since applying a correction deletes every proposal for its
//     line; or another database error.
func (store *Store) CueForChange(ctx context.Context, changeID int64) (int64, error) {
	var cueID int64
	err := store.database.QueryRowContext(ctx, `SELECT cue_id FROM subtitle_changes WHERE id = ?`, changeID).Scan(&cueID)
	return cueID, notFound(err)
}

// [Changes] lists the open proposals for a line, most votes first.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [cueID]: the line.
//   - [userID]: the viewer, for [Change.VotedByMe], or 0 for anonymous.
//
// Returns:
//   - [[]Change]: the proposals, or [nil] if there are none.
//   - [error]: [nil] on success, or the database error.
func (store *Store) Changes(ctx context.Context, cueID, userID int64) ([]Change, error) {
	return queryAll(ctx, store.database, func(row rowScanner) (Change, error) {
		var change Change
		err := row.Scan(&change.ID, &change.CueID, &change.OriginalText, &change.ProposedText, &change.Votes, &change.VotedByMe)
		return change, err
	}, `
		SELECT ch.id, ch.cue_id, cu.text, ch.proposed_text,
		       (SELECT COUNT(*) FROM subtitle_votes v WHERE v.change_id = ch.id),
		       EXISTS (SELECT 1 FROM subtitle_votes v WHERE v.change_id = ch.id AND v.user_id = ?)
		FROM subtitle_changes ch JOIN subtitle_cues cu ON cu.id = ch.cue_id
		WHERE ch.cue_id = ? ORDER BY 5 DESC, ch.id`, userID, cueID)
}

// [ProposeChange] records a user's correction to a line, with their vote. If
// someone already proposed the same text, the user's vote goes to that
// proposal instead. Once a proposal has enough votes it replaces the line.
//
// Parameters:
//   - [ctx]: cancels the transaction.
//   - [cueID]: the line.
//   - [userID]: the proposer.
//   - [text]: the corrected text; spaces around it are trimmed.
//   - [votesRequired]: how many votes apply a proposal.
//
// Returns:
//   - [bool]: [true] if the correction was applied straight away, such as
//     when one vote is enough; [false] otherwise.
//   - [error]: [nil] on success; [ErrNotFound] if the line does not exist;
//     [ErrUnchanged] if [text] is the line's current text;
//     [ErrAlreadyVoted] if the user already voted on this line; otherwise
//     the database error.
func (store *Store) ProposeChange(ctx context.Context, cueID, userID int64, text string, votesRequired int) (bool, error) {
	text = strings.TrimSpace(text)
	var applied bool
	err := store.withTransaction(ctx, func(transaction *sql.Tx) error {
		var current string
		if err := transaction.QueryRowContext(ctx, `SELECT text FROM subtitle_cues WHERE id = ?`, cueID).Scan(&current); err != nil {
			return notFound(err)
		}
		if text == current {
			return ErrUnchanged
		}
		if err := checkNotVoted(ctx, transaction, cueID, userID); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `
			INSERT INTO subtitle_changes (cue_id, proposed_text, created_by, created_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(cue_id, proposed_text) DO NOTHING`, cueID, text, userID, store.unix()); err != nil {
			return err
		}
		var changeID int64
		if err := transaction.QueryRowContext(ctx,
			`SELECT id FROM subtitle_changes WHERE cue_id = ? AND proposed_text = ?`, cueID, text).Scan(&changeID); err != nil {
			return err
		}
		var err error
		applied, err = voteInTransaction(ctx, transaction, changeID, cueID, userID, votesRequired)
		return err
	})
	return applied, err
}

// [VoteChange] adds a user's vote to a proposal. Each user gets one vote per
// line. When a proposal reaches [votesRequired] votes it replaces the line,
// and every proposal for that line is deleted.
//
// Parameters:
//   - [ctx]: cancels the transaction.
//   - [changeID]: the proposal.
//   - [userID]: the voter.
//   - [votesRequired]: how many votes apply a proposal.
//
// Returns:
//   - [bool]: [true] if this vote applied the proposal, [false] otherwise.
//   - [error]: [nil] on success; [ErrNotFound] if the proposal no longer
//     exists; [ErrAlreadyVoted] if the user already voted on this line;
//     otherwise the database error.
func (store *Store) VoteChange(ctx context.Context, changeID, userID int64, votesRequired int) (bool, error) {
	var applied bool
	err := store.withTransaction(ctx, func(transaction *sql.Tx) error {
		var cueID int64
		if err := transaction.QueryRowContext(ctx, `SELECT cue_id FROM subtitle_changes WHERE id = ?`, changeID).Scan(&cueID); err != nil {
			return notFound(err)
		}
		if err := checkNotVoted(ctx, transaction, cueID, userID); err != nil {
			return err
		}
		var err error
		applied, err = voteInTransaction(ctx, transaction, changeID, cueID, userID, votesRequired)
		return err
	})
	return applied, err
}

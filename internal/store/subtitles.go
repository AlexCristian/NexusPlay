package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

// Subtitle is one subtitle track for a media item.
type Subtitle struct {
	ID       int64
	MediaID  int64
	Language string // BCP 47 code, e.g. "en"
	Label    string // shown in the player, e.g. "English"
	Source   string // sidecar file it was imported from, if any
}

// Cue is a stored subtitle line.
type Cue struct {
	ID      int64
	StartMS int64
	EndMS   int64
	Text    string
}

// Change is a proposed correction to a cue, with its vote count.
type Change struct {
	ID           int64
	CueID        int64
	OriginalText string
	ProposedText string
	Votes        int
	VotedByMe    bool
}

// Errors returned by the subtitle voting flow.
var (
	ErrAlreadyVoted = errors.New("you have already voted on this line")
	ErrUnchanged    = errors.New("the proposed text is the same as the current line")
)

// NewSubtitle is a subtitle track to store.
type NewSubtitle struct {
	MediaID  int64
	Language string // BCP 47 code, e.g. "en"
	Label    string // shown in the player, e.g. "English"
	Source   string // sidecar file it came from, relative to the media folder; "" for uploads
	Cues     []subtitle.Cue
}

// AddSubtitle stores a track and its cues. A track with a Source is imported
// only once per media item: adding it again returns inserted == false and
// changes nothing.
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

// SubtitleSources returns the sidecar files that have already been imported.
func (store *Store) SubtitleSources(ctx context.Context) (map[string]bool, error) {
	sources, err := queryAll(ctx, store.database, scanString, `SELECT source FROM subtitles WHERE source <> ''`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(sources))
	for _, source := range sources {
		out[source] = true
	}
	return out, nil
}

// UpdateSubtitleLabel renames a track.
func (store *Store) UpdateSubtitleLabel(ctx context.Context, id int64, language, label string) error {
	_, err := store.database.ExecContext(ctx, `UPDATE subtitles SET language = ?, label = ? WHERE id = ?`, language, label, id)
	return err
}

// DeleteSubtitle removes a track and its cues.
func (store *Store) DeleteSubtitle(ctx context.Context, id int64) error {
	_, err := store.database.ExecContext(ctx, `DELETE FROM subtitles WHERE id = ?`, id)
	return err
}

// Subtitles lists the tracks for a media item.
func (store *Store) Subtitles(ctx context.Context, mediaID int64) ([]Subtitle, error) {
	return queryAll(ctx, store.database, func(row rowScanner) (Subtitle, error) {
		var subtitle Subtitle
		err := row.Scan(&subtitle.ID, &subtitle.MediaID, &subtitle.Language, &subtitle.Label, &subtitle.Source)
		return subtitle, err
	}, `SELECT id, media_id, language, label, source FROM subtitles WHERE media_id = ? ORDER BY label`, mediaID)
}

// SubtitleByID returns one track, or ErrNotFound.
func (store *Store) SubtitleByID(ctx context.Context, id int64) (Subtitle, error) {
	var subtitle Subtitle
	err := store.database.QueryRowContext(ctx,
		`SELECT id, media_id, language, label, source FROM subtitles WHERE id = ?`, id).
		Scan(&subtitle.ID, &subtitle.MediaID, &subtitle.Language, &subtitle.Label, &subtitle.Source)
	return subtitle, notFound(err)
}

// Cues returns a track's cues in order.
func (store *Store) Cues(ctx context.Context, subtitleID int64) ([]Cue, error) {
	return queryAll(ctx, store.database, func(row rowScanner) (Cue, error) {
		var cue Cue
		err := row.Scan(&cue.ID, &cue.StartMS, &cue.EndMS, &cue.Text)
		return cue, err
	}, `SELECT id, start_ms, end_ms, text FROM subtitle_cues WHERE subtitle_id = ? ORDER BY idx`, subtitleID)
}

// CueAt returns the cue showing at a playback time, or the closest one
// before it. It returns ErrNotFound when no cue starts at or before ms.
func (store *Store) CueAt(ctx context.Context, subtitleID, milliseconds int64) (Cue, error) {
	var cue Cue
	err := store.database.QueryRowContext(ctx, `
		SELECT id, start_ms, end_ms, text FROM subtitle_cues
		WHERE subtitle_id = ? AND start_ms <= ? ORDER BY start_ms DESC, idx DESC LIMIT 1`, subtitleID, milliseconds).
		Scan(&cue.ID, &cue.StartMS, &cue.EndMS, &cue.Text)
	return cue, notFound(err)
}

// CueByID returns one cue and the ID of its track, or ErrNotFound.
func (store *Store) CueByID(ctx context.Context, id int64) (Cue, int64, error) {
	var cue Cue
	var subID int64
	err := store.database.QueryRowContext(ctx,
		`SELECT id, start_ms, end_ms, text, subtitle_id FROM subtitle_cues WHERE id = ?`, id).
		Scan(&cue.ID, &cue.StartMS, &cue.EndMS, &cue.Text, &subID)
	return cue, subID, notFound(err)
}

// CueForChange returns the ID of the cue a proposal targets, or ErrNotFound
// when the proposal no longer exists (applying a correction deletes every
// proposal for its line).
func (store *Store) CueForChange(ctx context.Context, changeID int64) (int64, error) {
	var cueID int64
	err := store.database.QueryRowContext(ctx, `SELECT cue_id FROM subtitle_changes WHERE id = ?`, changeID).Scan(&cueID)
	return cueID, notFound(err)
}

// Changes lists the open proposals for a cue, most votes first.
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

// ProposeChange records a correction to a cue; the proposer's vote counts.
// It reports whether the change was applied straight away (when one vote is
// enough). An identical proposal from someone else becomes a vote for it.
//
// Errors: ErrNotFound if the cue does not exist, ErrUnchanged if text is the
// line's current text, ErrAlreadyVoted if the user already voted on this line.
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
		applied, err = voteTx(ctx, transaction, changeID, cueID, userID, votesRequired)
		return err
	})
	return applied, err
}

// VoteChange adds a user's vote to a proposal. Each user gets one vote per
// line. When a proposal reaches votesRequired it replaces the line and every
// competing proposal for that line is discarded; the result reports whether
// that happened.
//
// Errors: ErrNotFound if the proposal no longer exists, ErrAlreadyVoted if
// the user already voted on this line.
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
		applied, err = voteTx(ctx, transaction, changeID, cueID, userID, votesRequired)
		return err
	})
	return applied, err
}

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

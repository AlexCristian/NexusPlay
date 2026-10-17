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
func (s *Store) AddSubtitle(ctx context.Context, t NewSubtitle) (id int64, inserted bool, err error) {
	err = s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx,
			`INSERT INTO subtitles (media_id, language, label, source, created_at) VALUES (?, ?, ?, ?, ?)
			 ON CONFLICT DO NOTHING`,
			t.MediaID, t.Language, t.Label, t.Source, s.unix())
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil || n == 0 {
			return err // already imported from this source
		}
		if id, err = res.LastInsertId(); err != nil {
			return err
		}
		inserted = true
		stmt, err := tx.PrepareContext(ctx,
			`INSERT INTO subtitle_cues (subtitle_id, idx, start_ms, end_ms, text) VALUES (?, ?, ?, ?, ?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for i, c := range t.Cues {
			if _, err := stmt.ExecContext(ctx, id, i, c.StartMS, c.EndMS, c.Text); err != nil {
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
func (s *Store) SubtitleSources(ctx context.Context) (map[string]bool, error) {
	sources, err := queryAll(ctx, s.db, scanString, `SELECT source FROM subtitles WHERE source <> ''`)
	if err != nil {
		return nil, err
	}
	out := make(map[string]bool, len(sources))
	for _, src := range sources {
		out[src] = true
	}
	return out, nil
}

// UpdateSubtitleLabel renames a track.
func (s *Store) UpdateSubtitleLabel(ctx context.Context, id int64, language, label string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE subtitles SET language = ?, label = ? WHERE id = ?`, language, label, id)
	return err
}

// DeleteSubtitle removes a track and its cues.
func (s *Store) DeleteSubtitle(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM subtitles WHERE id = ?`, id)
	return err
}

// Subtitles lists the tracks for a media item.
func (s *Store) Subtitles(ctx context.Context, mediaID int64) ([]Subtitle, error) {
	return queryAll(ctx, s.db, func(r rowScanner) (Subtitle, error) {
		var t Subtitle
		err := r.Scan(&t.ID, &t.MediaID, &t.Language, &t.Label, &t.Source)
		return t, err
	}, `SELECT id, media_id, language, label, source FROM subtitles WHERE media_id = ? ORDER BY label`, mediaID)
}

// SubtitleByID returns one track.
func (s *Store) SubtitleByID(ctx context.Context, id int64) (Subtitle, error) {
	var t Subtitle
	err := s.db.QueryRowContext(ctx,
		`SELECT id, media_id, language, label, source FROM subtitles WHERE id = ?`, id).
		Scan(&t.ID, &t.MediaID, &t.Language, &t.Label, &t.Source)
	return t, notFound(err)
}

// Cues returns a track's cues in order.
func (s *Store) Cues(ctx context.Context, subtitleID int64) ([]Cue, error) {
	return queryAll(ctx, s.db, func(r rowScanner) (Cue, error) {
		var c Cue
		err := r.Scan(&c.ID, &c.StartMS, &c.EndMS, &c.Text)
		return c, err
	}, `SELECT id, start_ms, end_ms, text FROM subtitle_cues WHERE subtitle_id = ? ORDER BY idx`, subtitleID)
}

// CueAt returns the cue showing at a playback time, or the closest one before it.
func (s *Store) CueAt(ctx context.Context, subtitleID, ms int64) (Cue, error) {
	var c Cue
	err := s.db.QueryRowContext(ctx, `
		SELECT id, start_ms, end_ms, text FROM subtitle_cues
		WHERE subtitle_id = ? AND start_ms <= ? ORDER BY start_ms DESC, idx DESC LIMIT 1`, subtitleID, ms).
		Scan(&c.ID, &c.StartMS, &c.EndMS, &c.Text)
	return c, notFound(err)
}

// CueByID returns one cue and the track it belongs to.
func (s *Store) CueByID(ctx context.Context, id int64) (Cue, int64, error) {
	var c Cue
	var subID int64
	err := s.db.QueryRowContext(ctx,
		`SELECT id, start_ms, end_ms, text, subtitle_id FROM subtitle_cues WHERE id = ?`, id).
		Scan(&c.ID, &c.StartMS, &c.EndMS, &c.Text, &subID)
	return c, subID, notFound(err)
}

// CueForChange returns the cue a proposal targets.
func (s *Store) CueForChange(ctx context.Context, changeID int64) (int64, error) {
	var cueID int64
	err := s.db.QueryRowContext(ctx, `SELECT cue_id FROM subtitle_changes WHERE id = ?`, changeID).Scan(&cueID)
	return cueID, notFound(err)
}

// Changes lists the open proposals for a cue, most votes first.
func (s *Store) Changes(ctx context.Context, cueID, userID int64) ([]Change, error) {
	return queryAll(ctx, s.db, func(r rowScanner) (Change, error) {
		var c Change
		err := r.Scan(&c.ID, &c.CueID, &c.OriginalText, &c.ProposedText, &c.Votes, &c.VotedByMe)
		return c, err
	}, `
		SELECT ch.id, ch.cue_id, cu.text, ch.proposed_text,
		       (SELECT COUNT(*) FROM subtitle_votes v WHERE v.change_id = ch.id),
		       EXISTS (SELECT 1 FROM subtitle_votes v WHERE v.change_id = ch.id AND v.user_id = ?)
		FROM subtitle_changes ch JOIN subtitle_cues cu ON cu.id = ch.cue_id
		WHERE ch.cue_id = ? ORDER BY 5 DESC, ch.id`, userID, cueID)
}

// ProposeChange records a correction to a cue; the proposer's vote counts.
// It reports whether the change was applied straight away (when one vote is enough).
func (s *Store) ProposeChange(ctx context.Context, cueID, userID int64, text string, votesRequired int) (bool, error) {
	text = strings.TrimSpace(text)
	var applied bool
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var current string
		if err := tx.QueryRowContext(ctx, `SELECT text FROM subtitle_cues WHERE id = ?`, cueID).Scan(&current); err != nil {
			return notFound(err)
		}
		if text == current {
			return ErrUnchanged
		}
		if err := checkNotVoted(ctx, tx, cueID, userID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO subtitle_changes (cue_id, proposed_text, created_by, created_at) VALUES (?, ?, ?, ?)
			ON CONFLICT(cue_id, proposed_text) DO NOTHING`, cueID, text, userID, s.unix()); err != nil {
			return err
		}
		var changeID int64
		if err := tx.QueryRowContext(ctx,
			`SELECT id FROM subtitle_changes WHERE cue_id = ? AND proposed_text = ?`, cueID, text).Scan(&changeID); err != nil {
			return err
		}
		var err error
		applied, err = voteTx(ctx, tx, changeID, cueID, userID, votesRequired)
		return err
	})
	return applied, err
}

// VoteChange adds a user's vote to a proposal. Each user gets one vote per
// line. When a proposal reaches votesRequired it replaces the line and every
// competing proposal for that line is discarded.
func (s *Store) VoteChange(ctx context.Context, changeID, userID int64, votesRequired int) (bool, error) {
	var applied bool
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var cueID int64
		if err := tx.QueryRowContext(ctx, `SELECT cue_id FROM subtitle_changes WHERE id = ?`, changeID).Scan(&cueID); err != nil {
			return notFound(err)
		}
		if err := checkNotVoted(ctx, tx, cueID, userID); err != nil {
			return err
		}
		var err error
		applied, err = voteTx(ctx, tx, changeID, cueID, userID, votesRequired)
		return err
	})
	return applied, err
}

func checkNotVoted(ctx context.Context, tx *sql.Tx, cueID, userID int64) error {
	var voted bool
	err := tx.QueryRowContext(ctx, `
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

func voteTx(ctx context.Context, tx *sql.Tx, changeID, cueID, userID int64, votesRequired int) (bool, error) {
	if _, err := tx.ExecContext(ctx, `INSERT INTO subtitle_votes (change_id, user_id) VALUES (?, ?)`, changeID, userID); err != nil {
		return false, err
	}
	var votes int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM subtitle_votes WHERE change_id = ?`, changeID).Scan(&votes); err != nil {
		return false, err
	}
	if votes < votesRequired {
		return false, nil
	}
	if _, err := tx.ExecContext(ctx, `
		UPDATE subtitle_cues SET text = (SELECT proposed_text FROM subtitle_changes WHERE id = ?) WHERE id = ?`,
		changeID, cueID); err != nil {
		return false, err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM subtitle_changes WHERE cue_id = ?`, cueID)
	return err == nil, err
}

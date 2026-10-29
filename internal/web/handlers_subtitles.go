package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

// subtitleHandlers serve subtitle tracks and the crowdsourced corrections:
// reporting a line, proposing a fix and voting on one.
type subtitleHandlers struct {
	responder
	store         subtitleStore
	votesRequired int // votes that apply a correction
}

// subtitleStore is the part of the database the subtitle features use.
type subtitleStore interface {
	mediaGetter
	SubtitleByID(ctx context.Context, id int64) (store.Subtitle, error)
	Cues(ctx context.Context, subtitleID int64) ([]store.Cue, error)
	CueAt(ctx context.Context, subtitleID, milliseconds int64) (store.Cue, error)
	CueByID(ctx context.Context, id int64) (store.Cue, int64, error)
	CueForChange(ctx context.Context, changeID int64) (int64, error)
	Changes(ctx context.Context, cueID, userID int64) ([]store.Change, error)
	ProposeChange(ctx context.Context, cueID, userID int64, text string, votesRequired int) (bool, error)
	VoteChange(ctx context.Context, changeID, userID int64, votesRequired int) (bool, error)
}

// visibleCue returns store.ErrNotFound unless the subtitle line belongs to
// media the viewer can see.
func (handlers *subtitleHandlers) visibleCue(ctx context.Context, viewer Viewer, cueID int64) error {
	_, subID, err := handlers.store.CueByID(ctx, cueID)
	if err != nil {
		return err
	}
	track, err := handlers.store.SubtitleByID(ctx, subID)
	if err != nil {
		return err
	}
	_, err = visibleMediaByID(ctx, handlers.store, viewer, track.MediaID)
	return err
}

// subtitleVTT handles GET /subtitles/{id}: a subtitle track as WebVTT, built
// from the database so accepted corrections show at once.
func (handlers *subtitleHandlers) subtitleVTT(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	track, err := handlers.store.SubtitleByID(ctx, id)
	if err != nil {
		return err
	}
	if _, err := visibleMediaByID(ctx, handlers.store, viewer(r), track.MediaID); err != nil {
		return err
	}
	cues, err := handlers.store.Cues(ctx, id)
	if err != nil {
		return err
	}
	out := make([]subtitle.Cue, len(cues))
	for i, cue := range cues {
		out[i] = subtitle.Cue{StartMS: cue.StartMS, EndMS: cue.EndMS, Text: cue.Text}
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(subtitle.WriteVTT(out)) // a failed write means the client left
	return nil
}

// subtitleReport handles GET /media/{id}/subtitle-report?track=&t=: the
// "Report a subtitle error" dialog for the line on screen at t milliseconds.
func (handlers *subtitleHandlers) subtitleReport(w http.ResponseWriter, r *http.Request) error {
	media, err := visibleMedia(r, handlers.store)
	if err != nil {
		return err
	}
	trackID, err := strconv.ParseInt(r.URL.Query().Get("track"), 10, 64)
	if err != nil {
		return badRequest("Choose a subtitle track first.")
	}
	milliseconds, _ := strconv.ParseInt(r.URL.Query().Get("t"), 10, 64) // missing or malformed: the start of the video
	track, err := handlers.store.SubtitleByID(r.Context(), trackID)
	if err != nil {
		return err
	}
	if track.MediaID != media.ID {
		return store.ErrNotFound
	}
	cue, err := handlers.store.CueAt(r.Context(), trackID, milliseconds)
	if errors.Is(err, store.ErrNotFound) {
		handlers.render(w, r, http.StatusOK, alert("info", "No subtitle line has been shown yet. Pause on the line you want to correct."))
		return nil
	}
	if err != nil {
		return err
	}
	return handlers.renderReport(w, r, track, cue, reportNote{})
}

// reportNote is the message shown at the top of the report dialog.
type reportNote struct {
	message string
	kind    string // "success" or "error"
}

// renderReport shows the report dialog for cue, with the open proposals
// for it and note at the top.
func (handlers *subtitleHandlers) renderReport(w http.ResponseWriter, r *http.Request, track store.Subtitle, cue store.Cue, note reportNote) error {
	changes, err := handlers.store.Changes(r.Context(), cue.ID, viewer(r).ID())
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, subtitleReport(viewer(r).CSRF, track, cue, changes, note.message, note.kind))
	return nil
}

// proposeChange handles POST /cues/{id}/changes: a viewer's correction of a
// subtitle line, which counts as their vote for it.
func (handlers *subtitleHandlers) proposeChange(w http.ResponseWriter, r *http.Request) error {
	cueID, err := pathID(r)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" || len(text) > 500 {
		return badRequest("A correction must be between 1 and 500 characters.")
	}
	if err := handlers.visibleCue(r.Context(), viewer(r), cueID); err != nil {
		return err
	}
	applied, err := handlers.store.ProposeChange(r.Context(), cueID, viewer(r).ID(), text, handlers.votesRequired)
	note, err := voteOutcome(applied, err, "Correction submitted. It is applied once enough people vote for it.")
	if err != nil {
		return err
	}
	return handlers.showVoteResult(w, r, cueID, note)
}

// voteChange handles POST /changes/{id}/vote: a vote for someone's correction.
func (handlers *subtitleHandlers) voteChange(w http.ResponseWriter, r *http.Request) error {
	changeID, err := pathID(r)
	if err != nil {
		return err
	}
	cueID, err := handlers.store.CueForChange(r.Context(), changeID)
	if errors.Is(err, store.ErrNotFound) {
		return &statusError{status: http.StatusNotFound, title: "Not found",
			message: "That correction no longer exists. It may have just been applied."}
	}
	if err != nil {
		return err
	}
	if err := handlers.visibleCue(r.Context(), viewer(r), cueID); err != nil {
		return err
	}
	applied, err := handlers.store.VoteChange(r.Context(), changeID, viewer(r).ID(), handlers.votesRequired)
	note, err := voteOutcome(applied, err, "Vote submitted.")
	if err != nil {
		return err
	}
	return handlers.showVoteResult(w, r, cueID, note)
}

// sentence capitalizes an error message for display and ends it with a
// full stop (Go error strings are lowercase, without one).
func sentence(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:] + "."
}

// voteOutcome turns the result of a proposal or vote into the note shown to
// the viewer. The voting rules' own refusals are shown, not treated as errors.
func voteOutcome(applied bool, err error, okMessage string) (reportNote, error) {
	switch {
	case errors.Is(err, store.ErrAlreadyVoted), errors.Is(err, store.ErrUnchanged):
		return reportNote{message: sentence(err.Error()), kind: "error"}, nil
	case err != nil:
		return reportNote{}, err
	case applied:
		return reportNote{message: "Change applied! It shows the next time the video is loaded.", kind: "success"}, nil
	default:
		return reportNote{message: okMessage, kind: "success"}, nil
	}
}

// showVoteResult re-renders the report dialog after a proposal or vote. A
// plain form post (scripts not loaded yet) goes back to the video instead.
func (handlers *subtitleHandlers) showVoteResult(w http.ResponseWriter, r *http.Request, cueID int64, note reportNote) error {
	cue, subID, err := handlers.store.CueByID(r.Context(), cueID)
	if err != nil {
		return err
	}
	track, err := handlers.store.SubtitleByID(r.Context(), subID)
	if err != nil {
		return err
	}
	if !isHTMX(r) {
		http.Redirect(w, r, "/watch/"+itoa(track.MediaID), http.StatusSeeOther)
		return nil
	}
	return handlers.renderReport(w, r, track, cue, note)
}

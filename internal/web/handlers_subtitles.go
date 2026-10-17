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
	CueAt(ctx context.Context, subtitleID, ms int64) (store.Cue, error)
	CueByID(ctx context.Context, id int64) (store.Cue, int64, error)
	CueForChange(ctx context.Context, changeID int64) (int64, error)
	Changes(ctx context.Context, cueID, userID int64) ([]store.Change, error)
	ProposeChange(ctx context.Context, cueID, userID int64, text string, votesRequired int) (bool, error)
	VoteChange(ctx context.Context, changeID, userID int64, votesRequired int) (bool, error)
}

// visibleCue returns store.ErrNotFound unless the subtitle line belongs to
// media the viewer can see.
func (h *subtitleHandlers) visibleCue(ctx context.Context, v Viewer, cueID int64) error {
	_, subID, err := h.store.CueByID(ctx, cueID)
	if err != nil {
		return err
	}
	track, err := h.store.SubtitleByID(ctx, subID)
	if err != nil {
		return err
	}
	_, err = visibleMediaByID(ctx, h.store, v, track.MediaID)
	return err
}

// subtitleVTT handles GET /subtitles/{id}: a subtitle track as WebVTT, built
// from the database so accepted corrections show at once.
func (h *subtitleHandlers) subtitleVTT(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	track, err := h.store.SubtitleByID(ctx, id)
	if err != nil {
		return err
	}
	if _, err := visibleMediaByID(ctx, h.store, viewer(r), track.MediaID); err != nil {
		return err
	}
	cues, err := h.store.Cues(ctx, id)
	if err != nil {
		return err
	}
	out := make([]subtitle.Cue, len(cues))
	for i, c := range cues {
		out[i] = subtitle.Cue{StartMS: c.StartMS, EndMS: c.EndMS, Text: c.Text}
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(subtitle.WriteVTT(out)) // a failed write means the client left
	return nil
}

// subtitleReport handles GET /media/{id}/subtitle-report?track=&t=: the
// "Report a subtitle error" dialog for the line on screen at t milliseconds.
func (h *subtitleHandlers) subtitleReport(w http.ResponseWriter, r *http.Request) error {
	m, err := visibleMedia(r, h.store)
	if err != nil {
		return err
	}
	trackID, err := strconv.ParseInt(r.URL.Query().Get("track"), 10, 64)
	if err != nil {
		return badRequest("Choose a subtitle track first.")
	}
	ms, _ := strconv.ParseInt(r.URL.Query().Get("t"), 10, 64) // missing or malformed: the start of the video
	track, err := h.store.SubtitleByID(r.Context(), trackID)
	if err != nil {
		return err
	}
	if track.MediaID != m.ID {
		return store.ErrNotFound
	}
	cue, err := h.store.CueAt(r.Context(), trackID, ms)
	if errors.Is(err, store.ErrNotFound) {
		h.render(w, r, http.StatusOK, alert("info", "No subtitle line has been shown yet. Pause on the line you want to correct."))
		return nil
	}
	if err != nil {
		return err
	}
	return h.renderReport(w, r, track, cue, reportNote{})
}

// reportNote is the message shown at the top of the report dialog.
type reportNote struct {
	msg  string
	kind string // "success" or "error"
}

// renderReport shows the report dialog for cue, with the open proposals
// for it and note at the top.
func (h *subtitleHandlers) renderReport(w http.ResponseWriter, r *http.Request, track store.Subtitle, cue store.Cue, note reportNote) error {
	changes, err := h.store.Changes(r.Context(), cue.ID, viewer(r).ID())
	if err != nil {
		return err
	}
	h.render(w, r, http.StatusOK, subtitleReport(viewer(r).CSRF, track, cue, changes, note.msg, note.kind))
	return nil
}

// proposeChange handles POST /cues/{id}/changes: a viewer's correction of a
// subtitle line, which counts as their vote for it.
func (h *subtitleHandlers) proposeChange(w http.ResponseWriter, r *http.Request) error {
	cueID, err := pathID(r)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" || len(text) > 500 {
		return badRequest("A correction must be between 1 and 500 characters.")
	}
	if err := h.visibleCue(r.Context(), viewer(r), cueID); err != nil {
		return err
	}
	applied, err := h.store.ProposeChange(r.Context(), cueID, viewer(r).ID(), text, h.votesRequired)
	note, err := voteOutcome(applied, err, "Correction submitted. It is applied once enough people vote for it.")
	if err != nil {
		return err
	}
	return h.showVoteResult(w, r, cueID, note)
}

// voteChange handles POST /changes/{id}/vote: a vote for someone's correction.
func (h *subtitleHandlers) voteChange(w http.ResponseWriter, r *http.Request) error {
	changeID, err := pathID(r)
	if err != nil {
		return err
	}
	cueID, err := h.store.CueForChange(r.Context(), changeID)
	if errors.Is(err, store.ErrNotFound) {
		return &statusError{status: http.StatusNotFound, title: "Not found",
			msg: "That correction no longer exists. It may have just been applied."}
	}
	if err != nil {
		return err
	}
	if err := h.visibleCue(r.Context(), viewer(r), cueID); err != nil {
		return err
	}
	applied, err := h.store.VoteChange(r.Context(), changeID, viewer(r).ID(), h.votesRequired)
	note, err := voteOutcome(applied, err, "Vote submitted.")
	if err != nil {
		return err
	}
	return h.showVoteResult(w, r, cueID, note)
}

// voteOutcome turns the result of a proposal or vote into the note shown to
// the viewer. The voting rules' own refusals are shown, not treated as errors.
func voteOutcome(applied bool, err error, okMsg string) (reportNote, error) {
	switch {
	case errors.Is(err, store.ErrAlreadyVoted), errors.Is(err, store.ErrUnchanged):
		return reportNote{msg: err.Error() + ".", kind: "error"}, nil
	case err != nil:
		return reportNote{}, err
	case applied:
		return reportNote{msg: "Change applied! It shows the next time the video is loaded.", kind: "success"}, nil
	default:
		return reportNote{msg: okMsg, kind: "success"}, nil
	}
}

// showVoteResult re-renders the report dialog after a proposal or vote. A
// plain form post (scripts not loaded yet) goes back to the video instead.
func (h *subtitleHandlers) showVoteResult(w http.ResponseWriter, r *http.Request, cueID int64, note reportNote) error {
	cue, subID, err := h.store.CueByID(r.Context(), cueID)
	if err != nil {
		return err
	}
	track, err := h.store.SubtitleByID(r.Context(), subID)
	if err != nil {
		return err
	}
	if !isHTMX(r) {
		http.Redirect(w, r, "/watch/"+itoa(track.MediaID), http.StatusSeeOther)
		return nil
	}
	return h.renderReport(w, r, track, cue, note)
}

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

// [subtitleHandlers] serve subtitle tracks and the crowdsourced corrections:
// reporting a line, proposing a fix and voting on one.
type subtitleHandlers struct {
	// [responder] writes the responses.
	responder
	// [store] reads tracks and lines and records proposals and votes.
	store subtitleStore
	// [votesRequired] is how many votes apply a correction.
	votesRequired int
}

// [maxCorrectionBytes] is the longest correction accepted, in bytes. The
// message shown says "characters", which is only exact for ASCII text.
const maxCorrectionBytes = 500

// [subtitleStore] is the part of the database the subtitle features use;
// [store.Store] implements it. Each method is described at the
// [store.Store] method of the same name.
type subtitleStore interface {
	// [mediaGetter] loads media items, for the visibility checks.
	mediaGetter
	// [SubtitleByID] loads one track.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [id]: the track.
	//
	// Returns:
	//   - [store.Subtitle]: the track.
	//   - [error]: [nil] on success, [store.ErrNotFound], or another database
	//     error.
	SubtitleByID(ctx context.Context, id int64) (store.Subtitle, error)
	// [Cues] lists a track's lines in order.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [subtitleID]: the track.
	//
	// Returns:
	//   - [[]store.Cue]: the lines with their current text.
	//   - [error]: [nil] on success, or the database error.
	Cues(ctx context.Context, subtitleID int64) ([]store.Cue, error)
	// [CueAt] finds the line showing at a playback time.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [subtitleID]: the track.
	//   - [milliseconds]: the playback time.
	//
	// Returns:
	//   - [store.Cue]: the line that started most recently at or before it.
	//   - [error]: [nil] on success, [store.ErrNotFound] if none had started,
	//     or another database error.
	CueAt(ctx context.Context, subtitleID, milliseconds int64) (store.Cue, error)
	// [CueByID] loads one line and its track's ID.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [id]: the line.
	//
	// Returns:
	//   - [store.Cue]: the line.
	//   - [int64]: its track's ID.
	//   - [error]: [nil] on success, [store.ErrNotFound], or another database
	//     error.
	CueByID(ctx context.Context, id int64) (store.Cue, int64, error)
	// [CueForChange] finds the line a proposal corrects.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [changeID]: the proposal.
	//
	// Returns:
	//   - [int64]: the line's ID.
	//   - [error]: [nil] on success, [store.ErrNotFound] if the proposal no
	//     longer exists, or another database error.
	CueForChange(ctx context.Context, changeID int64) (int64, error)
	// [Changes] lists the open proposals for a line.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [cueID]: the line.
	//   - [userID]: the viewer, for [store.Change.VotedByMe].
	//
	// Returns:
	//   - [[]store.Change]: the proposals, most votes first.
	//   - [error]: [nil] on success, or the database error.
	Changes(ctx context.Context, cueID, userID int64) ([]store.Change, error)
	// [ProposeChange] records a correction with the proposer's vote.
	//
	// Parameters:
	//   - [ctx]: cancels the transaction.
	//   - [cueID]: the line.
	//   - [userID]: the proposer.
	//   - [text]: the corrected text.
	//   - [votesRequired]: how many votes apply a proposal.
	//
	// Returns:
	//   - [bool]: [true] if the correction was applied straight away.
	//   - [error]: as [store.Store.ProposeChange] describes.
	ProposeChange(ctx context.Context, cueID, userID int64, text string, votesRequired int) (bool, error)
	// [VoteChange] adds a vote to a proposal.
	//
	// Parameters:
	//   - [ctx]: cancels the transaction.
	//   - [changeID]: the proposal.
	//   - [userID]: the voter.
	//   - [votesRequired]: how many votes apply a proposal.
	//
	// Returns:
	//   - [bool]: [true] if this vote applied the proposal.
	//   - [error]: as [store.Store.VoteChange] describes.
	VoteChange(ctx context.Context, changeID, userID int64, votesRequired int) (bool, error)
}

// [visibleCue] checks that a subtitle line belongs to media the viewer may
// see.
//
// Parameters:
//   - [ctx]: cancels the queries.
//   - [viewer]: who is asking.
//   - [cueID]: the line.
//
// Returns:
//   - [error]: [nil] if the viewer may see it; [store.ErrNotFound] if the
//     line, its track or its media item does not exist or is hidden;
//     otherwise the database error.
func (handlers *subtitleHandlers) visibleCue(ctx context.Context, viewer Viewer, cueID int64) error {
	_, subtitleID, err := handlers.store.CueByID(ctx, cueID)
	if err != nil {
		return err
	}
	track, err := handlers.store.SubtitleByID(ctx, subtitleID)
	if err != nil {
		return err
	}
	_, err = visibleMediaByID(ctx, handlers.store, viewer, track.MediaID)
	return err
}

// [subtitleVTT] handles GET /subtitles/{id}: a subtitle track as WebVTT,
// built from the database so accepted corrections show at once. Browsers
// must check back with the server before reusing a stored copy (no-cache).
//
// Parameters:
//   - [w]: where the track is written; a failed write means the client
//     left and is ignored.
//   - [r]: the request; {id} in its path names the track.
//
// Returns:
//   - [error]: [nil] once written; [store.ErrNotFound] if {id} is not a
//     valid ID, or the track or its media item does not exist or is hidden;
//     otherwise the database error.
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
	vttCues := make([]subtitle.Cue, len(cues))
	for i, cue := range cues {
		vttCues[i] = subtitle.Cue{StartMS: cue.StartMS, EndMS: cue.EndMS, Text: cue.Text}
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(subtitle.WriteVTT(vttCues)) // a failed write means the client left
	return nil
}

// [subtitleReport] handles GET /media/{id}/subtitle-report?track=&t=: the
// "Report a subtitle error" dialog for the line on screen at "t"
// milliseconds, or the last one shown before it (the start of the video if missing or malformed). If no line
// has been shown yet, it answers with a note instead.
//
// Parameters:
//   - [w]: where the dialog is written.
//   - [r]: the request from a signed-in viewer; {id} names the media item,
//     "track" the subtitle track and "t" the playback time.
//
// Returns:
//   - [error]: [nil] once written; a 400 [*statusError] if "track" is not a
//     number; [store.ErrNotFound] if the media item or track does not exist,
//     is hidden, or the track is not the item's; otherwise the database
//     error.
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

// [reportNote] is the message shown at the top of the report dialog.
type reportNote struct {
	// [message] is the sentence shown, or "" for none.
	message string
	// [kind] styles the message: "success" or "error".
	kind string
}

// [renderReport] shows the report dialog for a line, with its open proposals
// and a note at the top.
//
// Parameters:
//   - [w]: where the dialog is written.
//   - [r]: the request, which tells who the viewer is.
//   - [track]: the line's track.
//   - [cue]: the line.
//   - [note]: the message at the top, or a zero [reportNote] for none.
//
// Returns:
//   - [error]: [nil] once written, or the database error.
func (handlers *subtitleHandlers) renderReport(w http.ResponseWriter, r *http.Request, track store.Subtitle, cue store.Cue, note reportNote) error {
	changes, err := handlers.store.Changes(r.Context(), cue.ID, viewer(r).ID())
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, subtitleReport(viewer(r).CSRF, track, cue, changes, note.message, note.kind))
	return nil
}

// [proposeChange] handles POST /cues/{id}/changes: a viewer's correction of a
// subtitle line, which counts as their vote for it. It answers with the
// report dialog and a note saying what happened.
//
// Parameters:
//   - [w]: where the dialog, or for a plain form a redirect, is written.
//   - [r]: the request from a signed-in viewer; {id} names the line and the
//     form field "text" holds the correction.
//
// Returns:
//   - [error]: [nil] once answered, including when the voting rules refuse
//     the correction (shown in the dialog); a 400 [*statusError] if "text"
//     is empty or longer than [maxCorrectionBytes]; [store.ErrNotFound] if
//     the line does not exist or is hidden; otherwise the database error.
func (handlers *subtitleHandlers) proposeChange(w http.ResponseWriter, r *http.Request) error {
	cueID, err := pathID(r)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" || len(text) > maxCorrectionBytes {
		return badRequest("A correction must be between 1 and " + strconv.Itoa(maxCorrectionBytes) + " characters.")
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

// [voteChange] handles POST /changes/{id}/vote: a vote for someone's
// correction. It answers with the report dialog and a note saying what
// happened.
//
// Parameters:
//   - [w]: where the dialog, or for a plain form a redirect, is written.
//   - [r]: the request from a signed-in viewer; {id} names the proposal.
//
// Returns:
//   - [error]: [nil] once answered, including when the viewer already voted
//     on the line (shown in the dialog); a 404 [*statusError] saying the
//     correction may have just been applied if the proposal no longer
//     exists; [store.ErrNotFound] if its line is hidden; otherwise the
//     database error.
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

// [sentence] turns an error message into a sentence for display, since Go
// error messages start in lower case and have no full stop.
//
// Parameters:
//   - [text]: the message, such as "you have already voted on this line".
//
// Returns:
//   - [string]: [text] with its first byte in upper case and "." added, or
//     "" for "". Only correct for messages starting with an ASCII letter.
func sentence(text string) string {
	if text == "" {
		return text
	}
	return strings.ToUpper(text[:1]) + text[1:] + "."
}

// [voteOutcome] turns the result of a proposal or vote into the note shown to
// the viewer. The voting rules' own refusals ([store.ErrAlreadyVoted] and
// [store.ErrUnchanged]) are shown, not treated as errors.
//
// Parameters:
//   - [applied]: whether the proposal or vote applied the correction.
//   - [err]: the proposal's or vote's error, or [nil].
//   - [okMessage]: the note for a success that did not apply anything yet.
//
// Returns:
//   - [reportNote]: the note to show, or a zero [reportNote] if [error] is
//     not [nil].
//   - [error]: [nil] unless [err] is an unexpected error, which is returned
//     unchanged.
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

// [showVoteResult] shows the report dialog again after a proposal or vote. A
// plain form post (scripts not loaded yet) goes back to the video instead.
//
// Parameters:
//   - [w]: where the dialog or redirect is written.
//   - [r]: the request.
//   - [cueID]: the line.
//   - [note]: the message at the top.
//
// Returns:
//   - [error]: [nil] once answered; [store.ErrNotFound] if the line or its
//     track no longer exists; otherwise the database error.
func (handlers *subtitleHandlers) showVoteResult(w http.ResponseWriter, r *http.Request, cueID int64, note reportNote) error {
	cue, subtitleID, err := handlers.store.CueByID(r.Context(), cueID)
	if err != nil {
		return err
	}
	track, err := handlers.store.SubtitleByID(r.Context(), subtitleID)
	if err != nil {
		return err
	}
	if !isHTMX(r) {
		http.Redirect(w, r, "/watch/"+formatInteger(track.MediaID), http.StatusSeeOther)
		return nil
	}
	return handlers.renderReport(w, r, track, cue, note)
}

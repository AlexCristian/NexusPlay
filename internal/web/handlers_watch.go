package web

import (
	"context"
	"errors"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/media"
	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/stream"
	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

// visibleTo reports whether a viewer may see a media item. Unpublished media
// is visible only to administrators, who preview it before publishing; to
// everyone else it does not exist (404, not 403).
func visibleTo(m store.Media, v Viewer) bool {
	return m.Published || v.IsAdmin()
}

// visibleMedia loads the media named by {id} in the path. It returns
// store.ErrNotFound when the media does not exist or is hidden from the viewer.
func (s *Server) visibleMedia(r *http.Request) (store.Media, error) {
	id, err := pathID(r)
	if err != nil {
		return store.Media{}, err
	}
	return s.visibleMediaByID(r.Context(), viewer(r), id)
}

func (s *Server) visibleMediaByID(ctx context.Context, v Viewer, id int64) (store.Media, error) {
	m, err := s.Store.MediaByID(ctx, id)
	if err != nil {
		return store.Media{}, err
	}
	if !visibleTo(m, v) {
		return store.Media{}, store.ErrNotFound
	}
	return m, nil
}

// visibleCue returns store.ErrNotFound unless the subtitle line belongs to
// media the viewer can see.
func (s *Server) visibleCue(ctx context.Context, v Viewer, cueID int64) error {
	_, subID, err := s.Store.CueByID(ctx, cueID)
	if err != nil {
		return err
	}
	track, err := s.Store.SubtitleByID(ctx, subID)
	if err != nil {
		return err
	}
	_, err = s.visibleMediaByID(ctx, v, track.MediaID)
	return err
}

// watch handles GET /watch/{id}: the player page.
func (s *Server) watch(w http.ResponseWriter, r *http.Request) error {
	m, err := s.visibleMedia(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	subs, err := s.Store.Subtitles(ctx, m.ID)
	if err != nil {
		return err
	}
	v := viewer(r)
	var resumeAt float64
	if v.User != nil {
		if resumeAt, err = s.Store.Progress(ctx, v.ID(), m.ID); err != nil {
			return err
		}
		// Opening the video dismisses its "new episode" notification.
		if err := s.Store.ClearNotification(ctx, v.ID(), m.ID); err != nil {
			s.Log.Warn("clearing notification", "err", err)
		}
	}
	if m.Published {
		if err := s.Store.IncrementViews(ctx, m.ID); err != nil {
			s.Log.Warn("counting view", "err", err)
		}
	}
	title := m.Title
	if m.CollectionName != "" {
		title = m.CollectionName + " · " + m.Title
	}
	s.render(w, r, http.StatusOK, watchPage(s.page(r, title), m, playbackFor(m), subs, resumeAt))
	return nil
}

// playbackFor tells the player where to load a video from: the file itself,
// or an on-the-fly HLS conversion.
func playbackFor(m store.Media) Playback {
	if media.Deliver(formatOf(m)) == media.DirectPlay {
		return Playback{Direct: true, URL: "/media/" + itoa(m.ID) + "/file"}
	}
	return Playback{URL: "/media/" + itoa(m.ID) + "/hls/index.m3u8"}
}

// formatOf describes a media item for the playback policy.
func formatOf(m store.Media) media.Format {
	return media.Format{Path: m.Path, VideoCodec: m.VideoCodec, AudioCodec: m.AudioCodec, PixFmt: m.PixFmt}
}

// mediaPath resolves a media item's file, refusing anything outside the media
// folder. (Paths come from the scanner, so this is defence in depth.)
func (s *Server) mediaPath(m store.Media) (string, error) {
	p := filepath.Join(s.Cfg.MediaDir, filepath.FromSlash(m.Path))
	rel, err := filepath.Rel(s.Cfg.MediaDir, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("media path escapes the media folder")
	}
	return p, nil
}

// mediaFile handles GET /media/{id}/file: the original file, with range
// requests so the player can seek.
func (s *Server) mediaFile(w http.ResponseWriter, r *http.Request) error {
	m, err := s.visibleMedia(r)
	if err != nil {
		return err
	}
	p, err := s.mediaPath(m)
	if err != nil {
		return err
	}
	f, err := os.Open(p)
	if err != nil {
		return store.ErrNotFound // deleted since the last scan
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if ct, ok := media.MIMEType(m.Path); ok {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, "", info.ModTime(), f)
	return nil
}

// hls serves an on-demand transcode for files browsers cannot play
// directly: GET /media/{id}/hls/index.m3u8 starts (or reuses) the transcode
// and returns its playlist; GET /media/{id}/hls/segNNNNN.ts returns a segment.
// A segment request for a transcode that was evicted restarts it. At the
// transcode limit the answer is 503 with Retry-After, which player.js shows.
func (s *Server) hls(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("file")
	if name != "index.m3u8" && !stream.IsSegmentName(name) {
		return store.ErrNotFound
	}
	m, err := s.visibleMedia(r)
	if err != nil {
		return err
	}
	var playlist string
	if _, running := s.Transcodes.Touch(m.ID); name == "index.m3u8" || !running {
		if playlist, err = s.startTranscode(r.Context(), m); err != nil {
			return err
		}
	}
	if name == "index.m3u8" {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
		http.ServeFile(w, r, playlist)
		return nil
	}
	segment, err := s.Transcodes.Segment(r.Context(), m.ID, name)
	if err != nil {
		return store.ErrNotFound
	}
	w.Header().Set("Content-Type", "video/mp2t")
	http.ServeFile(w, r, segment)
	return nil
}

// startTranscode starts or reuses the HLS transcode of m and returns its
// playlist. A failed transcode is logged here, with the media it was for.
func (s *Server) startTranscode(ctx context.Context, m store.Media) (string, error) {
	src, err := s.mediaPath(m)
	if err != nil {
		return "", err
	}
	playlist, err := s.Transcodes.Playlist(ctx, m.ID, src, formatOf(m))
	if err != nil && !errors.Is(err, stream.ErrBusy) && ctx.Err() == nil {
		s.Log.Error("transcode failed", "media", m.ID, "err", err)
		return "", &statusError{status: http.StatusInternalServerError, title: "Playback failed",
			msg: "This video could not be converted for playback."}
	}
	return playlist, err
}

// subtitleVTT handles GET /subtitles/{id}: a subtitle track as WebVTT, built
// from the database so accepted corrections show at once.
func (s *Server) subtitleVTT(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	track, err := s.Store.SubtitleByID(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.visibleMediaByID(ctx, viewer(r), track.MediaID); err != nil {
		return err
	}
	cues, err := s.Store.Cues(ctx, id)
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

// poster handles GET /posters/{name}. Only plain file names in the poster
// folder are served.
func (s *Server) poster(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	if name == "" || filepath.Base(name) != name || strings.HasPrefix(name, ".") {
		return store.ErrNotFound
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.Cfg.PosterDir(), name))
	return nil
}

// saveProgress handles POST /me/progress from player.js, usually through
// navigator.sendBeacon. Two rules mark a video watched, on purpose: the
// player sends finished=1 when it ends, and the server also counts 95% as
// finished, because a tab closed during the credits never sends "ended".
func (s *Server) saveProgress(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(r.FormValue("media_id"), 10, 64)
	if err != nil {
		return badRequest("The media_id is not a number.")
	}
	pos, err := strconv.ParseFloat(r.FormValue("position"), 64)
	if err != nil || pos < 0 || math.IsNaN(pos) || math.IsInf(pos, 0) {
		return badRequest("The position must be a number of seconds.")
	}
	v := viewer(r)
	m, err := s.visibleMediaByID(r.Context(), v, id)
	if err != nil {
		return err
	}
	finished := r.FormValue("finished") == "1" || (m.Duration > 0 && pos >= m.Duration*0.95)
	if err := s.Store.SaveProgress(r.Context(), v.ID(), id, pos, finished); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

// subtitleReport handles GET /media/{id}/subtitle-report?track=&t=: the
// "Report a subtitle error" dialog for the line on screen at t milliseconds.
func (s *Server) subtitleReport(w http.ResponseWriter, r *http.Request) error {
	m, err := s.visibleMedia(r)
	if err != nil {
		return err
	}
	trackID, err := strconv.ParseInt(r.URL.Query().Get("track"), 10, 64)
	if err != nil {
		return badRequest("Choose a subtitle track first.")
	}
	ms, _ := strconv.ParseInt(r.URL.Query().Get("t"), 10, 64) // missing or malformed: the start of the video
	track, err := s.Store.SubtitleByID(r.Context(), trackID)
	if err != nil {
		return err
	}
	if track.MediaID != m.ID {
		return store.ErrNotFound
	}
	cue, err := s.Store.CueAt(r.Context(), trackID, ms)
	if errors.Is(err, store.ErrNotFound) {
		s.render(w, r, http.StatusOK, alert("info", "No subtitle line has been shown yet. Pause on the line you want to correct."))
		return nil
	}
	if err != nil {
		return err
	}
	return s.renderReport(w, r, track, cue, reportNote{})
}

// reportNote is the message shown at the top of the report dialog.
type reportNote struct {
	msg  string
	kind string // "success" or "error"
}

func (s *Server) renderReport(w http.ResponseWriter, r *http.Request, track store.Subtitle, cue store.Cue, note reportNote) error {
	changes, err := s.Store.Changes(r.Context(), cue.ID, viewer(r).ID())
	if err != nil {
		return err
	}
	s.render(w, r, http.StatusOK, subtitleReport(viewer(r).CSRF, track, cue, changes, note.msg, note.kind))
	return nil
}

// proposeChange handles POST /cues/{id}/changes: a viewer's correction of a
// subtitle line, which counts as their vote for it.
func (s *Server) proposeChange(w http.ResponseWriter, r *http.Request) error {
	cueID, err := pathID(r)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" || len(text) > 500 {
		return badRequest("A correction must be between 1 and 500 characters.")
	}
	if err := s.visibleCue(r.Context(), viewer(r), cueID); err != nil {
		return err
	}
	applied, err := s.Store.ProposeChange(r.Context(), cueID, viewer(r).ID(), text, s.Cfg.VotesRequired)
	note, err := voteOutcome(applied, err, "Correction submitted. It is applied once enough people vote for it.")
	if err != nil {
		return err
	}
	return s.showVoteResult(w, r, cueID, note)
}

// voteChange handles POST /changes/{id}/vote: a vote for someone's correction.
func (s *Server) voteChange(w http.ResponseWriter, r *http.Request) error {
	changeID, err := pathID(r)
	if err != nil {
		return err
	}
	cueID, err := s.Store.CueForChange(r.Context(), changeID)
	if errors.Is(err, store.ErrNotFound) {
		return &statusError{status: http.StatusNotFound, title: "Not found",
			msg: "That correction no longer exists. It may have just been applied."}
	}
	if err != nil {
		return err
	}
	if err := s.visibleCue(r.Context(), viewer(r), cueID); err != nil {
		return err
	}
	applied, err := s.Store.VoteChange(r.Context(), changeID, viewer(r).ID(), s.Cfg.VotesRequired)
	note, err := voteOutcome(applied, err, "Vote submitted.")
	if err != nil {
		return err
	}
	return s.showVoteResult(w, r, cueID, note)
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
func (s *Server) showVoteResult(w http.ResponseWriter, r *http.Request, cueID int64, note reportNote) error {
	cue, subID, err := s.Store.CueByID(r.Context(), cueID)
	if err != nil {
		return err
	}
	track, err := s.Store.SubtitleByID(r.Context(), subID)
	if err != nil {
		return err
	}
	if !isHTMX(r) {
		http.Redirect(w, r, "/watch/"+itoa(track.MediaID), http.StatusSeeOther)
		return nil
	}
	return s.renderReport(w, r, track, cue, note)
}

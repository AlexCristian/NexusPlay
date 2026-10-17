package web

import (
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

// cueVisible reports whether the subtitle line's video is visible to the
// viewer, answering 404 (or a server error) itself when it is not.
func (s *Server) cueVisible(w http.ResponseWriter, r *http.Request, cueID int64) bool {
	ctx := r.Context()
	_, subID, err := s.Store.CueByID(ctx, cueID)
	if err == nil {
		var track store.Subtitle
		if track, err = s.Store.SubtitleByID(ctx, subID); err == nil {
			var m store.Media
			if m, err = s.Store.MediaByID(ctx, track.MediaID); err == nil && visibleTo(m, viewer(r)) {
				return true
			}
		}
	}
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.serverError(w, r, err)
		return false
	}
	s.notFound(w, r)
	return false
}

// loadMedia fetches the media named in the path. Unpublished media is only
// visible to administrators, who preview it before publishing.
func (s *Server) loadMedia(w http.ResponseWriter, r *http.Request) (store.Media, bool) {
	id, ok := pathID(r)
	if !ok {
		s.notFound(w, r)
		return store.Media{}, false
	}
	m, err := s.Store.MediaByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) || (err == nil && !visibleTo(m, viewer(r))) {
		s.notFound(w, r)
		return m, false
	}
	if err != nil {
		s.serverError(w, r, err)
		return m, false
	}
	return m, true
}

func (s *Server) watch(w http.ResponseWriter, r *http.Request) {
	m, ok := s.loadMedia(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	subs, err := s.Store.Subtitles(ctx, m.ID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	v := viewer(r)
	var resumeAt float64
	if v.User != nil {
		if resumeAt, err = s.Store.Progress(ctx, v.ID(), m.ID); err != nil {
			s.serverError(w, r, err)
			return
		}
		if err := s.Store.ClearNotification(ctx, v.ID(), m.ID); err != nil {
			s.Log.Warn("clearing notification", "err", err)
		}
	}
	if m.Published {
		if err := s.Store.IncrementViews(ctx, m.ID); err != nil {
			s.Log.Warn("counting view", "err", err)
		}
	}
	pb := Playback{Direct: media.Deliver(formatOf(m)) == media.DirectPlay}
	if pb.Direct {
		pb.URL = "/media/" + itoa(m.ID) + "/file"
	} else {
		pb.URL = "/media/" + itoa(m.ID) + "/hls/index.m3u8"
	}
	title := m.Title
	if m.CollectionName != "" {
		title = m.CollectionName + " · " + m.Title
	}
	s.render(w, r, http.StatusOK, watchPage(s.page(r, title), m, pb, subs, resumeAt))
}

// formatOf describes a media item for the playback policy.
func formatOf(m store.Media) media.Format {
	return media.Format{Path: m.Path, VideoCodec: m.VideoCodec, AudioCodec: m.AudioCodec, PixFmt: m.PixFmt}
}

// mediaPath resolves a media item's file, refusing anything outside the media folder.
func (s *Server) mediaPath(m store.Media) (string, error) {
	p := filepath.Join(s.Cfg.MediaDir, filepath.FromSlash(m.Path))
	rel, err := filepath.Rel(s.Cfg.MediaDir, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("media path escapes the media folder")
	}
	return p, nil
}

// mediaFile streams the original file with range-request support.
func (s *Server) mediaFile(w http.ResponseWriter, r *http.Request) {
	m, ok := s.loadMedia(w, r)
	if !ok {
		return
	}
	p, err := s.mediaPath(m)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	f, err := os.Open(p)
	if err != nil {
		s.notFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if ct, ok := media.MIMEType(m.Path); ok {
		w.Header().Set("Content-Type", ct)
	}
	http.ServeContent(w, r, "", info.ModTime(), f)
}

// hls serves an on-demand transcode for files browsers cannot play
// directly: GET /media/{id}/hls/index.m3u8 starts (or reuses) the transcode
// and returns its playlist; GET /media/{id}/hls/segNNNNN.ts returns a segment.
// A segment request for a transcode that was evicted restarts it.
func (s *Server) hls(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if name != "index.m3u8" && !stream.IsSegmentName(name) {
		s.notFound(w, r)
		return
	}
	m, ok := s.loadMedia(w, r)
	if !ok {
		return
	}
	_, running := s.Transcodes.Touch(m.ID)
	var file string
	if name == "index.m3u8" || !running {
		src, err := s.mediaPath(m)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		playlist, err := s.Transcodes.Playlist(r.Context(), m.ID, src, formatOf(m))
		if errors.Is(err, stream.ErrBusy) {
			w.Header().Set("Retry-After", "30")
			http.Error(w, "The server is busy converting other videos. Try again in a minute.", http.StatusServiceUnavailable)
			return
		}
		if err != nil {
			if r.Context().Err() == nil {
				s.Log.Error("transcode", "media", m.ID, "err", err)
				http.Error(w, "This video could not be converted for playback.", http.StatusInternalServerError)
			}
			return
		}
		file = playlist
	}
	if name == "index.m3u8" {
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-cache")
	} else {
		segment, err := s.Transcodes.Segment(r.Context(), m.ID, name)
		if err != nil {
			s.notFound(w, r)
			return
		}
		file = segment
		w.Header().Set("Content-Type", "video/mp2t")
	}
	http.ServeFile(w, r, file)
}

func (s *Server) subtitleVTT(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	ctx := r.Context()
	track, err := s.Store.SubtitleByID(ctx, id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	m, err := s.Store.MediaByID(ctx, track.MediaID)
	if err != nil || !visibleTo(m, viewer(r)) {
		s.notFound(w, r)
		return
	}
	cues, err := s.Store.Cues(ctx, id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	out := make([]subtitle.Cue, len(cues))
	for i, c := range cues {
		out[i] = subtitle.Cue{StartMS: c.StartMS, EndMS: c.EndMS, Text: c.Text}
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(subtitle.WriteVTT(out))
}

func (s *Server) poster(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "" || filepath.Base(name) != name || strings.HasPrefix(name, ".") {
		s.notFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(s.Cfg.PosterDir(), name))
}

// saveProgress is called by the player (often through navigator.sendBeacon).
func (s *Server) saveProgress(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.FormValue("media_id"), 10, 64)
	if err != nil {
		http.Error(w, "bad media_id", http.StatusBadRequest)
		return
	}
	pos, err := strconv.ParseFloat(r.FormValue("position"), 64)
	if err != nil || pos < 0 || math.IsNaN(pos) || math.IsInf(pos, 0) {
		http.Error(w, "bad position", http.StatusBadRequest)
		return
	}
	m, err := s.Store.MediaByID(r.Context(), id)
	if err != nil || !visibleTo(m, viewer(r)) {
		http.Error(w, "unknown media", http.StatusNotFound)
		return
	}
	finished := r.FormValue("finished") == "1" || (m.Duration > 0 && pos >= m.Duration*0.95)
	if err := s.Store.SaveProgress(r.Context(), viewer(r).ID(), id, pos, finished); err != nil {
		s.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// subtitleReport opens the "Report a subtitle error" dialog for the line on
// screen at time t (milliseconds) in the given track.
func (s *Server) subtitleReport(w http.ResponseWriter, r *http.Request) {
	m, ok := s.loadMedia(w, r)
	if !ok {
		return
	}
	trackID, err := strconv.ParseInt(r.URL.Query().Get("track"), 10, 64)
	if err != nil {
		s.errorPage(w, r, http.StatusBadRequest, "Bad request", "Choose a subtitle track first.")
		return
	}
	ms, _ := strconv.ParseInt(r.URL.Query().Get("t"), 10, 64) // missing or malformed: the start of the video
	track, err := s.Store.SubtitleByID(r.Context(), trackID)
	if err != nil || track.MediaID != m.ID {
		s.notFound(w, r)
		return
	}
	cue, err := s.Store.CueAt(r.Context(), trackID, ms)
	if errors.Is(err, store.ErrNotFound) {
		s.render(w, r, http.StatusOK, alert("info", "No subtitle line has been shown yet. Pause on the line you want to correct."))
		return
	} else if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.renderReport(w, r, track, cue, "", "")
}

func (s *Server) renderReport(w http.ResponseWriter, r *http.Request, track store.Subtitle, cue store.Cue, msg, kind string) {
	changes, err := s.Store.Changes(r.Context(), cue.ID, viewer(r).ID())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, subtitleReport(viewer(r).CSRF, track, cue, changes, msg, kind))
}

func (s *Server) proposeChange(w http.ResponseWriter, r *http.Request) {
	cueID, ok := pathID(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" || len(text) > 500 {
		s.errorPage(w, r, http.StatusBadRequest, "Bad request", "A correction must be between 1 and 500 characters.")
		return
	}
	if !s.cueVisible(w, r, cueID) {
		return
	}
	applied, err := s.Store.ProposeChange(r.Context(), cueID, viewer(r).ID(), text, s.Cfg.VotesRequired)
	s.afterVote(w, r, cueID, applied, err, "Correction submitted. It is applied once enough people vote for it.")
}

func (s *Server) voteChange(w http.ResponseWriter, r *http.Request) {
	changeID, ok := pathID(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	cueID, err := s.Store.CueForChange(r.Context(), changeID)
	if err != nil {
		s.errorPage(w, r, http.StatusNotFound, "Not found", "That correction no longer exists. It may have just been applied.")
		return
	}
	if !s.cueVisible(w, r, cueID) {
		return
	}
	applied, err := s.Store.VoteChange(r.Context(), changeID, viewer(r).ID(), s.Cfg.VotesRequired)
	s.afterVote(w, r, cueID, applied, err, "Vote submitted.")
}

func (s *Server) afterVote(w http.ResponseWriter, r *http.Request, cueID int64, applied bool, err error, okMsg string) {
	msg, kind := okMsg, "success"
	switch {
	case errors.Is(err, store.ErrAlreadyVoted), errors.Is(err, store.ErrUnchanged):
		msg, kind = err.Error()+".", "error"
	case errors.Is(err, store.ErrNotFound):
		s.notFound(w, r)
		return
	case err != nil:
		s.serverError(w, r, err)
		return
	case applied:
		msg = "Change applied! It shows the next time the video is loaded."
	}
	cue, subID, err := s.Store.CueByID(r.Context(), cueID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	track, err := s.Store.SubtitleByID(r.Context(), subID)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if !isHTMX(r) {
		// A plain form post (scripts not loaded yet): go back to the video.
		http.Redirect(w, r, "/watch/"+itoa(track.MediaID), http.StatusSeeOther)
		return
	}
	s.renderReport(w, r, track, cue, msg, kind)
}

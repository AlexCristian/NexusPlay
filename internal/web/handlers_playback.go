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
)

// playbackHandlers serve videos: the player page, the file itself, HLS
// conversions for files a browser cannot play, posters, and watch progress.
type playbackHandlers struct {
	responder
	store      playbackStore
	transcodes transcoder
	mediaDir   string
	posterDir  string
}

// playbackStore is the part of the database the player uses.
type playbackStore interface {
	mediaGetter
	Subtitles(ctx context.Context, mediaID int64) ([]store.Subtitle, error)
	Progress(ctx context.Context, userID, mediaID int64) (float64, error)
	SaveProgress(ctx context.Context, userID, mediaID int64, position float64, finished bool) error
	ClearNotification(ctx context.Context, userID, mediaID int64) error
	IncrementViews(ctx context.Context, id int64) error
}

// transcoder runs the on-demand HLS conversions (stream.Manager).
type transcoder interface {
	Touch(mediaID int64) (dir string, ok bool)
	Playlist(ctx context.Context, mediaID int64, src string, f media.Format) (string, error)
	Segment(ctx context.Context, mediaID int64, name string) (string, error)
}

// watch handles GET /watch/{id}: the player page.
func (h *playbackHandlers) watch(w http.ResponseWriter, r *http.Request) error {
	m, err := visibleMedia(r, h.store)
	if err != nil {
		return err
	}
	ctx := r.Context()
	subs, err := h.store.Subtitles(ctx, m.ID)
	if err != nil {
		return err
	}
	v := viewer(r)
	var resumeAt float64
	if v.User != nil {
		if resumeAt, err = h.store.Progress(ctx, v.ID(), m.ID); err != nil {
			return err
		}
		// Opening the video dismisses its "new episode" notification.
		if err := h.store.ClearNotification(ctx, v.ID(), m.ID); err != nil {
			h.log.Warn("clearing notification", "err", err)
		}
	}
	if m.Published {
		if err := h.store.IncrementViews(ctx, m.ID); err != nil {
			h.log.Warn("counting view", "err", err)
		}
	}
	title := m.Title
	if m.CollectionName != "" {
		title = m.CollectionName + " · " + m.Title
	}
	h.render(w, r, http.StatusOK, watchPage(newPage(r, title), m, playbackFor(m), subs, resumeAt))
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
func (h *playbackHandlers) mediaPath(m store.Media) (string, error) {
	p := filepath.Join(h.mediaDir, filepath.FromSlash(m.Path))
	rel, err := filepath.Rel(h.mediaDir, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("media path escapes the media folder")
	}
	return p, nil
}

// mediaFile handles GET /media/{id}/file: the original file, with range
// requests so the player can seek.
func (h *playbackHandlers) mediaFile(w http.ResponseWriter, r *http.Request) error {
	m, err := visibleMedia(r, h.store)
	if err != nil {
		return err
	}
	p, err := h.mediaPath(m)
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

// HLS: files browsers cannot play directly are converted on demand, as a
// growing HLS stream. At the transcode limit the answer is 503 with
// Retry-After, which player.js shows.

// hlsPlaylist handles GET /media/{id}/hls/index.m3u8: it starts the
// transcode, or reuses a running one, and returns its playlist.
func (h *playbackHandlers) hlsPlaylist(w http.ResponseWriter, r *http.Request) error {
	m, err := visibleMedia(r, h.store)
	if err != nil {
		return err
	}
	playlist, err := h.startTranscode(r.Context(), m)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, playlist)
	return nil
}

// hlsSegment handles GET /media/{id}/hls/{segment}, such as seg00012.ts,
// waiting for the segment if it is not written yet. A request for a
// transcode that was stopped for being idle starts it again.
func (h *playbackHandlers) hlsSegment(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("segment")
	if !stream.IsSegmentName(name) {
		return store.ErrNotFound
	}
	m, err := visibleMedia(r, h.store)
	if err != nil {
		return err
	}
	if _, running := h.transcodes.Touch(m.ID); !running {
		if _, err := h.startTranscode(r.Context(), m); err != nil {
			return err
		}
	}
	segment, err := h.transcodes.Segment(r.Context(), m.ID, name)
	if err != nil {
		return store.ErrNotFound
	}
	w.Header().Set("Content-Type", "video/mp2t")
	http.ServeFile(w, r, segment)
	return nil
}

// startTranscode starts or reuses the HLS transcode of m and returns its
// playlist. A failed transcode is logged here, with the media it was for.
func (h *playbackHandlers) startTranscode(ctx context.Context, m store.Media) (string, error) {
	src, err := h.mediaPath(m)
	if err != nil {
		return "", err
	}
	playlist, err := h.transcodes.Playlist(ctx, m.ID, src, formatOf(m))
	if err != nil && !errors.Is(err, stream.ErrBusy) && ctx.Err() == nil {
		h.log.Error("transcode failed", "media", m.ID, "err", err)
		return "", &statusError{status: http.StatusInternalServerError, title: "Playback failed",
			msg: "This video could not be converted for playback."}
	}
	return playlist, err
}

// poster handles GET /posters/{name}. Only plain file names in the poster
// folder are served.
func (h *playbackHandlers) poster(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	if name == "" || filepath.Base(name) != name || strings.HasPrefix(name, ".") {
		return store.ErrNotFound
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(h.posterDir, name))
	return nil
}

// saveProgress handles POST /me/progress from player.js, usually through
// navigator.sendBeacon. Two rules mark a video watched, on purpose: the
// player sends finished=1 when it ends, and the server also counts 95% as
// finished, because a tab closed during the credits never sends "ended".
func (h *playbackHandlers) saveProgress(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(r.FormValue("media_id"), 10, 64)
	if err != nil {
		return badRequest("The media_id is not a number.")
	}
	pos, err := strconv.ParseFloat(r.FormValue("position"), 64)
	if err != nil || pos < 0 || math.IsNaN(pos) || math.IsInf(pos, 0) {
		return badRequest("The position must be a number of seconds.")
	}
	v := viewer(r)
	m, err := visibleMediaByID(r.Context(), h.store, v, id)
	if err != nil {
		return err
	}
	finished := r.FormValue("finished") == "1" || (m.Duration > 0 && pos >= m.Duration*0.95)
	if err := h.store.SaveProgress(r.Context(), v.ID(), id, pos, finished); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

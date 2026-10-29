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
	store           playbackStore
	transcodes      transcoder
	mediaDirectory  string
	posterDirectory string
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
	Touch(mediaID int64) (directory string, found bool)
	Playlist(ctx context.Context, mediaID int64, source string, format media.Format) (string, error)
	Segment(ctx context.Context, mediaID int64, name string) (string, error)
}

// watch handles GET /watch/{id}: the player page.
func (handlers *playbackHandlers) watch(w http.ResponseWriter, r *http.Request) error {
	media, err := visibleMedia(r, handlers.store)
	if err != nil {
		return err
	}
	ctx := r.Context()
	subs, err := handlers.store.Subtitles(ctx, media.ID)
	if err != nil {
		return err
	}
	currentViewer := viewer(r)
	var resumeAt float64
	if currentViewer.User != nil {
		if resumeAt, err = handlers.store.Progress(ctx, currentViewer.ID(), media.ID); err != nil {
			return err
		}
		// Opening the video dismisses its "new episode" notification.
		if err := handlers.store.ClearNotification(ctx, currentViewer.ID(), media.ID); err != nil {
			handlers.log.Warn("clearing notification", "err", err)
		}
	}
	if media.Published {
		if err := handlers.store.IncrementViews(ctx, media.ID); err != nil {
			handlers.log.Warn("counting view", "err", err)
		}
	}
	title := media.Title
	if media.CollectionName != "" {
		title = media.CollectionName + " · " + media.Title
	}
	handlers.render(w, r, http.StatusOK, watchPage(newPage(r, title), media, playbackFor(media), subs, resumeAt))
	return nil
}

// playbackFor tells the player where to load a video from: the file itself,
// or an on-the-fly HLS conversion.
func playbackFor(mediaItem store.Media) Playback {
	if media.Deliver(formatOf(mediaItem)) == media.DirectPlay {
		return Playback{Direct: true, URL: "/media/" + itoa(mediaItem.ID) + "/file"}
	}
	return Playback{URL: "/media/" + itoa(mediaItem.ID) + "/hls/index.m3u8"}
}

// formatOf describes a media item for the playback policy.
func formatOf(mediaItem store.Media) media.Format {
	return media.Format{Path: mediaItem.Path, VideoCodec: mediaItem.VideoCodec, AudioCodec: mediaItem.AudioCodec, PixelFormat: mediaItem.PixelFormat}
}

// mediaPath resolves a media item's file, refusing anything outside the media
// folder. (Paths come from the scanner, so this is defence in depth.)
func (handlers *playbackHandlers) mediaPath(media store.Media) (string, error) {
	path := filepath.Join(handlers.mediaDirectory, filepath.FromSlash(media.Path))
	relativePath, err := filepath.Rel(handlers.mediaDirectory, path)
	if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", errors.New("media path escapes the media folder")
	}
	return path, nil
}

// mediaFile handles GET /media/{id}/file: the original file, with range
// requests so the player can seek.
func (handlers *playbackHandlers) mediaFile(w http.ResponseWriter, r *http.Request) error {
	mediaItem, err := visibleMedia(r, handlers.store)
	if err != nil {
		return err
	}
	path, err := handlers.mediaPath(mediaItem)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return store.ErrNotFound // deleted since the last scan
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if contentType, known := media.MIMEType(mediaItem.Path); known {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, "", info.ModTime(), file)
	return nil
}

// HLS: files browsers cannot play directly are converted on demand, as a
// growing HLS stream. At the transcode limit the answer is 503 with
// Retry-After, which player.js shows.

// hlsPlaylist handles GET /media/{id}/hls/index.m3u8: it starts the
// transcode, or reuses a running one, and returns its playlist.
func (handlers *playbackHandlers) hlsPlaylist(w http.ResponseWriter, r *http.Request) error {
	media, err := visibleMedia(r, handlers.store)
	if err != nil {
		return err
	}
	playlist, err := handlers.startTranscode(r.Context(), media)
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
func (handlers *playbackHandlers) hlsSegment(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("segment")
	if !stream.IsSegmentName(name) {
		return store.ErrNotFound
	}
	media, err := visibleMedia(r, handlers.store)
	if err != nil {
		return err
	}
	if _, running := handlers.transcodes.Touch(media.ID); !running {
		if _, err := handlers.startTranscode(r.Context(), media); err != nil {
			return err
		}
	}
	segment, err := handlers.transcodes.Segment(r.Context(), media.ID, name)
	if err != nil {
		return store.ErrNotFound
	}
	w.Header().Set("Content-Type", "video/mp2t")
	http.ServeFile(w, r, segment)
	return nil
}

// startTranscode starts or reuses the HLS transcode of m and returns its
// playlist. A failed transcode is logged here, with the media it was for.
func (handlers *playbackHandlers) startTranscode(ctx context.Context, media store.Media) (string, error) {
	source, err := handlers.mediaPath(media)
	if err != nil {
		return "", err
	}
	playlist, err := handlers.transcodes.Playlist(ctx, media.ID, source, formatOf(media))
	if err != nil && !errors.Is(err, stream.ErrBusy) && ctx.Err() == nil {
		handlers.log.Error("transcode failed", "media", media.ID, "err", err)
		return "", &statusError{status: http.StatusInternalServerError, title: "Playback failed",
			message: "This video could not be converted for playback."}
	}
	return playlist, err
}

// poster handles GET /posters/{name}. Only plain file names in the poster
// folder are served.
func (handlers *playbackHandlers) poster(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	if name == "" || filepath.Base(name) != name || strings.HasPrefix(name, ".") {
		return store.ErrNotFound
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(handlers.posterDirectory, name))
	return nil
}

// saveProgress handles POST /me/progress from player.js, usually through
// navigator.sendBeacon. Two rules mark a video watched, on purpose: the
// player sends finished=1 when it ends, and the server also counts 95% as
// finished, because a tab closed during the credits never sends "ended".
func (handlers *playbackHandlers) saveProgress(w http.ResponseWriter, r *http.Request) error {
	id, err := strconv.ParseInt(r.FormValue("media_id"), 10, 64)
	if err != nil {
		return badRequest("The media_id is not a number.")
	}
	position, err := strconv.ParseFloat(r.FormValue("position"), 64)
	if err != nil || position < 0 || math.IsNaN(position) || math.IsInf(position, 0) {
		return badRequest("The position must be a number of seconds.")
	}
	currentViewer := viewer(r)
	media, err := visibleMediaByID(r.Context(), handlers.store, currentViewer, id)
	if err != nil {
		return err
	}
	finished := r.FormValue("finished") == "1" || (media.Duration > 0 && position >= media.Duration*0.95)
	if err := handlers.store.SaveProgress(r.Context(), currentViewer.ID(), id, position, finished); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

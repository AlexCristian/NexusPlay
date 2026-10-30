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

// [playbackHandlers] serve videos: the player page, the file itself, HLS
// transcodes for files a browser cannot play, posters, and watch progress.
type playbackHandlers struct {
	// [responder] writes the responses.
	responder
	// [store] loads media and subtitles and saves progress.
	store playbackStore
	// [transcodes] runs the HLS transcodes; normally the [stream.Manager].
	transcodes transcoder
	// [mediaDirectory] is the media folder.
	mediaDirectory string
	// [posterDirectory] is the poster folder.
	posterDirectory string
}

// [playbackStore] is the part of the database the player uses;
// [store.Store] implements it.
type playbackStore interface {
	// [mediaGetter] loads media items.
	mediaGetter
	// [Subtitles] lists a media item's subtitle tracks.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [mediaID]: the media item.
	//
	// Returns:
	//   - [[]store.Subtitle]: the tracks, by label.
	//   - [error]: [nil] on success, or the database error.
	Subtitles(ctx context.Context, mediaID int64) ([]store.Subtitle, error)
	// [Progress] gets where a user stopped in a media item.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [userID]: the viewer.
	//   - [mediaID]: the media item.
	//
	// Returns:
	//   - [float64]: the position in seconds, or 0 if none is saved.
	//   - [error]: [nil] on success, or the database error.
	Progress(ctx context.Context, userID, mediaID int64) (float64, error)
	// [SaveProgress] records where a user is in a media item, or marks it
	// watched.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [userID]: the viewer.
	//   - [mediaID]: the media item.
	//   - [position]: seconds from the start.
	//   - [finished]: [true] to mark the item watched instead.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	SaveProgress(ctx context.Context, userID, mediaID int64, position float64, finished bool) error
	// [ClearNotification] dismisses a user's notification about a media item.
	//
	// Parameters:
	//   - [ctx]: cancels the delete.
	//   - [userID]: the viewer.
	//   - [mediaID]: the media item.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	ClearNotification(ctx context.Context, userID, mediaID int64) error
	// [IncrementViews] counts one more play of a media item.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [id]: the media item.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	IncrementViews(ctx context.Context, id int64) error
}

// [transcoder] runs the on-demand HLS transcodes; [stream.Manager]
// implements it.
type transcoder interface {
	// [Touch] keeps a media item's transcode alive, if it has one.
	//
	// Parameters:
	//   - [mediaID]: the media item.
	//
	// Returns:
	//   - [string] ([directory]): the transcode's folder, or "".
	//   - [bool] ([found]): [true] if the item has a transcode.
	Touch(mediaID int64) (directory string, found bool)
	// [Playlist] starts or reuses a media item's transcode and waits for its
	// first segment.
	//
	// Parameters:
	//   - [ctx]: stops the wait when cancelled.
	//   - [mediaID]: the media item.
	//   - [source]: the original file's path.
	//   - [format]: the original's codecs.
	//
	// Returns:
	//   - [string]: the playlist file's path.
	//   - [error]: as [stream.Manager.Playlist] describes, including
	//     [stream.ErrBusy].
	Playlist(ctx context.Context, mediaID int64, source string, format media.Format) (string, error)
	// [Segment] finds one segment of a media item's transcode, waiting for it
	// to be written.
	//
	// Parameters:
	//   - [ctx]: stops the wait when cancelled.
	//   - [mediaID]: the media item.
	//   - [name]: the segment's file name.
	//
	// Returns:
	//   - [string]: the segment file's path.
	//   - [error]: as [stream.Manager.Segment] describes.
	Segment(ctx context.Context, mediaID int64, name string) (string, error)
}

// [watch] handles GET /watch/{id}: the player page, resuming where the
// viewer stopped. For a signed-in viewer it also dismisses the item's "new
// episode" notification, and for a published item it counts a view; either
// failing is only logged.
//
// Parameters:
//   - [w]: where the page is written.
//   - [r]: the request; {id} in its path names the media item.
//
// Returns:
//   - [error]: [nil] once the page is written; [store.ErrNotFound] if the
//     item does not exist or the viewer may not see it; otherwise the
//     database error.
func (handlers *playbackHandlers) watch(w http.ResponseWriter, r *http.Request) error {
	media, err := visibleMedia(r, handlers.store)
	if err != nil {
		return err
	}
	ctx := r.Context()
	subtitles, err := handlers.store.Subtitles(ctx, media.ID)
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
	handlers.render(w, r, http.StatusOK, watchPage(newPage(r, title), media, playbackFor(media), subtitles, resumeAt))
	return nil
}

// [playbackFor] tells the player where to load a video from: the file
// itself if browsers play it (see [media.Deliver]), or else an on-the-fly
// HLS transcode.
//
// Parameters:
//   - [mediaItem]: the media item.
//
// Returns:
//   - [Playback]: "/media/{id}/file" with [Playback.Direct] set, or
//     "/media/{id}/hls/index.m3u8".
func playbackFor(mediaItem store.Media) Playback {
	if media.Deliver(formatOf(mediaItem)) == media.DirectPlay {
		return Playback{Direct: true, URL: "/media/" + formatInteger(mediaItem.ID) + "/file"}
	}
	return Playback{URL: "/media/" + formatInteger(mediaItem.ID) + "/hls/index.m3u8"}
}

// [formatOf] describes a media item for the playback policy.
//
// Parameters:
//   - [mediaItem]: the media item.
//
// Returns:
//   - [media.Format]: its path and codecs.
func formatOf(mediaItem store.Media) media.Format {
	return media.Format{Path: mediaItem.Path, VideoCodec: mediaItem.VideoCodec, AudioCodec: mediaItem.AudioCodec, PixelFormat: mediaItem.PixelFormat}
}

// [mediaPath] works out a media item's file path, refusing anything outside
// the media folder. Paths come from the scanner, so this is defence in
// depth.
//
// Parameters:
//   - [media]: the media item.
//
// Returns:
//   - [string]: the file's full path, or "" if [error] is not [nil].
//   - [error]: [nil] on success, or an error if the path would leave the
//     media folder.
func (handlers *playbackHandlers) mediaPath(media store.Media) (string, error) {
	path := filepath.Join(handlers.mediaDirectory, filepath.FromSlash(media.Path))
	relativePath, err := filepath.Rel(handlers.mediaDirectory, path)
	if err != nil || relativePath == ".." || strings.HasPrefix(relativePath, ".."+string(filepath.Separator)) {
		return "", errors.New("media path escapes the media folder")
	}
	return path, nil
}

// [mediaFile] handles GET /media/{id}/file: the original file, with range
// requests so the player can seek.
//
// Parameters:
//   - [w]: where the file is written.
//   - [r]: the request; {id} in its path names the media item.
//
// Returns:
//   - [error]: [nil] once the file is served; [store.ErrNotFound] if the
//     item does not exist, the viewer may not see it, or its file was
//     deleted since the last scan; otherwise a path or file error.
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

// [hlsPlaylist] handles GET /media/{id}/hls/index.m3u8 for files browsers
// cannot play directly: it starts the item's transcode, or reuses a running
// one, and serves its playlist, which grows as the transcode runs. At the
// transcode limit the answer is 503 with Retry-After, which player.js shows.
//
// Parameters:
//   - [w]: where the playlist is written.
//   - [r]: the request; {id} in its path names the media item.
//
// Returns:
//   - [error]: [nil] once the playlist is served; [store.ErrNotFound] if the
//     item does not exist or the viewer may not see it; otherwise as
//     [playbackHandlers.startTranscode] describes.
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

// [hlsSegment] handles GET /media/{id}/hls/{segment}, such as
// "seg00012.ts", waiting for the segment if it is not written yet. A request
// for a transcode that was stopped for being idle starts it again.
//
// Parameters:
//   - [w]: where the segment is written.
//   - [r]: the request; {id} names the media item and {segment} the file.
//
// Returns:
//   - [error]: [nil] once the segment is served; [store.ErrNotFound] if
//     {segment} is not a segment name, the item does not exist or is hidden
//     from the viewer, or the segment does not appear; otherwise as
//     [playbackHandlers.startTranscode] describes.
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

// [startTranscode] starts or reuses a media item's HLS transcode. A failed
// transcode is logged here, with the item it was for.
//
// Parameters:
//   - [ctx]: stops the wait for the first segment when cancelled.
//   - [media]: the media item.
//
// Returns:
//   - [string]: the playlist file's path, or "" if [error] is not [nil].
//   - [error]: [nil] on success; [stream.ErrBusy] at the transcode limit;
//     [ctx]'s error if it ended; a [*statusError] with status 500 and
//     "Playback failed" if the transcode failed; or an error if the item's
//     path leaves the media folder.
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

// [poster] handles GET /posters/{name}: a poster image, which browsers may
// keep for a day. Only plain file names in the poster folder are served;
// anything with a folder or starting with "." is refused.
//
// Parameters:
//   - [w]: where the image is written; a missing file gets a 404.
//   - [r]: the request; {name} in its path is the file name.
//
// Returns:
//   - [error]: [nil] once answered, or [store.ErrNotFound] for a refused
//     name.
func (handlers *playbackHandlers) poster(w http.ResponseWriter, r *http.Request) error {
	name := r.PathValue("name")
	if name == "" || filepath.Base(name) != name || strings.HasPrefix(name, ".") {
		return store.ErrNotFound
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	http.ServeFile(w, r, filepath.Join(handlers.posterDirectory, name))
	return nil
}

// [saveProgress] handles POST /me/progress from player.js, usually sent with
// navigator.sendBeacon: the form fields "media_id", "position" (seconds)
// and "finished" ("1" when the video ended). Two rules mark a video watched,
// on purpose: the player sends finished=1 when it ends, and the server also
// counts 95% as finished, because a tab closed during the credits never
// sends "ended". It answers 204 No Content.
//
// Parameters:
//   - [w]: where the empty response is written.
//   - [r]: the request from a signed-in viewer.
//
// Returns:
//   - [error]: [nil] on success; a 400 [*statusError] if "media_id" or
//     "position" is not a valid number; [store.ErrNotFound] if the item does
//     not exist or the viewer may not see it; otherwise the database error.
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

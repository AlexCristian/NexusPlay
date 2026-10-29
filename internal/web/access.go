package web

import (
	"context"
	"net/http"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// visibleTo reports whether a viewer may see a media item. Unpublished media
// is visible only to administrators, who preview it before publishing; to
// everyone else it does not exist (404, not 403).
func visibleTo(media store.Media, viewer Viewer) bool {
	return media.Published || viewer.IsAdmin()
}

// mediaGetter is how the visibility checks load media.
type mediaGetter interface {
	MediaByID(ctx context.Context, id int64) (store.Media, error)
}

// visibleMedia loads the media named by {id} in the path. It returns
// store.ErrNotFound when the media does not exist or is hidden from the viewer.
func visibleMedia(r *http.Request, media mediaGetter) (store.Media, error) {
	id, err := pathID(r)
	if err != nil {
		return store.Media{}, err
	}
	return visibleMediaByID(r.Context(), media, viewer(r), id)
}

// visibleMediaByID is visibleMedia for an ID from elsewhere in the request.
func visibleMediaByID(ctx context.Context, media mediaGetter, viewer Viewer, id int64) (store.Media, error) {
	mediaItem, err := media.MediaByID(ctx, id)
	if err != nil {
		return store.Media{}, err
	}
	if !visibleTo(mediaItem, viewer) {
		return store.Media{}, store.ErrNotFound
	}
	return mediaItem, nil
}

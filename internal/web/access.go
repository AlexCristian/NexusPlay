package web

import (
	"context"
	"net/http"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [visibleTo] reports whether a viewer may see a media item. Unpublished
// media are visible only to administrators, who preview them before
// publishing; to everyone else they do not exist (404, not 403).
//
// Parameters:
//   - [media]: the media item.
//   - [viewer]: who is asking.
//
// Returns:
//   - [bool]: [true] if the item is published or the viewer is an
//     administrator, [false] otherwise.
func visibleTo(media store.Media, viewer Viewer) bool {
	return media.Published || viewer.IsAdmin()
}

// [mediaGetter] is how the visibility checks load media; [store.Store]
// implements it.
type mediaGetter interface {
	// [MediaByID] loads one media item, published or not.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [id]: the media item.
	//
	// Returns:
	//   - [store.Media]: the media item.
	//   - [error]: [nil] on success, [store.ErrNotFound] if it does not exist,
	//     or another database error.
	MediaByID(ctx context.Context, id int64) (store.Media, error)
}

// [visibleMedia] loads the media item named by {id} in the request's path,
// if the viewer may see it.
//
// Parameters:
//   - [r]: the request, whose route has an {id} wildcard.
//   - [media]: where to load the item from.
//
// Returns:
//   - [store.Media]: the media item, or a zero [store.Media] if [error] is
//     not [nil].
//   - [error]: [nil] on success; [store.ErrNotFound] when {id} is not a
//     valid ID, the item does not exist, or it is hidden from the viewer;
//     otherwise the database error.
func visibleMedia(r *http.Request, media mediaGetter) (store.Media, error) {
	id, err := pathID(r)
	if err != nil {
		return store.Media{}, err
	}
	return visibleMediaByID(r.Context(), media, viewer(r), id)
}

// [visibleMediaByID] is [visibleMedia] for an ID taken from somewhere other
// than the path, such as a form field.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [media]: where to load the item from.
//   - [viewer]: who is asking.
//   - [id]: the media item.
//
// Returns:
//   - [store.Media]: the media item, or a zero [store.Media] if [error] is
//     not [nil].
//   - [error]: [nil] on success; [store.ErrNotFound] when the item does not
//     exist or is hidden from [viewer], so a visitor cannot tell hidden media
//     from missing media; otherwise the database error, unchanged.
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

package web

import (
	"context"
	_ "embed"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/auth"
	"github.com/AlexCristian/NexusPlay/internal/datafile"
	"github.com/AlexCristian/NexusPlay/internal/metadata"
	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

// adminHandlers serve the control panel: publishing and editing media,
// collections and subtitle tracks, scans and the conversion queue.
type adminHandlers struct {
	responder
	store           adminStore
	metadata        metadataLookup
	refresher       metadataRefresher
	scanner         scanTrigger
	conversions     Conversions
	posterDirectory string
}

// adminStore is the part of the database the control panel edits.
type adminStore interface {
	mediaGetter
	PendingMedia(ctx context.Context) ([]store.Media, error)
	UpdateMedia(ctx context.Context, id int64, update store.MediaUpdate, publish bool) error
	Collections(ctx context.Context) ([]store.Collection, error)
	CategoryNames(ctx context.Context) ([]string, error)
	CollectionByID(ctx context.Context, id int64) (store.Collection, error)
	CollectionByName(ctx context.Context, name string) (store.Collection, error)
	CreateCollection(ctx context.Context, collection store.Collection) (id int64, err error)
	UpdateCollection(ctx context.Context, id int64, update store.CollectionUpdate) error
	DeleteCollection(ctx context.Context, id int64) error
	Subtitles(ctx context.Context, mediaID int64) ([]store.Subtitle, error)
	SubtitleByID(ctx context.Context, id int64) (store.Subtitle, error)
	AddSubtitle(ctx context.Context, subtitle store.NewSubtitle) (id int64, inserted bool, err error)
	UpdateSubtitleLabel(ctx context.Context, id int64, language, label string) error
	DeleteSubtitle(ctx context.Context, id int64) error
	ConversionFailures(ctx context.Context) ([]store.ConversionFailure, error)
	RetryConversion(ctx context.Context, id int64) error
}

// metadataLookup finds show details on TMDB (metadata.Client).
type metadataLookup interface {
	Enabled() bool
	Show(ctx context.Context, name, year string) (metadata.Info, error)
	Movie(ctx context.Context, title, year string) (metadata.Info, error)
	Collection(ctx context.Context, name string) (metadata.Info, error)
}

// metadataRefresher looks one media item up again (library.Scanner).
type metadataRefresher interface {
	RefreshMetadata(ctx context.Context, id int64) error
}

// scanTrigger starts a library scan (library.Scanner).
type scanTrigger interface {
	Trigger()
}

// mediaFromPath loads the media named by {id}. Administrators see
// unpublished media too, so there is no visibility check.
func (handlers *adminHandlers) mediaFromPath(r *http.Request) (store.Media, error) {
	id, err := pathID(r)
	if err != nil {
		return store.Media{}, err
	}
	return handlers.store.MediaByID(r.Context(), id)
}

// controlPanel handles GET /admin: media waiting to be published, the
// collections, and the conversion queue.
func (handlers *adminHandlers) controlPanel(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	pending, err := handlers.store.PendingMedia(ctx)
	if err != nil {
		return err
	}
	colls, err := handlers.store.Collections(ctx)
	if err != nil {
		return err
	}
	categories, err := handlers.store.CategoryNames(ctx)
	if err != nil {
		return err
	}
	conversions, err := handlers.conversionView(ctx)
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, controlPanel(newPage(r, "Control panel"), pending, colls, categories, handlers.metadata.Enabled(), conversions,
		doneMessages[r.URL.Query().Get("done")]))
	return nil
}

// [doneMessagesFile] is the contents of done_messages.txt, built into the
// program by the compiler. The build fails if the file is missing.
//
//go:embed done_messages.txt
var doneMessagesFile string

// [doneMessages] confirm an action after the redirect back to /admin?done=,
// from done_messages.txt.
var doneMessages = datafile.Table("done_messages.txt", doneMessagesFile)

// conversionView is the converter's status and the recorded failures.
func (handlers *adminHandlers) conversionView(ctx context.Context) (ConversionView, error) {
	var view ConversionView
	var err error
	if view.Status, err = handlers.conversions.Status(ctx); err != nil {
		return view, err
	}
	view.Failures, err = handlers.store.ConversionFailures(ctx)
	return view, err
}

// conversionsPanel handles GET /admin/conversions, polled by the control
// panel while conversions run.
func (handlers *adminHandlers) conversionsPanel(w http.ResponseWriter, r *http.Request) error {
	view, err := handlers.conversionView(r.Context())
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, conversionsSection(viewer(r).CSRF, view))
	return nil
}

// retryConversion handles POST /admin/media/{id}/retry-conversion.
func (handlers *adminHandlers) retryConversion(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := handlers.store.RetryConversion(r.Context(), id); err != nil {
		return err
	}
	handlers.conversions.Wake()
	http.Redirect(w, r, "/admin?done=retry", http.StatusSeeOther)
	return nil
}

// scanNow handles POST /admin/scan.
func (handlers *adminHandlers) scanNow(w http.ResponseWriter, r *http.Request) error {
	handlers.scanner.Trigger()
	http.Redirect(w, r, "/admin?done=scan", http.StatusSeeOther)
	return nil
}

// mediaForm fills the edit form for m. For media not yet published it
// pre-fills the collection and category from the file name.
func (handlers *adminHandlers) mediaForm(ctx context.Context, media store.Media) (MediaForm, error) {
	form := MediaForm{Media: media, CollectionName: media.CollectionName, MetadataEnabled: handlers.metadata.Enabled()}
	var err error
	if form.Collections, err = handlers.store.Collections(ctx); err != nil {
		return form, err
	}
	if form.Categories, err = handlers.store.CategoryNames(ctx); err != nil {
		return form, err
	}
	if form.Subtitles, err = handlers.store.Subtitles(ctx, media.ID); err != nil {
		return form, err
	}
	if !media.Published {
		// Prefill from the file name: an episode's show becomes its collection.
		guess := guessFor(media)
		if guess.IsEpisode() && form.CollectionName == "" {
			form.CollectionName = guess.Show
			for _, collection := range form.Collections {
				if strings.EqualFold(collection.Name, guess.Show) {
					form.CollectionName = collection.Name
				}
			}
		}
		if form.Media.Category == "" && !guess.IsEpisode() {
			form.Media.Category = "Movies"
		}
	}
	return form, nil
}

// editMediaPage handles GET /admin/media/{id}.
func (handlers *adminHandlers) editMediaPage(w http.ResponseWriter, r *http.Request) error {
	media, err := handlers.mediaFromPath(r)
	if err != nil {
		return err
	}
	form, err := handlers.mediaForm(r.Context(), media)
	if err != nil {
		return err
	}
	form.Saved = r.URL.Query().Get("saved") == "1"
	form.Refreshed = r.URL.Query().Get("refreshed") == "1"
	handlers.render(w, r, http.StatusOK, editMediaPage(newPage(r, "Edit "+media.Title), form))
	return nil
}

// showMediaFormError shows the edit page again when err is a *formError,
// and returns any other error unchanged.
func (handlers *adminHandlers) showMediaFormError(w http.ResponseWriter, r *http.Request, media store.Media, err error) error {
	var formErr *formError
	if !errors.As(err, &formErr) {
		return err
	}
	form, ferr := handlers.mediaForm(r.Context(), media)
	if ferr != nil {
		return ferr
	}
	form.Error = formErr.message
	handlers.render(w, r, http.StatusBadRequest, editMediaPage(newPage(r, "Edit "+media.Title), form))
	return nil
}

// saveMedia handles POST /admin/media/{id}: the edit form, whose second
// button also publishes.
func (handlers *adminHandlers) saveMedia(w http.ResponseWriter, r *http.Request) error {
	media, err := handlers.mediaFromPath(r)
	if err != nil {
		return err
	}
	input, err := decodeMediaForm(r)
	if err != nil {
		return handlers.showMediaFormError(w, r, media, err)
	}
	if err := handlers.applyMediaInput(r.Context(), media, input); err != nil {
		return err
	}
	if input.publish {
		http.Redirect(w, r, "/admin?done=published", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/admin/media/"+formatInteger(media.ID)+"?saved=1", http.StatusSeeOther)
	}
	return nil
}

// applyMediaInput saves a decoded edit form: it finds or creates the
// collection, stores the poster, then updates the media.
func (handlers *adminHandlers) applyMediaInput(ctx context.Context, media store.Media, input mediaInput) error {
	update := input.update
	if input.collection != "" {
		collection, err := handlers.findOrCreateCollection(ctx, input.collection, update.Category, showYearGuess(media, input.collection))
		if err != nil {
			return err
		}
		update.CollectionID = collection.ID
	}
	var err error
	if update.Poster, err = handlers.savePoster(input.poster); err != nil {
		return err
	}
	return handlers.store.UpdateMedia(ctx, media.ID, update, input.publish)
}

// showYearGuess is the year a new collection for m starts with: the year in
// the show's folder name ("Star Trek: Deep Space Nine (1993)/..."), if the
// file is an episode of that show. Never the media's own year, which for an
// episode is the year it aired, not the year the show began; without a
// folder year, TMDB fills the year in.
func showYearGuess(media store.Media, collection string) string {
	guess := guessFor(media)
	if guess.IsEpisode() && strings.EqualFold(guess.Show, collection) {
		return guess.Year
	}
	return ""
}

// findOrCreateCollection returns the named collection, creating it (with
// TMDB details when available) if it does not exist yet.
func (handlers *adminHandlers) findOrCreateCollection(ctx context.Context, name, category, year string) (store.Collection, error) {
	collection, err := handlers.store.CollectionByName(ctx, name)
	if err == nil || !errors.Is(err, store.ErrNotFound) {
		return collection, err
	}
	if category == "" {
		category = "TV Shows"
	}
	collection = store.Collection{Name: name, Category: category, Year: year}
	handlers.fillFromTMDB(ctx, &collection)
	if collection.ID, err = handlers.store.CreateCollection(ctx, collection); errors.Is(err, store.ErrDuplicate) {
		return handlers.store.CollectionByName(ctx, name)
	}
	return collection, err
}

// fillCollectionUpdate applies a TMDB lookup to an edit of existing. The
// lookup only fills empty fields: an existing poster is kept unless the
// administrator removed it, in which case the lookup may supply a new one.
func (handlers *adminHandlers) fillCollectionUpdate(ctx context.Context, existing store.Collection, update *store.CollectionUpdate) {
	collection := store.Collection{Name: update.Name, Year: update.Year, Overview: update.Overview, Poster: update.Poster}
	if collection.Poster == "" && !update.RemovePoster {
		collection.Poster = existing.Poster
	}
	handlers.fillFromTMDB(ctx, &collection)
	update.Year, update.Overview = collection.Year, collection.Overview
	if collection.Poster != existing.Poster || update.RemovePoster {
		update.Poster = collection.Poster
		update.RemovePoster = collection.Poster == ""
	}
}

// lookupCollection finds a collection on TMDB. A "Movies" collection is a
// series of films, so it is searched as a TMDB collection and then as a
// film; anything else is searched as a TV show first, then the same way.
func (handlers *adminHandlers) lookupCollection(ctx context.Context, name, category string) (metadata.Info, error) {
	films := func() (metadata.Info, error) {
		info, err := handlers.metadata.Collection(ctx, name)
		if errors.Is(err, metadata.ErrNoMatch) {
			info, err = handlers.metadata.Movie(ctx, name, "")
			info.Year = "" // the first film's year is not the collection's
		}
		return info, err
	}
	if isFilmCategory(category) {
		return films()
	}
	info, err := handlers.metadata.Show(ctx, name, "")
	if errors.Is(err, metadata.ErrNoMatch) {
		return films()
	}
	return info, err
}

// isFilmCategory reports whether a category holds films rather than shows.
func isFilmCategory(category string) bool {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "movies", "movie", "films", "film":
		return true
	}
	return false
}

// fillFromTMDB fills a collection's empty fields from TMDB, when lookups
// are on. A failed lookup is logged and leaves c unchanged.
func (handlers *adminHandlers) fillFromTMDB(ctx context.Context, collection *store.Collection) {
	if !handlers.metadata.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	info, err := handlers.lookupCollection(ctx, collection.Name, collection.Category)
	if err != nil {
		if !errors.Is(err, metadata.ErrNoMatch) {
			handlers.log.Warn("collection lookup failed", "name", collection.Name, "err", err)
		}
		return
	}
	if collection.Year == "" {
		collection.Year = info.Year
	}
	if collection.Overview == "" {
		collection.Overview = info.Overview
	}
	if collection.Poster == "" {
		collection.Poster = info.Poster
	}
}

// savePoster stores an uploaded poster under a new random name and returns
// the name, or "" when p is nil.
func (handlers *adminHandlers) savePoster(upload *posterUpload) (string, error) {
	if upload == nil {
		return "", nil
	}
	file, err := upload.file.Open()
	if err != nil {
		return "", err
	}
	defer file.Close()
	token, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	name := "upload-" + token[:16] + upload.extension
	out, err := os.OpenFile(filepath.Join(handlers.posterDirectory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, file); err != nil {
		out.Close()
		return "", err
	}
	return name, out.Close()
}

// refreshMetadata handles POST /admin/media/{id}/refresh-metadata: look the
// item up on TMDB again and take its title, year, description and poster,
// even if it is published. Problems are shown on the edit page.
func (handlers *adminHandlers) refreshMetadata(w http.ResponseWriter, r *http.Request) error {
	media, err := handlers.mediaFromPath(r)
	if err != nil {
		return err
	}
	err = handlers.refresher.RefreshMetadata(r.Context(), media.ID)
	switch {
	case errors.Is(err, metadata.ErrDisabled):
		err = newFormError("Metadata lookups are off. Set NEXUSPLAY_TMDB_API_KEY to use them.")
	case errors.Is(err, metadata.ErrNoMatch):
		err = newFormError("TMDB has nothing for this item. Check its title, collection, season and episode, save, and try again.")
	case err != nil && !errors.Is(err, store.ErrNotFound) && r.Context().Err() == nil:
		handlers.log.Warn("metadata refresh failed", "media", media.ID, "err", err)
		err = newFormError("The lookup failed: " + err.Error() + ". Try again in a minute.")
	}
	if err != nil {
		return handlers.showMediaFormError(w, r, media, err)
	}
	http.Redirect(w, r, "/admin/media/"+formatInteger(media.ID)+"?refreshed=1", http.StatusSeeOther)
	return nil
}

// uploadSubtitle handles POST /admin/media/{id}/subtitles: an SRT or
// WebVTT file for the video.
func (handlers *adminHandlers) uploadSubtitle(w http.ResponseWriter, r *http.Request) error {
	media, err := handlers.mediaFromPath(r)
	if err != nil {
		return err
	}
	language, cues, err := decodeSubtitleUpload(r)
	if err != nil {
		return handlers.showMediaFormError(w, r, media, err)
	}
	// An upload has no source, so it is always inserted.
	_, _, err = handlers.store.AddSubtitle(r.Context(), store.NewSubtitle{MediaID: media.ID, Language: language, Label: subtitle.LanguageName(language), Cues: cues})
	if err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/media/"+formatInteger(media.ID)+"?saved=1", http.StatusSeeOther)
	return nil
}

// saveSubtitle handles POST /admin/subtitles/{id}: a track's language and label.
func (handlers *adminHandlers) saveSubtitle(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	subtitle, err := handlers.store.SubtitleByID(r.Context(), id)
	if err != nil {
		return err
	}
	language := strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	label := strings.TrimSpace(r.FormValue("label"))
	if language == "" || label == "" {
		return &statusError{status: http.StatusBadRequest, title: "Missing fields",
			message: "A subtitle track needs a language code and a label."}
	}
	if err := handlers.store.UpdateSubtitleLabel(r.Context(), id, language, label); err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/media/"+formatInteger(subtitle.MediaID)+"?saved=1", http.StatusSeeOther)
	return nil
}

// deleteSubtitle handles POST /admin/subtitles/{id}/delete.
func (handlers *adminHandlers) deleteSubtitle(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	subtitle, err := handlers.store.SubtitleByID(r.Context(), id)
	if err != nil {
		return err
	}
	if err := handlers.store.DeleteSubtitle(r.Context(), id); err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/media/"+formatInteger(subtitle.MediaID)+"?saved=1", http.StatusSeeOther)
	return nil
}

// createCollection handles POST /admin/collections, from the control panel.
func (handlers *adminHandlers) createCollection(w http.ResponseWriter, r *http.Request) error {
	const title = "Could not create the collection"
	input, err := decodeCollectionForm(r)
	var formErr *formError
	if errors.As(err, &formErr) {
		return &statusError{status: http.StatusBadRequest, title: title, message: formErr.message}
	}
	if err != nil {
		return err
	}
	update := input.update
	collection := store.Collection{Name: update.Name, Category: update.Category, Year: update.Year, Overview: update.Overview}
	if collection.Poster, err = handlers.savePoster(input.poster); err != nil {
		return err
	}
	if input.lookup {
		handlers.fillFromTMDB(r.Context(), &collection)
	}
	id, err := handlers.store.CreateCollection(r.Context(), collection)
	if errors.Is(err, store.ErrDuplicate) {
		return &statusError{status: http.StatusConflict, title: title, message: "A collection with that name already exists."}
	}
	if err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/collections/"+formatInteger(id), http.StatusSeeOther)
	return nil
}

// editCollectionPage handles GET /admin/collections/{id}.
func (handlers *adminHandlers) editCollectionPage(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	collection, err := handlers.store.CollectionByID(r.Context(), id)
	if err != nil {
		return err
	}
	categories, err := handlers.store.CategoryNames(r.Context())
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, editCollectionPage(newPage(r, "Edit "+collection.Name), collection, categories, handlers.metadata.Enabled(), ""))
	return nil
}

// saveCollection handles POST /admin/collections/{id}.
func (handlers *adminHandlers) saveCollection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	existing, err := handlers.store.CollectionByID(r.Context(), id)
	if err != nil {
		return err
	}
	input, err := decodeCollectionForm(r)
	if err != nil {
		return handlers.showCollectionFormError(w, r, existing, input.update, err)
	}
	update := input.update
	if update.Poster, err = handlers.savePoster(input.poster); err != nil {
		return err
	}
	if input.lookup {
		handlers.fillCollectionUpdate(r.Context(), existing, &update)
	}
	err = handlers.store.UpdateCollection(r.Context(), id, update)
	if errors.Is(err, store.ErrDuplicate) {
		return handlers.showCollectionFormError(w, r, existing, update, newFormError("Another collection already has that name."))
	}
	if err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/collections/"+formatInteger(id), http.StatusSeeOther)
	return nil
}

// showCollectionFormError shows the edit page again, with what was typed,
// when err is a *formError, and returns any other error unchanged.
func (handlers *adminHandlers) showCollectionFormError(w http.ResponseWriter, r *http.Request, collection store.Collection, typed store.CollectionUpdate, err error) error {
	var formErr *formError
	if !errors.As(err, &formErr) {
		return err
	}
	categories, err := handlers.store.CategoryNames(r.Context())
	if err != nil {
		return err
	}
	collection.Name, collection.Category, collection.Year, collection.Overview = typed.Name, typed.Category, typed.Year, typed.Overview
	handlers.render(w, r, http.StatusBadRequest, editCollectionPage(newPage(r, "Edit collection"), collection, categories, handlers.metadata.Enabled(), formErr.message))
	return nil
}

// deleteCollection handles POST /admin/collections/{id}/delete. Its media
// stays in the library, outside any collection.
func (handlers *adminHandlers) deleteCollection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := handlers.store.DeleteCollection(r.Context(), id); err != nil {
		return err
	}
	http.Redirect(w, r, "/admin?done=deleted", http.StatusSeeOther)
	return nil
}

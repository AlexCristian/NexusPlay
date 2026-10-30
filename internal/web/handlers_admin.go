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

// [adminHandlers] serve the control panel: publishing and editing media,
// collections and subtitle tracks, scans and the conversion queue.
type adminHandlers struct {
	// [responder] writes the responses.
	responder
	// [store] reads and edits the library.
	store adminStore
	// [metadata] looks up collection details on TMDB.
	metadata metadataLookup
	// [refresher] looks one media item up again; normally the
	// [library.Scanner].
	refresher metadataRefresher
	// [scanner] starts library scans; normally the [library.Scanner].
	scanner scanTrigger
	// [conversions] is the MP4 converter, or [convert.Disabled].
	conversions Conversions
	// [posterDirectory] is the folder uploaded posters are saved in.
	posterDirectory string
}

// [adminStore] is the part of the database the control panel edits;
// [store.Store] implements it. Each method is described at the
// [store.Store] method of the same name.
type adminStore interface {
	// [mediaGetter] loads media items.
	mediaGetter
	// [PendingMedia] lists the unpublished media.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//
	// Returns:
	//   - [[]store.Media]: the media, by path.
	//   - [error]: [nil] on success, or the database error.
	PendingMedia(ctx context.Context) ([]store.Media, error)
	// [UpdateMedia] saves a media item's fields and optionally publishes it.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [id]: the media item.
	//   - [update]: the new values.
	//   - [publish]: [true] to publish as well.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	UpdateMedia(ctx context.Context, id int64, update store.MediaUpdate, publish bool) error
	// [Collections] lists every collection.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//
	// Returns:
	//   - [[]store.Collection]: the collections, by name.
	//   - [error]: [nil] on success, or the database error.
	Collections(ctx context.Context) ([]store.Collection, error)
	// [CategoryNames] lists the categories in use.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//
	// Returns:
	//   - [[]string]: the names, sorted.
	//   - [error]: [nil] on success, or the database error.
	CategoryNames(ctx context.Context) ([]string, error)
	// [CollectionByID] loads one collection.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [id]: the collection.
	//
	// Returns:
	//   - [store.Collection]: the collection.
	//   - [error]: [nil] on success, [store.ErrNotFound], or another database
	//     error.
	CollectionByID(ctx context.Context, id int64) (store.Collection, error)
	// [CollectionByName] finds a collection by name, regardless of letter case.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [name]: the name.
	//
	// Returns:
	//   - [store.Collection]: the collection.
	//   - [error]: [nil] on success, [store.ErrNotFound], or another database
	//     error.
	CollectionByName(ctx context.Context, name string) (store.Collection, error)
	// [CreateCollection] adds a collection.
	//
	// Parameters:
	//   - [ctx]: cancels the insert.
	//   - [collection]: the new collection.
	//
	// Returns:
	//   - [int64] ([id]): the new collection's ID.
	//   - [error] ([err]): [nil] on success, [store.ErrDuplicate] if the name is
	//     taken, or another database error.
	CreateCollection(ctx context.Context, collection store.Collection) (id int64, err error)
	// [UpdateCollection] saves a collection's fields.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [id]: the collection.
	//   - [update]: the new values.
	//
	// Returns:
	//   - [error]: [nil] on success, [store.ErrDuplicate] if another collection
	//     has the name, or another database error.
	UpdateCollection(ctx context.Context, id int64, update store.CollectionUpdate) error
	// [DeleteCollection] removes a collection, keeping its media.
	//
	// Parameters:
	//   - [ctx]: cancels the delete.
	//   - [id]: the collection.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	DeleteCollection(ctx context.Context, id int64) error
	// [Subtitles] lists a media item's tracks.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [mediaID]: the media item.
	//
	// Returns:
	//   - [[]store.Subtitle]: the tracks.
	//   - [error]: [nil] on success, or the database error.
	Subtitles(ctx context.Context, mediaID int64) ([]store.Subtitle, error)
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
	// [AddSubtitle] stores a subtitle track.
	//
	// Parameters:
	//   - [ctx]: cancels the inserts.
	//   - [subtitle]: the track and its lines.
	//
	// Returns:
	//   - [int64] ([id]): the new track's ID.
	//   - [bool] ([inserted]): [true] if the track was added.
	//   - [error] ([err]): [nil] on success, or the database error.
	AddSubtitle(ctx context.Context, subtitle store.NewSubtitle) (id int64, inserted bool, err error)
	// [UpdateSubtitleLabel] changes a track's language and label.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [id]: the track.
	//   - [language]: the new language code.
	//   - [label]: the new label.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	UpdateSubtitleLabel(ctx context.Context, id int64, language, label string) error
	// [DeleteSubtitle] removes a track.
	//
	// Parameters:
	//   - [ctx]: cancels the delete.
	//   - [id]: the track.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	DeleteSubtitle(ctx context.Context, id int64) error
	// [ConversionFailures] lists the failed conversions.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//
	// Returns:
	//   - [[]store.ConversionFailure]: the failures, by path.
	//   - [error]: [nil] on success, or the database error.
	ConversionFailures(ctx context.Context) ([]store.ConversionFailure, error)
	// [RetryConversion] puts a failed file back in the conversion queue.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [id]: the media item.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	RetryConversion(ctx context.Context, id int64) error
}

// [metadataLookup] finds titles on TMDB for the control panel;
// [metadata.Client] implements it.
type metadataLookup interface {
	// [Enabled] reports whether TMDB lookups are configured.
	//
	// Returns:
	//   - [bool]: [true] if an API key is set.
	Enabled() bool
	// [Show] looks up a TV show.
	//
	// Parameters:
	//   - [ctx]: cancels the lookup.
	//   - [name]: the show's name.
	//   - [year]: the first year, or "" for any.
	//
	// Returns:
	//   - [metadata.Info]: the show's details.
	//   - [error]: as [metadata.Client.Show] describes.
	Show(ctx context.Context, name, year string) (metadata.Info, error)
	// [Movie] looks up a film.
	//
	// Parameters:
	//   - [ctx]: cancels the lookup.
	//   - [title]: the film's title.
	//   - [year]: the release year, or "" for any.
	//
	// Returns:
	//   - [metadata.Info]: the film's details.
	//   - [error]: as [metadata.Client.Movie] describes.
	Movie(ctx context.Context, title, year string) (metadata.Info, error)
	// [Collection] looks up a collection of films.
	//
	// Parameters:
	//   - [ctx]: cancels the lookup.
	//   - [name]: the collection's name.
	//
	// Returns:
	//   - [metadata.Info]: the collection's details.
	//   - [error]: as [metadata.Client.Collection] describes.
	Collection(ctx context.Context, name string) (metadata.Info, error)
}

// [metadataRefresher] looks one media item up again; [library.Scanner]
// implements it.
type metadataRefresher interface {
	// [RefreshMetadata] looks a media item up on TMDB again and saves what it
	// finds.
	//
	// Parameters:
	//   - [ctx]: cancels the lookup.
	//   - [id]: the media item.
	//
	// Returns:
	//   - [error]: as [library.Scanner.RefreshMetadata] describes.
	RefreshMetadata(ctx context.Context, id int64) error
}

// [scanTrigger] starts a library scan; [library.Scanner] implements it.
type scanTrigger interface {
	// [Trigger] asks for a scan as soon as possible, without waiting for it.
	Trigger()
}

// [mediaFromPath] loads the media item named by {id} in the request's path.
// Administrators see unpublished media too, so there is no visibility
// check.
//
// Parameters:
//   - [r]: the request, whose route has an {id} wildcard.
//
// Returns:
//   - [store.Media]: the media item, or a zero [store.Media] if [error] is
//     not [nil].
//   - [error]: [nil] on success, [store.ErrNotFound] if {id} is not a valid
//     ID or the item does not exist, or another database error.
func (handlers *adminHandlers) mediaFromPath(r *http.Request) (store.Media, error) {
	id, err := pathID(r)
	if err != nil {
		return store.Media{}, err
	}
	return handlers.store.MediaByID(r.Context(), id)
}

// [controlPanel] handles GET /admin: the media waiting to be published, the
// collections, the conversion queue, and the confirmation named by the
// "done" parameter, if any (see done_messages.txt).
//
// Parameters:
//   - [w]: where the page is written.
//   - [r]: the request from the administrator.
//
// Returns:
//   - [error]: [nil] once written, or the database error.
func (handlers *adminHandlers) controlPanel(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	pending, err := handlers.store.PendingMedia(ctx)
	if err != nil {
		return err
	}
	allCollections, err := handlers.store.Collections(ctx)
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
	handlers.render(w, r, http.StatusOK, controlPanel(newPage(r, "Control panel"), pending, allCollections, categories, handlers.metadata.Enabled(), conversions,
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

// [conversionView] loads the converter's status and the recorded failures.
//
// Parameters:
//   - [ctx]: cancels the database queries.
//
// Returns:
//   - [ConversionView]: the status and failures, filled as far as they were
//     read.
//   - [error]: [nil] on success, or the database error.
func (handlers *adminHandlers) conversionView(ctx context.Context) (ConversionView, error) {
	var view ConversionView
	var err error
	if view.Status, err = handlers.conversions.Status(ctx); err != nil {
		return view, err
	}
	view.Failures, err = handlers.store.ConversionFailures(ctx)
	return view, err
}

// [conversionsPanel] handles GET /admin/conversions: the conversion section
// alone, which the control panel polls while conversions run.
//
// Parameters:
//   - [w]: where the section is written.
//   - [r]: the request from the administrator.
//
// Returns:
//   - [error]: [nil] once written, or the database error.
func (handlers *adminHandlers) conversionsPanel(w http.ResponseWriter, r *http.Request) error {
	view, err := handlers.conversionView(r.Context())
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, conversionsSection(viewer(r).CSRF, view))
	return nil
}

// [retryConversion] handles POST /admin/media/{id}/retry-conversion: it puts
// a failed file back in the queue, wakes the converter and goes back to the
// control panel with a confirmation.
//
// Parameters:
//   - [w]: where the redirect is written.
//   - [r]: the request; {id} in its path names the media item.
//
// Returns:
//   - [error]: [nil] once answered, [store.ErrNotFound] if {id} is not a
//     valid ID, or the database error.
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

// [scanNow] handles POST /admin/scan: it asks for a library scan and goes
// back to the control panel with a confirmation, without waiting for the
// scan.
//
// Parameters:
//   - [w]: where the redirect is written.
//   - [r]: the request from the administrator.
//
// Returns:
//   - [error]: always [nil].
func (handlers *adminHandlers) scanNow(w http.ResponseWriter, r *http.Request) error {
	handlers.scanner.Trigger()
	http.Redirect(w, r, "/admin?done=scan", http.StatusSeeOther)
	return nil
}

// [mediaForm] fills the edit form for a media item. For an item not yet
// published it suggests values from the file name: an episode's show
// becomes its collection (spelled like an existing collection of the same
// name), and a film without a category gets "Movies".
//
// Parameters:
//   - [ctx]: cancels the database queries.
//   - [media]: the media item.
//
// Returns:
//   - [MediaForm]: the form's data, filled as far as it was read.
//   - [error]: [nil] on success, or the database error.
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

// [editMediaPage] handles GET /admin/media/{id}: a media item's edit page.
// The "saved" and "refreshed" parameters show a confirmation.
//
// Parameters:
//   - [w]: where the page is written.
//   - [r]: the request; {id} in its path names the media item.
//
// Returns:
//   - [error]: [nil] once written; [store.ErrNotFound] if {id} is not a
//     valid ID or the item does not exist; otherwise the database error.
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

// [showMediaFormError] shows the edit page again with a [*formError]'s
// message and status 400. Note that the form shows the item's stored values,
// not what was typed.
//
// Parameters:
//   - [w]: where the page is written.
//   - [r]: the request.
//   - [media]: the media item.
//   - [err]: the error from handling the form.
//
// Returns:
//   - [error]: [nil] if [err] was a [*formError] and the page was written;
//     otherwise [err] unchanged, or the database error from loading the
//     form.
func (handlers *adminHandlers) showMediaFormError(w http.ResponseWriter, r *http.Request, media store.Media, err error) error {
	var formErr *formError
	if !errors.As(err, &formErr) {
		return err
	}
	form, loadErr := handlers.mediaForm(r.Context(), media)
	if loadErr != nil {
		return loadErr
	}
	form.Error = formErr.message
	handlers.render(w, r, http.StatusBadRequest, editMediaPage(newPage(r, "Edit "+media.Title), form))
	return nil
}

// [saveMedia] handles POST /admin/media/{id}: the edit form, whose second
// button also publishes. After saving it goes back to the edit page, or
// after publishing to the control panel, with a confirmation.
//
// Parameters:
//   - [w]: where the redirect or form is written.
//   - [r]: the request; {id} in its path names the media item.
//
// Returns:
//   - [error]: [nil] once answered, including for invalid input (the form
//     is shown again); [store.ErrNotFound] if {id} is not a valid ID or the
//     item does not exist; otherwise a database or file error.
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

// [applyMediaInput] saves a decoded edit form: it finds or creates the
// collection, stores the new poster, then updates the media item.
//
// Parameters:
//   - [ctx]: cancels the database work and TMDB lookup.
//   - [media]: the media item.
//   - [input]: the decoded form.
//
// Returns:
//   - [error]: [nil] on success, or the database or file error.
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

// [showYearGuess] picks the year a new collection for a media item starts
// with: the year in the show's folder name ("Star Trek: Deep Space Nine
// (1993)/..."), if the file is an episode of that show. Never the item's own
// year, which for an episode is the year it aired, not the year the show
// began; without a folder year, TMDB fills the year in.
//
// Parameters:
//   - [media]: the media item.
//   - [collection]: the new collection's name.
//
// Returns:
//   - [string]: the year from the path, or "".
func showYearGuess(media store.Media, collection string) string {
	guess := guessFor(media)
	if guess.IsEpisode() && strings.EqualFold(guess.Show, collection) {
		return guess.Year
	}
	return ""
}

// [findOrCreateCollection] gets a collection by name, creating it if it does
// not exist yet. A new collection gets whatever details TMDB has, and is
// still created if the lookup fails.
//
// Parameters:
//   - [ctx]: cancels the database work and TMDB lookup.
//   - [name]: the collection's name; case does not matter for finding it.
//   - [category]: the category for a new collection; "" means "TV Shows".
//   - [year]: the year for a new collection, or "" to let TMDB fill it in.
//
// Returns:
//   - [store.Collection]: the existing or new collection. [category] and
//     [year] are only used when it is created.
//   - [error]: [nil] on success, or the database error. If another request
//     creates the same collection at the same moment, that collection is
//     returned instead of an error.
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

// [fillCollectionUpdate] applies a TMDB lookup to an edit of a collection.
// The lookup only fills empty fields: the existing poster is kept unless the
// administrator removed it, in which case the lookup may supply a new one.
//
// Parameters:
//   - [ctx]: cancels the lookup.
//   - [existing]: the collection as stored.
//   - [update]: the edit; its year, overview and poster fields are changed.
func (handlers *adminHandlers) fillCollectionUpdate(ctx context.Context, existing store.Collection, update *store.CollectionUpdate) {
	collection := store.Collection{Name: update.Name, Category: update.Category, Year: update.Year, Overview: update.Overview, Poster: update.Poster}
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

// [lookupCollection] finds a collection on TMDB. A films category (see
// [isFilmCategory]) means a series of films, so it is searched as a TMDB
// collection and then as a single film; anything else is searched as a TV
// show first, then the same way. A film's year is dropped, since it is not
// the collection's.
//
// Parameters:
//   - [ctx]: cancels the lookups.
//   - [name]: the collection's name.
//   - [category]: the collection's category.
//
// Returns:
//   - [metadata.Info]: the details found.
//   - [error]: [nil] on success, [metadata.ErrNoMatch] if nothing matched,
//     or another lookup error.
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

// [isFilmCategory] reports whether a category holds films rather than
// shows.
//
// Parameters:
//   - [category]: the category's name; case and spaces around it do not
//     matter.
//
// Returns:
//   - [bool]: [true] for "movies", "movie", "films" and "film", [false]
//     otherwise.
func isFilmCategory(category string) bool {
	switch strings.ToLower(strings.TrimSpace(category)) {
	case "movies", "movie", "films", "film":
		return true
	}
	return false
}

// [fillFromTMDB] fills a collection's empty year, overview and poster from
// TMDB, when lookups are on, within 20 seconds. A failed lookup is logged
// (finding nothing is not) and leaves the collection unchanged.
//
// Parameters:
//   - [ctx]: cancels the lookup.
//   - [collection]: the collection to fill in; changed in place.
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

// [savePoster] stores an uploaded poster in the poster folder under a new
// random name, never overwriting a file. If copying fails partway, the
// partly written file is left behind for the [posters.Sweeper] to delete.
//
// Parameters:
//   - [upload]: the poster, or [nil] if none was uploaded.
//
// Returns:
//   - [string]: the new file's name, such as "upload-3f9a0c1d2e4b5a6f.jpg",
//     or "" if [upload] is [nil] or [error] is not [nil].
//   - [error]: [nil] on success or when [upload] is [nil]; otherwise the
//     error from reading the upload, making the random name, creating the
//     file, or writing it.
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
	posterFile, err := os.OpenFile(filepath.Join(handlers.posterDirectory, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(posterFile, file); err != nil {
		posterFile.Close()
		return "", err
	}
	return name, posterFile.Close()
}

// [refreshMetadata] handles POST /admin/media/{id}/refresh-metadata: it looks
// the item up on TMDB again and takes its title, year, description and
// poster, even if it is published, then goes back to the edit page. Lookups
// that are off, find nothing or fail are shown on the edit page.
//
// Parameters:
//   - [w]: where the redirect or page is written.
//   - [r]: the request; {id} in its path names the media item.
//
// Returns:
//   - [error]: [nil] once answered; [store.ErrNotFound] if {id} is not a
//     valid ID or the item does not exist; otherwise the database error.
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

// [uploadSubtitle] handles POST /admin/media/{id}/subtitles: an SRT or
// WebVTT file for the video, labelled with [subtitle.LanguageName]. An
// upload has no source, so it is always added. It goes back to the edit
// page; a bad upload is shown there.
//
// Parameters:
//   - [w]: where the redirect or page is written.
//   - [r]: the request; {id} names the media item and the form holds
//     "language" and "file".
//
// Returns:
//   - [error]: [nil] once answered; [store.ErrNotFound] if {id} is not a
//     valid ID or the item does not exist; otherwise the database error.
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

// [saveSubtitle] handles POST /admin/subtitles/{id}: a track's language code
// (lowercased) and label, then back to the media item's edit page.
//
// Parameters:
//   - [w]: where the redirect is written.
//   - [r]: the request; {id} names the track and the form holds "language"
//     and "label".
//
// Returns:
//   - [error]: [nil] once answered; a 400 [*statusError] if either field is
//     empty; [store.ErrNotFound] if {id} is not a valid ID or the track does
//     not exist; otherwise the database error.
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

// [deleteSubtitle] handles POST /admin/subtitles/{id}/delete: it removes a
// track, then goes back to the media item's edit page.
//
// Parameters:
//   - [w]: where the redirect is written.
//   - [r]: the request; {id} in its path names the track.
//
// Returns:
//   - [error]: [nil] once answered; [store.ErrNotFound] if {id} is not a
//     valid ID or the track does not exist; otherwise the database error.
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

// [createCollection] handles POST /admin/collections from the control panel:
// it creates a collection, filling empty fields from TMDB if asked, and
// opens its edit page.
//
// Parameters:
//   - [w]: where the redirect is written.
//   - [r]: the request with the collection form.
//
// Returns:
//   - [error]: [nil] once answered; a 400 [*statusError] for invalid input;
//     a 409 [*statusError] if the name is taken; otherwise a database or file
//     error.
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

// [editCollectionPage] handles GET /admin/collections/{id}: a collection's
// edit page.
//
// Parameters:
//   - [w]: where the page is written.
//   - [r]: the request; {id} in its path names the collection.
//
// Returns:
//   - [error]: [nil] once written; [store.ErrNotFound] if {id} is not a
//     valid ID or the collection does not exist; otherwise the database
//     error.
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

// [saveCollection] handles POST /admin/collections/{id}: the collection edit
// form, filling empty fields from TMDB if asked, then back to the edit page.
// Invalid input or a name another collection has is shown on the edit page.
//
// Parameters:
//   - [w]: where the redirect or page is written.
//   - [r]: the request; {id} names the collection and the form holds its
//     fields.
//
// Returns:
//   - [error]: [nil] once answered; [store.ErrNotFound] if {id} is not a
//     valid ID or the collection does not exist; otherwise a database or
//     file error.
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

// [showCollectionFormError] shows a collection's edit page again with what
// was typed and a [*formError]'s message, with status 400.
//
// Parameters:
//   - [w]: where the page is written.
//   - [r]: the request.
//   - [collection]: the collection as stored.
//   - [typed]: what was typed into the form.
//   - [err]: the error from handling the form.
//
// Returns:
//   - [error]: [nil] if [err] was a [*formError] and the page was written;
//     otherwise [err] unchanged, or the database error.
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

// [deleteCollection] handles POST /admin/collections/{id}/delete: it removes
// the collection, whose media stay in the library outside any collection,
// and goes back to the control panel with a confirmation.
//
// Parameters:
//   - [w]: where the redirect is written.
//   - [r]: the request; {id} in its path names the collection.
//
// Returns:
//   - [error]: [nil] once answered, [store.ErrNotFound] if {id} is not a
//     valid ID, or the database error.
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

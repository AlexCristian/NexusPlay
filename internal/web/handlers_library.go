package web

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/AlexCristian/NexusPlay/internal/collections"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [libraryHandlers] serve browsing: the home page, search, collections,
// subscriptions and the viewer's own lists.
type libraryHandlers struct {
	// [responder] writes the responses.
	responder
	// [store] reads the library and the viewer's activity.
	store libraryStore
}

// [libraryStore] is the part of the database the library pages read;
// [store.Store] implements it. Each method is described at the
// [store.Store] method of the same name.
type libraryStore interface {
	// [RecentTiles] lists tiles for the most recently published media.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [userID]: the viewer, or 0 for anonymous.
	//   - [limit]: the most tiles to return.
	//
	// Returns:
	//   - [[]store.Tile]: the tiles, newest first.
	//   - [error]: [nil] on success, or the database error.
	RecentTiles(ctx context.Context, userID int64, limit int) ([]store.Tile, error)
	// [Categories] groups everything published into home-page rows.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [userID]: the viewer, or 0 for anonymous.
	//
	// Returns:
	//   - [[]store.Category]: the rows.
	//   - [error]: [nil] on success, or the database error.
	Categories(ctx context.Context, userID int64) ([]store.Category, error)
	// [Search] finds standalone media and collections whose title contains some
	// text.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [userID]: the viewer, or 0 for anonymous.
	//   - [query]: the text to look for.
	//   - [limit]: the most tiles to return.
	//
	// Returns:
	//   - [[]store.Category]: the matching tiles in rows.
	//   - [error]: [nil] on success, or the database error.
	Search(ctx context.Context, userID int64, query string, limit int) ([]store.Category, error)
	// [CollectionByID] loads one collection.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [id]: the collection.
	//
	// Returns:
	//   - [store.Collection]: the collection.
	//   - [error]: [nil] on success, [store.ErrNotFound] if it does not exist,
	//     or another database error.
	CollectionByID(ctx context.Context, id int64) (store.Collection, error)
	// [CollectionEpisodes] lists a collection's published media in order.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [collectionID]: the collection.
	//
	// Returns:
	//   - [[]store.Media]: the media, by season and episode.
	//   - [error]: [nil] on success, or the database error.
	CollectionEpisodes(ctx context.Context, collectionID int64) ([]store.Media, error)
	// [WatchedIn] lists which media of a collection a user has finished.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [userID]: the viewer.
	//   - [collectionID]: the collection.
	//
	// Returns:
	//   - [collections.Set[int64]]: the finished media's IDs.
	//   - [error]: [nil] on success, or the database error.
	WatchedIn(ctx context.Context, userID, collectionID int64) (collections.Set[int64], error)
	// [IsSubscribed] reports whether a user follows a collection.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [userID]: the viewer.
	//   - [collectionID]: the collection.
	//
	// Returns:
	//   - [bool]: [true] if subscribed.
	//   - [error]: [nil] on success, or the database error.
	IsSubscribed(ctx context.Context, userID, collectionID int64) (bool, error)
	// [ToggleSubscription] subscribes a user to a collection, or unsubscribes
	// them.
	//
	// Parameters:
	//   - [ctx]: cancels the update.
	//   - [userID]: the viewer.
	//   - [collectionID]: the collection.
	//
	// Returns:
	//   - [bool]: [true] if the user is now subscribed.
	//   - [error]: [nil] on success, or the database error.
	ToggleSubscription(ctx context.Context, userID, collectionID int64) (bool, error)
	// [InProgress] lists the media a user stopped partway through.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [userID]: the viewer.
	//   - [limit]: the most items to list.
	//
	// Returns:
	//   - [[]store.Resume]: the items, most recently watched first.
	//   - [error]: [nil] on success, or the database error.
	InProgress(ctx context.Context, userID int64, limit int) ([]store.Resume, error)
	// [Notifications] lists the media a user was notified about.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [userID]: the viewer.
	//   - [limit]: the most items to list.
	//
	// Returns:
	//   - [[]store.Media]: the media, newest first.
	//   - [error]: [nil] on success, or the database error.
	Notifications(ctx context.Context, userID int64, limit int) ([]store.Media, error)
	// [SubscriptionCount] counts the collections a user is subscribed to.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//   - [userID]: the viewer.
	//
	// Returns:
	//   - [int]: the number of subscriptions.
	//   - [error]: [nil] on success, or the database error.
	SubscriptionCount(ctx context.Context, userID int64) (int, error)
}

const (
	// [recentLimit] is how many tiles the "Just added" row shows.
	recentLimit = 12
	// [searchLimit] is how many results a search shows.
	searchLimit = 60
	// [maxQueryLength] is how many characters of a search query are used;
	// the rest is ignored.
	maxQueryLength = 100
	// [resumeLimit] is how many items the "Previously watching" list shows.
	resumeLimit = 10
	// [notificationLimit] is how many notifications the list shows.
	notificationLimit = 20
)

// [home] handles GET /: the "Just added" row and a tab per category, with
// the viewer's watched marks.
//
// Parameters:
//   - [w]: where the page is written.
//   - [r]: the request.
//
// Returns:
//   - [error]: [nil] once the page is written, or the database error.
func (handlers *libraryHandlers) home(w http.ResponseWriter, r *http.Request) error {
	recent, categories, err := handlers.libraryData(r)
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, homePage(newPage(r, ""), recent, categories))
	return nil
}

// [libraryData] loads what the home page and an emptied search show: the
// "Just added" row and every category, with the viewer's watched marks.
//
// Parameters:
//   - [r]: the request, which tells who the viewer is.
//
// Returns:
//   - [[]store.Tile]: the "Just added" tiles, newest first.
//   - [[]store.Category]: every category with its tiles.
//   - [error]: [nil] on success, or the database error.
func (handlers *libraryHandlers) libraryData(r *http.Request) ([]store.Tile, []store.Category, error) {
	userID := viewer(r).ID()
	recent, err := handlers.store.RecentTiles(r.Context(), userID, recentLimit)
	if err != nil {
		return nil, nil, err
	}
	categories, err := handlers.store.Categories(r.Context(), userID)
	return recent, categories, err
}

// [search] handles GET /search?q=. As the visitor types, htmx puts the
// results in the search drawer; an empty query or no results renders
// nothing, which closes it. Without JavaScript it is the home page with the
// drawer open, and an empty query redirects to the home page. Only the
// first [maxQueryLength] characters of the query are used.
//
// Parameters:
//   - [w]: where the results or page are written.
//   - [r]: the request; its "q" parameter is the query.
//
// Returns:
//   - [error]: [nil] once answered, or the database error.
func (handlers *libraryHandlers) search(w http.ResponseWriter, r *http.Request) error {
	query := truncate(strings.TrimSpace(r.URL.Query().Get("q")), maxQueryLength)
	if query == "" && !isHTMX(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return nil
	}
	var categories []store.Category
	if query != "" {
		var err error
		if categories, err = handlers.store.Search(r.Context(), viewer(r).ID(), query, searchLimit); err != nil {
			return err
		}
	}
	if isHTMX(r) {
		handlers.render(w, r, http.StatusOK, searchResults(categories))
		return nil
	}
	recent, libraryCategories, err := handlers.libraryData(r)
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, searchPage(newPage(r, "Search"), categories, recent, libraryCategories))
	return nil
}

// [truncate] shortens text to a number of characters.
//
// Parameters:
//   - [text]: the text to shorten.
//   - [limit]: the most characters (runes) to keep.
//
// Returns:
//   - [string]: [text] unchanged if it is short enough, otherwise its first
//     [limit] characters. Nothing is added to show it was cut.
func truncate(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit])
}

// [collection] handles GET /collections/{id}: a collection's dialog with its
// seasons and, for a signed-in viewer, their subscription and watched marks.
// Without htmx it is a full page. A collection with no published media is
// still shown, empty.
//
// Parameters:
//   - [w]: where the dialog or page is written.
//   - [r]: the request; {id} in its path names the collection.
//
// Returns:
//   - [error]: [nil] once written; [store.ErrNotFound] if {id} is not a
//     valid ID or the collection does not exist; otherwise the database
//     error.
func (handlers *libraryHandlers) collection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	collection, err := handlers.store.CollectionByID(ctx, id)
	if err != nil {
		return err
	}
	episodes, err := handlers.store.CollectionEpisodes(ctx, id)
	if err != nil {
		return err
	}
	currentViewer := viewer(r)
	var subscribed bool
	watched := collections.NewSet[int64]()
	if currentViewer.User != nil {
		if subscribed, err = handlers.store.IsSubscribed(ctx, currentViewer.ID(), id); err != nil {
			return err
		}
		if watched, err = handlers.store.WatchedIn(ctx, currentViewer.ID(), id); err != nil {
			return err
		}
	}
	dialog := collectionDialog(currentViewer, collection, groupBySeason(episodes), subscribed, watched)
	return handlers.respond(w, r, http.StatusOK, collection.Name, dialog, collectionPage)
}

// [groupBySeason] splits a collection's episodes into seasons.
//
// Parameters:
//   - [episodes]: the episodes, already sorted by season and episode.
//
// Returns:
//   - [[]Season]: one entry per season, in the order they appear in
//     [episodes]; [nil] if there are none.
func groupBySeason(episodes []store.Media) []Season {
	var seasons []Season
	for _, episode := range episodes {
		if len(seasons) == 0 || seasons[len(seasons)-1].Number != episode.Season {
			seasons = append(seasons, Season{Number: episode.Season})
		}
		last := &seasons[len(seasons)-1]
		last.Episodes = append(last.Episodes, episode)
	}
	return seasons
}

// [toggleSubscription] handles POST /collections/{id}/subscribe: it
// subscribes the viewer to the collection, or unsubscribes them, and
// answers with the subscribe ribbon in its new state.
//
// Parameters:
//   - [w]: where the ribbon is written.
//   - [r]: the request from a signed-in viewer; {id} in its path names the
//     collection.
//
// Returns:
//   - [error]: [nil] once written; [store.ErrNotFound] if {id} is not a
//     valid ID or the collection does not exist; otherwise the database
//     error.
func (handlers *libraryHandlers) toggleSubscription(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if _, err := handlers.store.CollectionByID(r.Context(), id); err != nil {
		return err
	}
	subscribed, err := handlers.store.ToggleSubscription(r.Context(), viewer(r).ID(), id)
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, subscribeRibbon(id, subscribed))
	return nil
}

// [resumeList] handles GET /me/resume: the "Previously watching" popover,
// with at most [resumeLimit] items.
//
// Parameters:
//   - [w]: where the popover is written.
//   - [r]: the request from a signed-in viewer.
//
// Returns:
//   - [error]: [nil] once written, or the database error.
func (handlers *libraryHandlers) resumeList(w http.ResponseWriter, r *http.Request) error {
	items, err := handlers.store.InProgress(r.Context(), viewer(r).ID(), resumeLimit)
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, resumeList(items))
	return nil
}

// [notificationList] handles GET /me/notifications: new episodes of shows
// the viewer subscribed to, at most [notificationLimit] of them.
//
// Parameters:
//   - [w]: where the list is written.
//   - [r]: the request from a signed-in viewer.
//
// Returns:
//   - [error]: [nil] once written, or the database error.
func (handlers *libraryHandlers) notificationList(w http.ResponseWriter, r *http.Request) error {
	items, err := handlers.store.Notifications(r.Context(), viewer(r).ID(), notificationLimit)
	if err != nil {
		return err
	}
	subscriptionCount, err := handlers.store.SubscriptionCount(r.Context(), viewer(r).ID())
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, notificationList(items, subscriptionCount))
	return nil
}

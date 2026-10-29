package web

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/AlexCristian/NexusPlay/internal/collections"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// libraryHandlers serve browsing: the home page, search, collections,
// subscriptions and the viewer's own lists.
type libraryHandlers struct {
	responder
	store libraryStore
}

// libraryStore is the part of the database the library pages read.
type libraryStore interface {
	RecentTiles(ctx context.Context, userID int64, limit int) ([]store.Tile, error)
	Categories(ctx context.Context, userID int64) ([]store.Category, error)
	Search(ctx context.Context, userID int64, query string, limit int) ([]store.Category, error)
	CollectionByID(ctx context.Context, id int64) (store.Collection, error)
	CollectionEpisodes(ctx context.Context, collectionID int64) ([]store.Media, error)
	WatchedIn(ctx context.Context, userID, collectionID int64) (collections.Set[int64], error)
	IsSubscribed(ctx context.Context, userID, collectionID int64) (bool, error)
	ToggleSubscription(ctx context.Context, userID, collectionID int64) (bool, error)
	InProgress(ctx context.Context, userID int64, limit int) ([]store.Resume, error)
	Notifications(ctx context.Context, userID int64, limit int) ([]store.Media, error)
	SubscriptionCount(ctx context.Context, userID int64) (int, error)
}

const (
	recentLimit = 12  // tiles under "Just added"
	searchLimit = 60  // results per search
	maxQuery    = 100 // characters of a search query that are used
)

// home handles GET /: "Just added" and a tab per category.
func (handlers *libraryHandlers) home(w http.ResponseWriter, r *http.Request) error {
	recent, cats, err := handlers.libraryData(r)
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, homePage(newPage(r, ""), recent, cats))
	return nil
}

// libraryData is what the home page and an emptied search show: the
// recent row and every category, with the viewer's watched marks.
func (handlers *libraryHandlers) libraryData(r *http.Request) ([]store.Tile, []store.Category, error) {
	uid := viewer(r).ID()
	recent, err := handlers.store.RecentTiles(r.Context(), uid, recentLimit)
	if err != nil {
		return nil, nil, err
	}
	cats, err := handlers.store.Categories(r.Context(), uid)
	return recent, cats, err
}

// search handles GET /search?q=. As the visitor types, htmx puts the
// results in the search drawer; an empty query or no results renders
// nothing, which closes it. Without JavaScript it is the home page with the
// drawer open.
func (handlers *libraryHandlers) search(w http.ResponseWriter, r *http.Request) error {
	query := truncate(strings.TrimSpace(r.URL.Query().Get("q")), maxQuery)
	if query == "" && !isHTMX(r) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return nil
	}
	var cats []store.Category
	if query != "" {
		var err error
		if cats, err = handlers.store.Search(r.Context(), viewer(r).ID(), query, searchLimit); err != nil {
			return err
		}
	}
	if isHTMX(r) {
		handlers.render(w, r, http.StatusOK, searchResults(cats))
		return nil
	}
	recent, libCats, err := handlers.libraryData(r)
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, searchPage(newPage(r, "Search"), cats, recent, libCats))
	return nil
}

// truncate shortens s to at most n characters.
func truncate(text string, limit int) string {
	if utf8.RuneCountInString(text) <= limit {
		return text
	}
	return string([]rune(text)[:limit])
}

// collection handles GET /collections/{id}: the show's dialog with its
// seasons, or a full page without htmx.
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

// groupBySeason splits episodes, already sorted by season and episode, into
// seasons.
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

// toggleSubscription handles POST /collections/{id}/subscribe and returns
// the ribbon in its new state.
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

// resumeList handles GET /me/resume: the "Previously watching" popover.
func (handlers *libraryHandlers) resumeList(w http.ResponseWriter, r *http.Request) error {
	items, err := handlers.store.InProgress(r.Context(), viewer(r).ID(), 10)
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, resumeList(items))
	return nil
}

// notificationList handles GET /me/notifications: new episodes of shows
// the viewer subscribed to.
func (handlers *libraryHandlers) notificationList(w http.ResponseWriter, r *http.Request) error {
	items, err := handlers.store.Notifications(r.Context(), viewer(r).ID(), 20)
	if err != nil {
		return err
	}
	subs, err := handlers.store.SubscriptionCount(r.Context(), viewer(r).ID())
	if err != nil {
		return err
	}
	handlers.render(w, r, http.StatusOK, notificationList(items, subs))
	return nil
}

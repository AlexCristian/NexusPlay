package web

import (
	"context"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/AlexCristian/NexusPlay/internal/library"
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
	Search(ctx context.Context, userID int64, q string, limit int) ([]store.Tile, error)
	CollectionByID(ctx context.Context, id int64) (store.Collection, error)
	CollectionEpisodes(ctx context.Context, collectionID int64) ([]store.Media, error)
	WatchedIn(ctx context.Context, userID, collectionID int64) (map[int64]bool, error)
	IsSubscribed(ctx context.Context, userID, collectionID int64) (bool, error)
	ToggleSubscription(ctx context.Context, userID, collectionID int64) (bool, error)
	InProgress(ctx context.Context, userID int64, limit int) ([]store.Resume, error)
	Notifications(ctx context.Context, userID int64, limit int) ([]store.Media, error)
}

const (
	recentLimit = 12  // tiles under "Just added"
	searchLimit = 60  // results per search
	maxQuery    = 100 // characters of a search query that are used
)

func guessFor(m store.Media) library.Guess { return library.ParsePath(m.Path) }

// home handles GET /: "Just added" and a tab per category.
func (h *libraryHandlers) home(w http.ResponseWriter, r *http.Request) error {
	recent, cats, err := h.libraryData(r)
	if err != nil {
		return err
	}
	h.render(w, r, http.StatusOK, homePage(newPage(r, ""), recent, cats))
	return nil
}

func (h *libraryHandlers) libraryData(r *http.Request) ([]store.Tile, []store.Category, error) {
	uid := viewer(r).ID()
	recent, err := h.store.RecentTiles(r.Context(), uid, recentLimit)
	if err != nil {
		return nil, nil, err
	}
	cats, err := h.store.Categories(r.Context(), uid)
	return recent, cats, err
}

// search handles GET /search?q=. htmx swaps the results into the library
// panel as the visitor types; an empty query brings the library back.
func (h *libraryHandlers) search(w http.ResponseWriter, r *http.Request) error {
	q := truncate(strings.TrimSpace(r.URL.Query().Get("q")), maxQuery)
	if q == "" {
		if !isHTMX(r) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return nil
		}
		recent, cats, err := h.libraryData(r)
		if err != nil {
			return err
		}
		h.render(w, r, http.StatusOK, libraryView(newPage(r, "Search"), recent, cats))
		return nil
	}
	tiles, err := h.store.Search(r.Context(), viewer(r).ID(), q, searchLimit)
	if err != nil {
		return err
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, searchResults(q, tiles))
	} else {
		h.render(w, r, http.StatusOK, searchPage(newPage(r, "Search"), q, tiles))
	}
	return nil
}

// truncate shortens s to at most n characters.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n])
}

// collection handles GET /collections/{id}: the show's dialog with its
// seasons, or a full page without htmx.
func (h *libraryHandlers) collection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	c, err := h.store.CollectionByID(ctx, id)
	if err != nil {
		return err
	}
	eps, err := h.store.CollectionEpisodes(ctx, id)
	if err != nil {
		return err
	}
	v := viewer(r)
	var subscribed bool
	watched := map[int64]bool{}
	if v.User != nil {
		if subscribed, err = h.store.IsSubscribed(ctx, v.ID(), id); err != nil {
			return err
		}
		if watched, err = h.store.WatchedIn(ctx, v.ID(), id); err != nil {
			return err
		}
	}
	dialog := collectionDialog(v, c, groupBySeason(eps), subscribed, watched)
	return h.respond(w, r, http.StatusOK, c.Name, dialog, collectionPage)
}

// groupBySeason splits episodes, already sorted by season and episode, into
// seasons.
func groupBySeason(eps []store.Media) []Season {
	var seasons []Season
	for _, e := range eps {
		if len(seasons) == 0 || seasons[len(seasons)-1].Number != e.Season {
			seasons = append(seasons, Season{Number: e.Season})
		}
		last := &seasons[len(seasons)-1]
		last.Episodes = append(last.Episodes, e)
	}
	return seasons
}

// toggleSubscription handles POST /collections/{id}/subscribe and returns
// the ribbon in its new state.
func (h *libraryHandlers) toggleSubscription(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if _, err := h.store.CollectionByID(r.Context(), id); err != nil {
		return err
	}
	on, err := h.store.ToggleSubscription(r.Context(), viewer(r).ID(), id)
	if err != nil {
		return err
	}
	h.render(w, r, http.StatusOK, subscribeRibbon(id, on))
	return nil
}

// resumeList handles GET /me/resume: the "Previously watching" popover.
func (h *libraryHandlers) resumeList(w http.ResponseWriter, r *http.Request) error {
	items, err := h.store.InProgress(r.Context(), viewer(r).ID(), 10)
	if err != nil {
		return err
	}
	h.render(w, r, http.StatusOK, resumeList(items))
	return nil
}

// notificationList handles GET /me/notifications: new episodes of shows
// the viewer subscribed to.
func (h *libraryHandlers) notificationList(w http.ResponseWriter, r *http.Request) error {
	items, err := h.store.Notifications(r.Context(), viewer(r).ID(), 20)
	if err != nil {
		return err
	}
	h.render(w, r, http.StatusOK, notificationList(items))
	return nil
}

package web

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/AlexCristian/NexusPlay/internal/library"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

const (
	recentLimit = 12  // tiles under "Just added"
	searchLimit = 60  // results per search
	maxQuery    = 100 // characters of a search query that are used
)

func guessFor(m store.Media) library.Guess { return library.ParsePath(m.Path) }

// home handles GET /: "Just added" and a tab per category.
func (s *Server) home(w http.ResponseWriter, r *http.Request) error {
	recent, cats, err := s.libraryData(r)
	if err != nil {
		return err
	}
	s.render(w, r, http.StatusOK, homePage(s.page(r, ""), recent, cats))
	return nil
}

func (s *Server) libraryData(r *http.Request) ([]store.Tile, []store.Category, error) {
	uid := viewer(r).ID()
	recent, err := s.Store.RecentTiles(r.Context(), uid, recentLimit)
	if err != nil {
		return nil, nil, err
	}
	cats, err := s.Store.Categories(r.Context(), uid)
	return recent, cats, err
}

// search handles GET /search?q=. htmx swaps the results into the library
// panel as the visitor types; an empty query brings the library back.
func (s *Server) search(w http.ResponseWriter, r *http.Request) error {
	q := truncate(strings.TrimSpace(r.URL.Query().Get("q")), maxQuery)
	if q == "" {
		if !isHTMX(r) {
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return nil
		}
		recent, cats, err := s.libraryData(r)
		if err != nil {
			return err
		}
		s.render(w, r, http.StatusOK, libraryView(s.page(r, "Search"), recent, cats))
		return nil
	}
	tiles, err := s.Store.Search(r.Context(), viewer(r).ID(), q, searchLimit)
	if err != nil {
		return err
	}
	if isHTMX(r) {
		s.render(w, r, http.StatusOK, searchResults(q, tiles))
	} else {
		s.render(w, r, http.StatusOK, searchPage(s.page(r, "Search"), q, tiles))
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
func (s *Server) collection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	ctx := r.Context()
	c, err := s.Store.CollectionByID(ctx, id)
	if err != nil {
		return err
	}
	eps, err := s.Store.CollectionEpisodes(ctx, id)
	if err != nil {
		return err
	}
	v := viewer(r)
	var subscribed bool
	watched := map[int64]bool{}
	if v.User != nil {
		if subscribed, err = s.Store.IsSubscribed(ctx, v.ID(), id); err != nil {
			return err
		}
		if watched, err = s.Store.WatchedIn(ctx, v.ID(), id); err != nil {
			return err
		}
	}
	dialog := collectionDialog(v, c, groupBySeason(eps), subscribed, watched)
	return s.respond(w, r, http.StatusOK, c.Name, dialog, collectionPage)
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
func (s *Server) toggleSubscription(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if _, err := s.Store.CollectionByID(r.Context(), id); err != nil {
		return err
	}
	on, err := s.Store.ToggleSubscription(r.Context(), viewer(r).ID(), id)
	if err != nil {
		return err
	}
	s.render(w, r, http.StatusOK, subscribeRibbon(id, on))
	return nil
}

// resumeList handles GET /me/resume: the "Previously watching" popover.
func (s *Server) resumeList(w http.ResponseWriter, r *http.Request) error {
	items, err := s.Store.InProgress(r.Context(), viewer(r).ID(), 10)
	if err != nil {
		return err
	}
	s.render(w, r, http.StatusOK, resumeList(items))
	return nil
}

// notificationList handles GET /me/notifications: new episodes of shows
// the viewer subscribed to.
func (s *Server) notificationList(w http.ResponseWriter, r *http.Request) error {
	items, err := s.Store.Notifications(r.Context(), viewer(r).ID(), 20)
	if err != nil {
		return err
	}
	s.render(w, r, http.StatusOK, notificationList(items))
	return nil
}

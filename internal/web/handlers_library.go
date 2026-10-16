package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/library"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

const recentLimit = 12

func guessFor(m store.Media) library.Guess { return library.ParsePath(m.Path) }

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	recent, cats, err := s.libraryData(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, homePage(s.page(r, ""), recent, cats))
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

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	p := s.page(r, "Search")
	if q == "" {
		recent, cats, err := s.libraryData(r)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		if isHTMX(r) {
			s.render(w, r, http.StatusOK, libraryView(p, recent, cats))
		} else {
			http.Redirect(w, r, "/", http.StatusSeeOther)
		}
		return
	}
	if len(q) > 100 {
		q = q[:100]
	}
	tiles, err := s.Store.Search(r.Context(), viewer(r).ID(), q, 60)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if isHTMX(r) {
		s.render(w, r, http.StatusOK, searchResults(q, tiles))
		return
	}
	s.render(w, r, http.StatusOK, searchPage(p, q, tiles))
}

func (s *Server) collection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	ctx := r.Context()
	c, err := s.Store.CollectionByID(ctx, id)
	if errors.Is(err, store.ErrNotFound) {
		s.notFound(w, r)
		return
	} else if err != nil {
		s.serverError(w, r, err)
		return
	}
	eps, err := s.Store.CollectionEpisodes(ctx, id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	var seasons []Season
	for _, e := range eps {
		if len(seasons) == 0 || seasons[len(seasons)-1].Number != e.Season {
			seasons = append(seasons, Season{Number: e.Season})
		}
		seasons[len(seasons)-1].Episodes = append(seasons[len(seasons)-1].Episodes, e)
	}
	v := viewer(r)
	var subscribed bool
	watched := map[int64]bool{}
	if v.User != nil {
		if subscribed, err = s.Store.IsSubscribed(ctx, v.ID(), id); err != nil {
			s.serverError(w, r, err)
			return
		}
		if watched, err = s.Store.WatchedIn(ctx, v.ID(), id); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	dialog := collectionDialog(v, c, seasons, subscribed, watched)
	if isHTMX(r) {
		s.render(w, r, http.StatusOK, dialog)
		return
	}
	s.render(w, r, http.StatusOK, collectionPage(s.page(r, c.Name), dialog))
}

func (s *Server) toggleSubscription(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	if _, err := s.Store.CollectionByID(r.Context(), id); err != nil {
		s.notFound(w, r)
		return
	}
	on, err := s.Store.ToggleSubscription(r.Context(), viewer(r).ID(), id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, subscribeRibbon(id, on))
}

func (s *Server) resumeList(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.InProgress(r.Context(), viewer(r).ID(), 10)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, resumeList(items))
}

func (s *Server) notificationList(w http.ResponseWriter, r *http.Request) {
	items, err := s.Store.Notifications(r.Context(), viewer(r).ID(), 20)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, notificationList(items))
}

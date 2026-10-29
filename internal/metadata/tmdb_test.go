package metadata

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTMDB serves canned API responses and poster images.
type fakeTMDB struct {
	srv         *httptest.Server
	searches    atomic.Int32
	seasons     atomic.Int32
	posterFails bool
	lastAuth    atomic.Value // string: "key" or "bearer"
}

func newFake(t *testing.T) *fakeTMDB {
	t.Helper()
	f := &fakeTMDB{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search/movie", func(w http.ResponseWriter, r *http.Request) {
		f.note(r)
		if r.URL.Query().Get("query") == "Nothing" {
			io.WriteString(w, `{"results": []}`)
			return
		}
		io.WriteString(w, `{"results": [{"id": 603, "title": "The Matrix", "release_date": "1999-03-31",
			"overview": "A hacker learns the truth.", "poster_path": "/matrix.jpg"}]}`)
	})
	mux.HandleFunc("/api/search/tv", func(w http.ResponseWriter, r *http.Request) {
		f.note(r)
		f.searches.Add(1)
		time.Sleep(50 * time.Millisecond) // long enough for concurrent lookups to overlap
		io.WriteString(w, `{"results": [{"id": 1399, "name": "Cosmos", "first_air_date": "2014-03-09", "poster_path": "/cosmos.png"}]}`)
	})
	mux.HandleFunc("/api/tv/1399/season/1/episode/2", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"name": "Some of the Things That Molecules Do", "air_date": "2014-03-16"}`)
	})
	mux.HandleFunc("/api/tv/1399/season/1", func(w http.ResponseWriter, r *http.Request) {
		f.seasons.Add(1)
		io.WriteString(w, `{"episodes": [
			{"episode_number": 1, "name": "Standing Up in the Milky Way", "air_date": "2014-03-09", "overview": "One."},
			{"episode_number": 2, "name": "Some of the Things That Molecules Do", "air_date": "2014-03-16", "overview": "Two."}]}`)
	})
	mux.HandleFunc("/api/search/collection", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"results": [{"id": 115575, "name": "Star Trek: The Original Series Collection",
			"overview": "Films.", "poster_path": "/st.jpg"}]}`)
	})
	mux.HandleFunc("/img/", func(w http.ResponseWriter, r *http.Request) {
		if f.posterFails {
			http.Error(w, "gone", http.StatusInternalServerError)
			return
		}
		io.WriteString(w, "image bytes for "+r.URL.Path)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeTMDB) note(r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		f.lastAuth.Store("bearer")
	} else if r.URL.Query().Get("api_key") != "" {
		f.lastAuth.Store("key")
	}
}

func (f *fakeTMDB) client(t *testing.T, apiKey string) (*Client, string) {
	dir := t.TempDir()
	c := New(Config{APIKey: apiKey, PosterDirectory: dir}, slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithBaseURL(f.srv.URL+"/api", f.srv.URL+"/img"))
	return c, dir
}

func TestMovieLookup(t *testing.T) {
	f := newFake(t)
	c, dir := f.client(t, "short-v3-key")
	info, err := c.Movie(context.Background(), "The Matrix", "1999")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != 603 || info.Year != "1999" || info.Poster != "tmdb-movie-603.jpg" {
		t.Errorf("info = %+v", info)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, info.Poster)); string(b) != "image bytes for /img/matrix.jpg" {
		t.Errorf("poster file = %q", b)
	}
	if f.lastAuth.Load() != "key" {
		t.Errorf("a short v3 key must go in the query string, got %v", f.lastAuth.Load())
	}
	if _, err := c.Movie(context.Background(), "Nothing", ""); !errors.Is(err, ErrNoMatch) {
		t.Errorf("no results: err = %v, want ErrNoMatch", err)
	}
}

func TestBearerToken(t *testing.T) {
	f := newFake(t)
	c, _ := f.client(t, strings.Repeat("x", 120)) // v4 read access tokens are long JWTs
	if _, err := c.Movie(context.Background(), "The Matrix", ""); err != nil {
		t.Fatal(err)
	}
	if f.lastAuth.Load() != "bearer" {
		t.Errorf("a long v4 token must be sent as a bearer token, got %v", f.lastAuth.Load())
	}
}

func TestDisabledWithoutKey(t *testing.T) {
	c, _ := newFake(t).client(t, "")
	if c.Enabled() {
		t.Error("Enabled() with no key")
	}
	if _, err := c.Movie(context.Background(), "The Matrix", ""); !errors.Is(err, ErrDisabled) {
		t.Errorf("err = %v, want ErrDisabled", err)
	}
}

// A poster that fails to download is logged and skipped; the rest of the
// lookup is still returned.
func TestPosterFailureKeepsTheRest(t *testing.T) {
	f := newFake(t)
	f.posterFails = true
	c, dir := f.client(t, "key")
	info, err := c.Movie(context.Background(), "The Matrix", "")
	if err != nil || info.Title != "The Matrix" || info.Poster != "" {
		t.Errorf("info = %+v, err = %v; want the movie without a poster", info, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("a failed download left files behind: %v", entries)
	}
	if _, err := c.fetchPoster(context.Background(), "/matrix.jpg", "movie-603"); err == nil {
		t.Error("fetchPoster should report the HTTP error")
	}
}

func TestFetchPosterReusesExistingFile(t *testing.T) {
	f := newFake(t)
	c, dir := f.client(t, "key")
	os.WriteFile(filepath.Join(dir, "tmdb-movie-603.jpg"), []byte("cached"), 0o644)
	f.posterFails = true // a request would fail; there must be none
	file, err := c.fetchPoster(context.Background(), "/matrix.jpg", "movie-603")
	if err != nil || file != "tmdb-movie-603.jpg" {
		t.Errorf("file = %q, err = %v", file, err)
	}
	if file, err := c.fetchPoster(context.Background(), "", "movie-1"); file != "" || err != nil {
		t.Errorf("no poster: %q, %v; want empty and no error", file, err)
	}
}

// Concurrent lookups of the same show share one request, and later ones hit
// the cache.
func TestShowLookupsCoalesce(t *testing.T) {
	f := newFake(t)
	c, _ := f.client(t, "key")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if info, err := c.Show(context.Background(), "Cosmos", ""); err != nil || info.ID != 1399 {
				t.Errorf("Show = %+v, %v", info, err)
			}
		}()
	}
	wg.Wait()
	c.Show(context.Background(), "COSMOS", "")
	if n := f.searches.Load(); n != 1 {
		t.Errorf("%d searches for one show, want 1", n)
	}
	ep, err := c.Episode(context.Background(), 1399, 1, 2)
	if err != nil || ep.Title != "Some of the Things That Molecules Do" || ep.Year != "2014" {
		t.Errorf("Episode = %+v, %v", ep, err)
	}
}

// A season's episode list is fetched once and then served from the cache
// until it expires.
func TestSeasonIsCached(t *testing.T) {
	f := newFake(t)
	c, _ := f.client(t, "key")
	clock := time.Now()
	c.now = func() time.Time { return clock }
	for i := 0; i < 3; i++ {
		eps, err := c.Season(context.Background(), 1399, 1)
		if err != nil || len(eps) != 2 || eps[1].Number != 2 || eps[1].Title != "Some of the Things That Molecules Do" || eps[1].Year != "2014" {
			t.Fatalf("Season = %+v, %v", eps, err)
		}
	}
	if n := f.seasons.Load(); n != 1 {
		t.Errorf("fetched the season %d times, want 1", n)
	}
	clock = clock.Add(2 * seasonTTL)
	c.Season(context.Background(), 1399, 1)
	if n := f.seasons.Load(); n != 2 {
		t.Errorf("after expiry: fetched %d times, want 2", n)
	}
}

func TestCollectionLookup(t *testing.T) {
	f := newFake(t)
	c, dir := f.client(t, "key")
	info, err := c.Collection(context.Background(), "Star Trek")
	if err != nil || info.Title != "Star Trek: The Original Series Collection" || info.Year != "" || info.Poster == "" {
		t.Fatalf("Collection = %+v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(dir, info.Poster)); err != nil {
		t.Errorf("poster not saved: %v", err)
	}
}

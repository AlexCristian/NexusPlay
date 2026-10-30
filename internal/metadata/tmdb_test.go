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
	tmdb := &fakeTMDB{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/search/movie", func(w http.ResponseWriter, r *http.Request) {
		tmdb.note(r)
		if r.URL.Query().Get("query") == "Nothing" {
			io.WriteString(w, `{"results": []}`)
			return
		}
		io.WriteString(w, `{"results": [{"id": 603, "title": "The Matrix", "release_date": "1999-03-31",
			"overview": "A hacker learns the truth.", "poster_path": "/matrix.jpg"}]}`)
	})
	mux.HandleFunc("/api/search/tv", func(w http.ResponseWriter, r *http.Request) {
		tmdb.note(r)
		tmdb.searches.Add(1)
		time.Sleep(50 * time.Millisecond) // long enough for concurrent lookups to overlap
		io.WriteString(w, `{"results": [{"id": 1399, "name": "Cosmos", "first_air_date": "2014-03-09", "poster_path": "/cosmos.png"}]}`)
	})
	mux.HandleFunc("/api/tv/1399/season/1/episode/2", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"name": "Some of the Things That Molecules Do", "air_date": "2014-03-16"}`)
	})
	mux.HandleFunc("/api/tv/1399/season/1", func(w http.ResponseWriter, r *http.Request) {
		tmdb.seasons.Add(1)
		io.WriteString(w, `{"episodes": [
			{"episode_number": 1, "name": "Standing Up in the Milky Way", "air_date": "2014-03-09", "overview": "One."},
			{"episode_number": 2, "name": "Some of the Things That Molecules Do", "air_date": "2014-03-16", "overview": "Two."}]}`)
	})
	mux.HandleFunc("/api/search/collection", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"results": [{"id": 115575, "name": "Star Trek: The Original Series Collection",
			"overview": "Films.", "poster_path": "/st.jpg"}]}`)
	})
	mux.HandleFunc("/img/", func(w http.ResponseWriter, r *http.Request) {
		if tmdb.posterFails {
			http.Error(w, "gone", http.StatusInternalServerError)
			return
		}
		io.WriteString(w, "image bytes for "+r.URL.Path)
	})
	tmdb.srv = httptest.NewServer(mux)
	t.Cleanup(tmdb.srv.Close)
	return tmdb
}

func (fakeTMDB *fakeTMDB) note(r *http.Request) {
	if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
		fakeTMDB.lastAuth.Store("bearer")
	} else if r.URL.Query().Get("api_key") != "" {
		fakeTMDB.lastAuth.Store("key")
	}
}

func (fakeTMDB *fakeTMDB) client(t *testing.T, apiKey string) (*Client, string) {
	directory := t.TempDir()
	client := New(Config{APIKey: apiKey, PosterDirectory: directory}, slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithBaseURL(fakeTMDB.srv.URL+"/api", fakeTMDB.srv.URL+"/img"))
	return client, directory
}

func TestMovieLookup(t *testing.T) {
	fakeTMDB := newFake(t)
	client, directory := fakeTMDB.client(t, "short-v3-key")
	info, err := client.Movie(context.Background(), "The Matrix", "1999")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != 603 || info.Year != "1999" || info.Poster != "tmdb-movie-603.jpg" {
		t.Errorf("info = %+v", info)
	}
	if content, _ := os.ReadFile(filepath.Join(directory, info.Poster)); string(content) != "image bytes for /img/matrix.jpg" {
		t.Errorf("poster file = %q", content)
	}
	if fakeTMDB.lastAuth.Load() != "key" {
		t.Errorf("a short v3 key must go in the query string, got %v", fakeTMDB.lastAuth.Load())
	}
	if _, err := client.Movie(context.Background(), "Nothing", ""); !errors.Is(err, ErrNoMatch) {
		t.Errorf("no results: err = %v, want ErrNoMatch", err)
	}
}

func TestBearerToken(t *testing.T) {
	fakeTMDB := newFake(t)
	client, _ := fakeTMDB.client(t, strings.Repeat("x", 120)) // v4 read access tokens are long JWTs
	if _, err := client.Movie(context.Background(), "The Matrix", ""); err != nil {
		t.Fatal(err)
	}
	if fakeTMDB.lastAuth.Load() != "bearer" {
		t.Errorf("a long v4 token must be sent as a bearer token, got %v", fakeTMDB.lastAuth.Load())
	}
}

func TestDisabledWithoutKey(t *testing.T) {
	client, _ := newFake(t).client(t, "")
	if client.Enabled() {
		t.Error("Enabled() with no key")
	}
	if _, err := client.Movie(context.Background(), "The Matrix", ""); !errors.Is(err, ErrDisabled) {
		t.Errorf("err = %v, want ErrDisabled", err)
	}
}

// A poster that fails to download is logged and skipped; the rest of the
// lookup is still returned.
func TestPosterFailureKeepsTheRest(t *testing.T) {
	fakeTMDB := newFake(t)
	fakeTMDB.posterFails = true
	client, directory := fakeTMDB.client(t, "key")
	info, err := client.Movie(context.Background(), "The Matrix", "")
	if err != nil || info.Title != "The Matrix" || info.Poster != "" {
		t.Errorf("info = %+v, err = %v; want the movie without a poster", info, err)
	}
	if entries, _ := os.ReadDir(directory); len(entries) != 0 {
		t.Errorf("a failed download left files behind: %v", entries)
	}
	if _, err := client.fetchPoster(context.Background(), "/matrix.jpg", "movie-603"); err == nil {
		t.Error("fetchPoster should report the HTTP error")
	}
}

func TestFetchPosterReusesExistingFile(t *testing.T) {
	fakeTMDB := newFake(t)
	client, directory := fakeTMDB.client(t, "key")
	os.WriteFile(filepath.Join(directory, "tmdb-movie-603.jpg"), []byte("cached"), 0o644)
	fakeTMDB.posterFails = true // a request would fail; there must be none
	file, err := client.fetchPoster(context.Background(), "/matrix.jpg", "movie-603")
	if err != nil || file != "tmdb-movie-603.jpg" {
		t.Errorf("file = %q, err = %v", file, err)
	}
	if file, err := client.fetchPoster(context.Background(), "", "movie-1"); file != "" || err != nil {
		t.Errorf("no poster: %q, %v; want empty and no error", file, err)
	}
}

// Concurrent lookups of the same show share one request, and later ones hit
// the cache.
func TestShowLookupsCoalesce(t *testing.T) {
	fakeTMDB := newFake(t)
	client, _ := fakeTMDB.client(t, "key")
	var waitGroup sync.WaitGroup
	for i := 0; i < 8; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			if info, err := client.Show(context.Background(), "Cosmos", ""); err != nil || info.ID != 1399 {
				t.Errorf("Show = %+v, %v", info, err)
			}
		}()
	}
	waitGroup.Wait()
	client.Show(context.Background(), "COSMOS", "")
	if searches := fakeTMDB.searches.Load(); searches != 1 {
		t.Errorf("%d searches for one show, want 1", searches)
	}
	episodeInfo, err := client.Episode(context.Background(), 1399, 1, 2)
	if err != nil || episodeInfo.Title != "Some of the Things That Molecules Do" || episodeInfo.Year != "2014" {
		t.Errorf("Episode = %+v, %v", episodeInfo, err)
	}
}

// A season's episode list is fetched once and then served from the cache
// until it expires.
func TestSeasonIsCached(t *testing.T) {
	fakeTMDB := newFake(t)
	client, _ := fakeTMDB.client(t, "key")
	clock := time.Now()
	client.now = func() time.Time { return clock }
	for i := 0; i < 3; i++ {
		episodes, err := client.Season(context.Background(), 1399, 1)
		if err != nil || len(episodes) != 2 || episodes[1].Number != 2 || episodes[1].Title != "Some of the Things That Molecules Do" || episodes[1].Year != "2014" {
			t.Fatalf("Season = %+v, %v", episodes, err)
		}
	}
	if requests := fakeTMDB.seasons.Load(); requests != 1 {
		t.Errorf("fetched the season %d times, want 1", requests)
	}
	clock = clock.Add(2 * seasonTTL)
	client.Season(context.Background(), 1399, 1)
	if requests := fakeTMDB.seasons.Load(); requests != 2 {
		t.Errorf("after expiry: fetched %d times, want 2", requests)
	}
}

func TestCollectionLookup(t *testing.T) {
	fakeTMDB := newFake(t)
	client, directory := fakeTMDB.client(t, "key")
	info, err := client.Collection(context.Background(), "Star Trek")
	if err != nil || info.Title != "Star Trek: The Original Series Collection" || info.Year != "" || info.Poster == "" {
		t.Fatalf("Collection = %+v, %v", info, err)
	}
	if _, err := os.Stat(filepath.Join(directory, info.Poster)); err != nil {
		t.Errorf("poster not saved: %v", err)
	}
}

func TestRequestErrorsHideTheAPIKey(t *testing.T) {
	client := New(Config{APIKey: "secret-v3-key"}, slog.New(slog.NewTextHandler(io.Discard, nil)),
		WithBaseURL("http://127.0.0.1:1", "http://127.0.0.1:1"))
	_, err := client.Movie(context.Background(), "The Matrix", "")
	if err == nil {
		t.Fatal("expected a connection error")
	}
	if strings.Contains(err.Error(), "secret-v3-key") {
		t.Errorf("error leaks the API key: %v", err)
	}
}

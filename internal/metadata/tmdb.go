// Package metadata looks up titles, overviews and posters on TMDB
// (themoviedb.org). Everything here is optional: without an API key the
// client reports ErrDisabled and NexusPlay falls back to file names.
package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ErrDisabled means no API key is configured.
var ErrDisabled = errors.New("metadata lookups are disabled")

// ErrNoMatch means the search returned nothing.
var ErrNoMatch = errors.New("no match")

// Info is what a lookup returns. Poster is a file name in the poster directory.
type Info struct {
	ID       int
	Title    string
	Year     string
	Overview string
	Poster   string
}

// Client talks to the TMDB v3 API.
type Client struct {
	apiKey    string
	posterDir string
	http      *http.Client
	baseURL   string
	imageURL  string

	mu    sync.Mutex
	shows map[string]showEntry // cache of show-name lookups made while scanning
}

type showEntry struct {
	info Info
	err  error
}

// New returns a client. An empty apiKey disables lookups.
func New(apiKey, posterDir string) *Client {
	return &Client{
		apiKey:    strings.TrimSpace(apiKey),
		posterDir: posterDir,
		http:      &http.Client{Timeout: 15 * time.Second},
		baseURL:   "https://api.themoviedb.org/3",
		imageURL:  "https://image.tmdb.org/t/p/w342",
		shows:     map[string]showEntry{},
	}
}

// Enabled reports whether an API key is configured.
func (c *Client) Enabled() bool { return c.apiKey != "" }

// Movie searches for a film by title and optional year.
func (c *Client) Movie(ctx context.Context, title, year string) (Info, error) {
	q := url.Values{"query": {title}}
	if year != "" {
		q.Set("year", year)
	}
	var res struct {
		Results []struct {
			ID          int    `json:"id"`
			Title       string `json:"title"`
			ReleaseDate string `json:"release_date"`
			Overview    string `json:"overview"`
			PosterPath  string `json:"poster_path"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/search/movie", q, &res); err != nil {
		return Info{}, err
	}
	if len(res.Results) == 0 {
		return Info{}, ErrNoMatch
	}
	r := res.Results[0]
	info := Info{ID: r.ID, Title: r.Title, Year: yearOf(r.ReleaseDate), Overview: r.Overview}
	info.Poster = c.downloadPoster(ctx, r.PosterPath, "movie-"+strconv.Itoa(r.ID))
	return info, nil
}

// Show searches for a TV series. Results are cached by name for the life of
// the process, so scanning a season does one search, not one per episode.
func (c *Client) Show(ctx context.Context, name, year string) (Info, error) {
	key := strings.ToLower(name) + "|" + year
	c.mu.Lock()
	if e, ok := c.shows[key]; ok {
		c.mu.Unlock()
		return e.info, e.err
	}
	c.mu.Unlock()

	info, err := c.show(ctx, name, year)
	if err == nil || errors.Is(err, ErrNoMatch) {
		c.mu.Lock()
		c.shows[key] = showEntry{info, err}
		c.mu.Unlock()
	}
	return info, err
}

func (c *Client) show(ctx context.Context, name, year string) (Info, error) {
	q := url.Values{"query": {name}}
	if year != "" {
		q.Set("first_air_date_year", year)
	}
	var res struct {
		Results []struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			FirstAirDate string `json:"first_air_date"`
			Overview     string `json:"overview"`
			PosterPath   string `json:"poster_path"`
		} `json:"results"`
	}
	if err := c.get(ctx, "/search/tv", q, &res); err != nil {
		return Info{}, err
	}
	if len(res.Results) == 0 {
		return Info{}, ErrNoMatch
	}
	r := res.Results[0]
	info := Info{ID: r.ID, Title: r.Name, Year: yearOf(r.FirstAirDate), Overview: r.Overview}
	info.Poster = c.downloadPoster(ctx, r.PosterPath, "tv-"+strconv.Itoa(r.ID))
	return info, nil
}

// Episode looks up one episode of a show found with Show.
func (c *Client) Episode(ctx context.Context, showID, season, episode int) (Info, error) {
	var res struct {
		Name     string `json:"name"`
		AirDate  string `json:"air_date"`
		Overview string `json:"overview"`
	}
	p := fmt.Sprintf("/tv/%d/season/%d/episode/%d", showID, season, episode)
	if err := c.get(ctx, p, url.Values{}, &res); err != nil {
		return Info{}, err
	}
	return Info{Title: res.Name, Year: yearOf(res.AirDate), Overview: res.Overview}, nil
}

func (c *Client) get(ctx context.Context, p string, q url.Values, out any) error {
	if !c.Enabled() {
		return ErrDisabled
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+p, nil)
	if err != nil {
		return err
	}
	// TMDB issues two kinds of credentials: a short v3 API key passed as a
	// query parameter, and a long v4 read access token sent as a bearer token.
	if len(c.apiKey) > 40 {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	} else {
		q.Set("api_key", c.apiKey)
	}
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNoMatch
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("tmdb %s: %s", p, resp.Status)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

// downloadPoster saves a poster once and returns its file name, or "" on failure.
func (c *Client) downloadPoster(ctx context.Context, posterPath, name string) string {
	if posterPath == "" {
		return ""
	}
	file := "tmdb-" + name + filepath.Ext(posterPath)
	dest := filepath.Join(c.posterDir, file)
	if _, err := os.Stat(dest); err == nil {
		return file
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.imageURL+posterPath, nil)
	if err != nil {
		return ""
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	tmp, err := os.CreateTemp(c.posterDir, ".poster-*")
	if err != nil {
		return ""
	}
	_, err = io.Copy(tmp, io.LimitReader(resp.Body, 10<<20))
	tmp.Close()
	if err != nil {
		os.Remove(tmp.Name())
		return ""
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		os.Remove(tmp.Name())
		return ""
	}
	return file
}

func yearOf(date string) string {
	if len(date) >= 4 {
		return date[:4]
	}
	return ""
}

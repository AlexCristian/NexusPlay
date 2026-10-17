// Package metadata looks up titles, overviews and posters on TMDB
// (themoviedb.org). Everything here is optional: without an API key the
// client reports ErrDisabled and NexusPlay falls back to file names.
//
// A Client is safe for concurrent use.
package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// ErrDisabled means no API key is configured.
var ErrDisabled = errors.New("metadata lookups are disabled")

// ErrNoMatch means the search returned nothing.
var ErrNoMatch = errors.New("no match")

// Info is what a lookup returns. Poster is a file name in the poster
// directory, or "" when TMDB has no poster or it could not be downloaded.
type Info struct {
	ID       int
	Title    string
	Year     string
	Overview string
	Poster   string
}

// Config configures a Client.
type Config struct {
	APIKey    string // a v3 API key or a v4 read access token; "" disables lookups
	PosterDir string // where downloaded posters are saved
}

// Option overrides a default. Options exist for tests, which point the client
// at a local server.
type Option func(*Client)

// WithBaseURL sends API requests to apiURL and poster downloads to imageURL.
func WithBaseURL(apiURL, imageURL string) Option {
	return func(c *Client) { c.baseURL, c.imageURL = apiURL, imageURL }
}

// WithHTTPClient replaces the HTTP client (which has a 15 second timeout).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// Client talks to the TMDB v3 API.
type Client struct {
	apiKey    string
	posterDir string
	log       *slog.Logger
	http      *http.Client
	baseURL   string
	imageURL  string

	// Show lookups are cached for the life of the process, so scanning a
	// season does one search, not one per episode. Concurrent lookups of the
	// same show (a scan and an admin save) share one request.
	lookups singleflight.Group
	mu      sync.Mutex
	shows   map[string]showEntry
}

type showEntry struct {
	info Info
	err  error
}

// New returns a client. An empty API key disables lookups.
func New(cfg Config, log *slog.Logger, opts ...Option) *Client {
	c := &Client{
		apiKey:    strings.TrimSpace(cfg.APIKey),
		posterDir: cfg.PosterDir,
		log:       log,
		http:      &http.Client{Timeout: 15 * time.Second},
		baseURL:   "https://api.themoviedb.org/3",
		imageURL:  "https://image.tmdb.org/t/p/w342",
		shows:     map[string]showEntry{},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// Enabled reports whether an API key is configured.
func (c *Client) Enabled() bool { return c.apiKey != "" }

// Movie searches for a film by title and optional year. It returns
// ErrNoMatch when TMDB knows no such film and ErrDisabled without an API key.
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
	info.Poster = c.posterOrEmpty(ctx, r.PosterPath, "movie-"+strconv.Itoa(r.ID))
	return info, nil
}

// Show searches for a TV series by name and optional first-air year. It
// returns ErrNoMatch or ErrDisabled like Movie. Successes and ErrNoMatch are
// cached; network errors are not, so the next call tries again.
func (c *Client) Show(ctx context.Context, name, year string) (Info, error) {
	key := strings.ToLower(name) + "|" + year
	c.mu.Lock()
	e, ok := c.shows[key]
	c.mu.Unlock()
	if ok {
		return e.info, e.err
	}
	v, err, _ := c.lookups.Do(key, func() (any, error) {
		info, err := c.searchShow(ctx, name, year)
		if err == nil || errors.Is(err, ErrNoMatch) {
			c.mu.Lock()
			c.shows[key] = showEntry{info, err}
			c.mu.Unlock()
		}
		return info, err
	})
	return v.(Info), err
}

func (c *Client) searchShow(ctx context.Context, name, year string) (Info, error) {
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
	info.Poster = c.posterOrEmpty(ctx, r.PosterPath, "tv-"+strconv.Itoa(r.ID))
	return info, nil
}

// Episode looks up one episode of a show found with Show. The result has no
// poster: episodes use their show's.
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

// get calls the TMDB API and decodes the JSON response into out. A 404 is
// ErrNoMatch.
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

// posterOrEmpty downloads a poster, logging and returning "" on failure: the
// poster is optional, and the rest of the lookup is still worth keeping.
func (c *Client) posterOrEmpty(ctx context.Context, posterPath, name string) string {
	file, err := c.fetchPoster(ctx, posterPath, name)
	if err != nil {
		c.log.Warn("poster download failed", "poster", posterPath, "err", err)
		return ""
	}
	return file
}

// fetchPoster saves a poster once and returns its file name in the poster
// directory. It returns ("", nil) when TMDB has no poster (posterPath is
// empty) and reuses a file downloaded earlier. The file appears atomically,
// so a half-written poster is never served.
func (c *Client) fetchPoster(ctx context.Context, posterPath, name string) (string, error) {
	if posterPath == "" {
		return "", nil
	}
	file := "tmdb-" + name + filepath.Ext(posterPath)
	dest := filepath.Join(c.posterDir, file)
	if _, err := os.Stat(dest); err == nil {
		return file, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.imageURL+posterPath, nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("poster %s: %s", posterPath, resp.Status)
	}
	tmp, err := os.CreateTemp(c.posterDir, ".poster-*")
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(tmp, io.LimitReader(resp.Body, 10<<20))
	closeErr := tmp.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("saving poster %s: %w", posterPath, err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("saving poster %s: %w", posterPath, err)
	}
	return file, nil
}

func yearOf(date string) string {
	if len(date) >= 4 {
		return date[:4]
	}
	return ""
}

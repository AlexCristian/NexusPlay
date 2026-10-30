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

// [Client] talks to the TMDB v3 API. Create one with [New].
type Client struct {
	// [apiKey] is the v3 API key or v4 read access token, or "" when lookups
	// are disabled.
	apiKey string
	// [posterDirectory] is the folder downloaded posters are saved in.
	posterDirectory string
	// [log] records poster downloads that failed.
	log *slog.Logger
	// [http] sends every request, to the API and for posters.
	http *http.Client
	// [baseURL] is the start of every API request's URL.
	baseURL string
	// [imageURL] is the start of every poster download's URL.
	imageURL string

	// [lookups] makes concurrent [Client.Show] and [Client.Season] calls for
	// the same show or season (a scan and an admin save) share one request.
	// Together with the [shows] cache, scanning a season does one show
	// search, not one per episode.
	lookups singleflight.Group
	// [mutex] guards [shows] and [seasons].
	mutex sync.Mutex
	// [shows] caches [Client.Show] results by lowercase name and year.
	shows map[string]showEntry
	// [seasons] caches [Client.Season] results by show ID and season.
	seasons map[string]seasonEntry
	// [now] tells the time, for expiring cached seasons. Tests replace it.
	now func() time.Time
}

// [seasonEntry] is a cached season episode list. Seasons of running shows
// gain episodes, so entries expire after [seasonTTL].
type seasonEntry struct {
	// [episodes] is the season's episode list as [Client.Season] returns it.
	episodes []Episode
	// [fetched] is when [episodes] was fetched from TMDB.
	fetched time.Time
}

// [seasonTTL] is how long a cached season episode list is used before it is
// fetched again.
const seasonTTL = time.Hour

// [showEntry] is a cached [Client.Show] result: either a match, or
// [ErrNoMatch].
type showEntry struct {
	// [info] is the matched show, or a zero [Info] if [err] is not [nil].
	info Info
	// [err] is [nil] for a match or [ErrNoMatch] when TMDB knows no such
	// show. Other errors are never cached.
	err error
}

// [New] creates a TMDB client.
//
// Parameters:
//   - [config]: the API key and poster folder. A blank [Config.APIKey]
//     disables lookups.
//   - [log]: where failed poster downloads are logged.
//   - [options]: overrides for the defaults, such as [WithBaseURL]. Only
//     tests need them.
//
// Returns:
//   - [*Client]: the new client, ready to use.
func New(config Config, log *slog.Logger, options ...Option) *Client {
	client := &Client{
		apiKey:          strings.TrimSpace(config.APIKey),
		posterDirectory: config.PosterDirectory,
		log:             log,
		http:            &http.Client{Timeout: 15 * time.Second},
		baseURL:         "https://api.themoviedb.org/3",
		imageURL:        "https://image.tmdb.org/t/p/w342",
		shows:           map[string]showEntry{},
		seasons:         map[string]seasonEntry{},
		now:             time.Now,
	}
	for _, option := range options {
		option(client)
	}
	return client
}

// [Enabled] reports whether lookups are turned on, which they are only when an
// API key is configured.
//
// Returns:
//   - [bool]: [true] if [Config.APIKey] was not blank, [false] otherwise.
func (client *Client) Enabled() bool { return client.apiKey != "" }

// [Movie] searches TMDB for a film and downloads its poster.
//
// Parameters:
//   - [ctx]: cancels the search and the poster download.
//   - [title]: the film's title, such as "The Matrix".
//   - [year]: the release year, such as "1999", to tell apart films with the
//     same title, or "" to search every year.
//
// Returns:
//   - [Info]: the first search result, or a zero [Info] if [error] is not
//     [nil]. [Info.Poster] is "" if TMDB has no poster or the download
//     failed; a failed download is logged, not returned.
//   - [error]: [nil] on success, [ErrNoMatch] if TMDB knows no such film,
//     [ErrDisabled] if no API key is configured, and otherwise the network
//     or decoding error.
func (client *Client) Movie(ctx context.Context, title, year string) (Info, error) {
	query := url.Values{"query": {title}}
	if year != "" {
		query.Set("year", year)
	}
	var response struct {
		Results []struct {
			ID          int    `json:"id"`
			Title       string `json:"title"`
			ReleaseDate string `json:"release_date"`
			Overview    string `json:"overview"`
			PosterPath  string `json:"poster_path"`
		} `json:"results"`
	}
	if err := client.get(ctx, "/search/movie", query, &response); err != nil {
		return Info{}, err
	}
	if len(response.Results) == 0 {
		return Info{}, ErrNoMatch
	}
	result := response.Results[0]
	info := Info{ID: result.ID, Title: result.Title, Year: yearOf(result.ReleaseDate), Overview: result.Overview}
	info.Poster = client.posterOrEmpty(ctx, result.PosterPath, "movie-"+strconv.Itoa(result.ID))
	return info, nil
}

// [Show] searches TMDB for a TV series and downloads its poster. Matches and
// [ErrNoMatch] are cached for the life of the process; other errors are not,
// so the next call tries again. Concurrent calls for the same show share one
// request.
//
// Parameters:
//   - [ctx]: cancels the search and the poster download.
//   - [name]: the show's name, such as "Star Trek: Deep Space Nine". Case
//     does not matter.
//   - [year]: the year the show first aired, such as "1993", to tell apart
//     shows with the same name, or "" to search every year.
//
// Returns:
//   - [Info]: the first search result, or a zero [Info] if [error] is not
//     [nil]. [Info.Poster] is "" if TMDB has no poster or the download
//     failed.
//   - [error]: [nil] on success, [ErrNoMatch] if TMDB knows no such show,
//     [ErrDisabled] if no API key is configured, and otherwise the network
//     or decoding error.
func (client *Client) Show(ctx context.Context, name, year string) (Info, error) {
	key := strings.ToLower(name) + "|" + year
	client.mutex.Lock()
	entry, cached := client.shows[key]
	client.mutex.Unlock()
	if cached {
		return entry.info, entry.err
	}
	result, err, _ := client.lookups.Do(key, func() (any, error) {
		info, err := client.searchShow(ctx, name, year)
		if err == nil || errors.Is(err, ErrNoMatch) {
			client.mutex.Lock()
			client.shows[key] = showEntry{info, err}
			client.mutex.Unlock()
		}
		return info, err
	})
	return result.(Info), err
}

// [searchShow] is [Client.Show] without the cache: it always asks TMDB.
//
// Parameters:
//   - [ctx]: cancels the search and the poster download.
//   - [name]: the show's name.
//   - [year]: the year the show first aired, or "" to search every year.
//
// Returns:
//   - [Info]: the first search result, or a zero [Info] if [error] is not
//     [nil].
//   - [error]: [nil] on success, [ErrNoMatch] if TMDB knows no such show,
//     [ErrDisabled] if no API key is configured, and otherwise the network
//     or decoding error.
func (client *Client) searchShow(ctx context.Context, name, year string) (Info, error) {
	query := url.Values{"query": {name}}
	if year != "" {
		query.Set("first_air_date_year", year)
	}
	var response struct {
		Results []struct {
			ID           int    `json:"id"`
			Name         string `json:"name"`
			FirstAirDate string `json:"first_air_date"`
			Overview     string `json:"overview"`
			PosterPath   string `json:"poster_path"`
		} `json:"results"`
	}
	if err := client.get(ctx, "/search/tv", query, &response); err != nil {
		return Info{}, err
	}
	if len(response.Results) == 0 {
		return Info{}, ErrNoMatch
	}
	result := response.Results[0]
	info := Info{ID: result.ID, Title: result.Name, Year: yearOf(result.FirstAirDate), Overview: result.Overview}
	info.Poster = client.posterOrEmpty(ctx, result.PosterPath, "tv-"+strconv.Itoa(result.ID))
	return info, nil
}

// [Episode] looks up one episode of a show. The result is not cached.
//
// Parameters:
//   - [ctx]: cancels the request.
//   - [showID]: the show's TMDB ID, the [Info.ID] returned by [Client.Show].
//   - [season]: the season number, where 0 is TMDB's "Specials".
//   - [episode]: the episode number within the season in TMDB's numbering,
//     starting at 1.
//
// Returns:
//   - [Info]: the episode's name, air year and overview, or a zero [Info] if
//     [error] is not [nil]. [Info.ID] and [Info.Poster] are always empty:
//     episodes use their show's poster.
//   - [error]: [nil] on success, [ErrNoMatch] if TMDB has no such episode,
//     [ErrDisabled] if no API key is configured, and otherwise the network
//     or decoding error.
func (client *Client) Episode(ctx context.Context, showID, season, episode int) (Info, error) {
	var response struct {
		Name     string `json:"name"`
		AirDate  string `json:"air_date"`
		Overview string `json:"overview"`
	}
	path := fmt.Sprintf("/tv/%d/season/%d/episode/%d", showID, season, episode)
	if err := client.get(ctx, path, url.Values{}, &response); err != nil {
		return Info{}, err
	}
	return Info{Title: response.Name, Year: yearOf(response.AirDate), Overview: response.Overview}, nil
}

// [Collection] searches TMDB for a collection of films and downloads its
// poster.
//
// Parameters:
//   - [ctx]: cancels the search and the poster download.
//   - [name]: the collection's name, such as "Star Trek" or "The Matrix
//     Collection".
//
// Returns:
//   - [Info]: the first search result, or a zero [Info] if [error] is not
//     [nil]. [Info.Year] is always "", because TMDB collections have no
//     year. [Info.Poster] is "" if TMDB has no poster or the download failed.
//   - [error]: [nil] on success, [ErrNoMatch] if TMDB knows no such
//     collection, [ErrDisabled] if no API key is configured, and otherwise
//     the network or decoding error.
func (client *Client) Collection(ctx context.Context, name string) (Info, error) {
	var response struct {
		Results []struct {
			ID         int    `json:"id"`
			Name       string `json:"name"`
			Overview   string `json:"overview"`
			PosterPath string `json:"poster_path"`
		} `json:"results"`
	}
	if err := client.get(ctx, "/search/collection", url.Values{"query": {name}}, &response); err != nil {
		return Info{}, err
	}
	if len(response.Results) == 0 {
		return Info{}, ErrNoMatch
	}
	result := response.Results[0]
	info := Info{ID: result.ID, Title: result.Name, Overview: result.Overview}
	info.Poster = client.posterOrEmpty(ctx, result.PosterPath, "collection-"+strconv.Itoa(result.ID))
	return info, nil
}

// [Season] lists every episode of one season of a show, in TMDB's numbering.
// Lists are cached for [seasonTTL], so matching a whole season's files by
// title costs one request. Concurrent calls for the same season share one
// request.
//
// Parameters:
//   - [ctx]: cancels the request.
//   - [showID]: the show's TMDB ID, the [Info.ID] returned by [Client.Show].
//   - [season]: the season number, where 0 is TMDB's "Specials".
//
// Returns:
//   - [[]Episode]: the season's episodes in the order TMDB lists them, or
//     [nil] if [error] is not [nil]. The slice is shared with the cache, so
//     callers must not modify it. No episode has a poster.
//   - [error]: [nil] on success, [ErrNoMatch] if TMDB has no such season,
//     [ErrDisabled] if no API key is configured, and otherwise the network
//     or decoding error.
func (client *Client) Season(ctx context.Context, showID, season int) ([]Episode, error) {
	key := fmt.Sprintf("%d/%d", showID, season)
	client.mutex.Lock()
	entry, cached := client.seasons[key]
	client.mutex.Unlock()
	if cached && client.now().Sub(entry.fetched) < seasonTTL {
		return entry.episodes, nil
	}
	result, err, _ := client.lookups.Do("season:"+key, func() (any, error) {
		var response struct {
			Episodes []struct {
				Number   int    `json:"episode_number"`
				Name     string `json:"name"`
				AirDate  string `json:"air_date"`
				Overview string `json:"overview"`
			} `json:"episodes"`
		}
		if err := client.get(ctx, fmt.Sprintf("/tv/%d/season/%d", showID, season), url.Values{}, &response); err != nil {
			return []Episode(nil), err
		}
		episodes := make([]Episode, len(response.Episodes))
		for i, listed := range response.Episodes {
			episodes[i] = Episode{Number: listed.Number, Info: Info{Title: listed.Name, Year: yearOf(listed.AirDate), Overview: listed.Overview}}
		}
		client.mutex.Lock()
		client.seasons[key] = seasonEntry{episodes: episodes, fetched: client.now()}
		client.mutex.Unlock()
		return episodes, nil
	})
	return result.([]Episode), err
}

// [get] sends a GET request to the TMDB API and decodes the JSON response.
// Responses larger than 4 MiB are cut off and fail to decode.
//
// Parameters:
//   - [ctx]: cancels the request.
//   - [path]: the API path after [Client.baseURL], such as "/search/movie".
//   - [query]: the URL's query parameters. get modifies it: a v3 API key is
//     added to it as "api_key".
//   - [out]: a pointer to the value the JSON response is decoded into.
//
// Returns:
//   - [error]: [nil] on success, [ErrDisabled] if no API key is configured,
//     [ErrNoMatch] if TMDB answers 404 Not Found, an error naming the status
//     for any other status than 200 OK, and otherwise the network or
//     decoding error.
func (client *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	if !client.Enabled() {
		return ErrDisabled
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.baseURL+path, nil)
	if err != nil {
		return err
	}
	// TMDB issues two kinds of credentials: a short v3 API key passed as a
	// query parameter, and a long v4 read access token sent as a bearer token.
	if len(client.apiKey) > 40 {
		request.Header.Set("Authorization", "Bearer "+client.apiKey)
	} else {
		query.Set("api_key", client.apiKey)
	}
	request.URL.RawQuery = query.Encode()
	request.Header.Set("Accept", "application/json")
	response, err := client.http.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return ErrNoMatch
	}
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("tmdb %s: %s", path, response.Status)
	}
	return json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(out)
}

// [posterOrEmpty] downloads a poster with [Client.fetchPoster], logging any
// failure instead of returning it: the poster is optional, and the rest of
// the lookup is still worth keeping.
//
// Parameters:
//   - [ctx]: cancels the download.
//   - [posterPath]: TMDB's path for the poster image, such as "/abc123.jpg",
//     or "" if TMDB has no poster.
//   - [name]: identifies the title in the saved file's name, such as
//     "movie-603".
//
// Returns:
//   - [string]: the saved file's name in the poster folder, or "" if there
//     is no poster or the download failed.
func (client *Client) posterOrEmpty(ctx context.Context, posterPath, name string) string {
	file, err := client.fetchPoster(ctx, posterPath, name)
	if err != nil {
		client.log.Warn("poster download failed", "poster", posterPath, "err", err)
		return ""
	}
	return file
}

// [fetchPoster] downloads a poster into the poster folder, unless an earlier
// call already did. The file appears atomically, so a half-written poster is
// never served. Posters larger than 10 MiB are cut off.
//
// Parameters:
//   - [ctx]: cancels the download.
//   - [posterPath]: TMDB's path for the poster image, such as "/abc123.jpg",
//     or "" if TMDB has no poster.
//   - [name]: identifies the title in the saved file's name, such as
//     "movie-603".
//
// Returns:
//   - [string]: the file's name in the poster folder, such as
//     "tmdb-movie-603.jpg", or "" if [posterPath] is "" or [error] is not
//     [nil].
//   - [error]: [nil] on success, including when there is no poster or the
//     file already exists. Otherwise an error naming [posterPath], for a
//     failed request, a status other than 200 OK, or a failure to write the
//     file.
func (client *Client) fetchPoster(ctx context.Context, posterPath, name string) (string, error) {
	if posterPath == "" {
		return "", nil
	}
	file := "tmdb-" + name + filepath.Ext(posterPath)
	destination := filepath.Join(client.posterDirectory, file)
	if _, err := os.Stat(destination); err == nil {
		return file, nil
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, client.imageURL+posterPath, nil)
	if err != nil {
		return "", err
	}
	response, err := client.http.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", fmt.Errorf("poster %s: %s", posterPath, response.Status)
	}
	temporaryFile, err := os.CreateTemp(client.posterDirectory, ".poster-*")
	if err != nil {
		return "", err
	}
	_, copyErr := io.Copy(temporaryFile, io.LimitReader(response.Body, 10<<20))
	closeErr := temporaryFile.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		os.Remove(temporaryFile.Name())
		return "", fmt.Errorf("saving poster %s: %w", posterPath, err)
	}
	if err := os.Rename(temporaryFile.Name(), destination); err != nil {
		os.Remove(temporaryFile.Name())
		return "", fmt.Errorf("saving poster %s: %w", posterPath, err)
	}
	return file, nil
}

// [yearOf] takes the year from a TMDB date.
//
// Parameters:
//   - [date]: a date as TMDB writes it, such as "1999-03-31", or "" when
//     TMDB has none.
//
// Returns:
//   - [string]: the first four characters of [date], such as "1999", or ""
//     if [date] is shorter than that.
func yearOf(date string) string {
	if len(date) >= 4 {
		return date[:4]
	}
	return ""
}

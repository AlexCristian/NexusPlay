package metadata

import "net/http"

// [Option] overrides one of a [Client]'s defaults when passed to [New].
// Options exist for tests, which point the client at a local server.
type Option func(*Client)

// [WithBaseURL] makes the client talk to other servers than TMDB's.
//
// Parameters:
//   - [apiURL]: replaces "https://api.themoviedb.org/3" as the start of
//     every API request's URL.
//   - [imageURL]: replaces "https://image.tmdb.org/t/p/w342" as the start of
//     every poster download's URL.
//
// Returns:
//   - [Option]: an option that sets both URLs, for passing to [New].
func WithBaseURL(apiURL, imageURL string) Option {
	return func(client *Client) { client.baseURL, client.imageURL = apiURL, imageURL }
}

// [WithHTTPClient] makes the client send its requests with a different HTTP
// client.
//
// Parameters:
//   - [httpClient]: the HTTP client to use instead of the default, which is
//     an [http.Client] with a 15 second timeout.
//
// Returns:
//   - [Option]: an option that sets the HTTP client, for passing to [New].
func WithHTTPClient(httpClient *http.Client) Option {
	return func(client *Client) { client.http = httpClient }
}

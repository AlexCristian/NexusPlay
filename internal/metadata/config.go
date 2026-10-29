package metadata

// [Config] holds the settings [New] needs to build a [Client].
type Config struct {
	// [APIKey] is a TMDB v3 API key or v4 read access token. A blank key
	// disables lookups: every one returns [ErrDisabled].
	APIKey string
	// [PosterDirectory] is the folder downloaded posters are saved in.
	PosterDirectory string
}

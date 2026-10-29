package metadata

import "errors"

// [ErrDisabled] is returned by every lookup when no API key is configured.
var ErrDisabled = errors.New("metadata lookups are disabled")

// [ErrNoMatch] is returned when a TMDB search finds nothing, or when TMDB
// answers a request with 404 Not Found.
var ErrNoMatch = errors.New("no match")

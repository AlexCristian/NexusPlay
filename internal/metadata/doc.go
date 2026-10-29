// Package [metadata] looks up titles, overviews and posters on TMDB
// (themoviedb.org). Everything here is optional: without an API key the
// [Client] returns [ErrDisabled] and NexusPlay falls back to file names.
//
// A [Client] is safe for concurrent use.
package metadata

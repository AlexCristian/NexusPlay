// Package posters removes poster images nothing uses any more. Posters are
// written by TMDB lookups (metadata) and by uploads in the control panel
// (web), and several rows can share one file, so neither writer deletes a
// file when it replaces one. The Sweeper deletes files that no media or
// collection refers to, once they are old enough that a row may not simply
// be about to refer to them.
package posters

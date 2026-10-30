package files

import "errors"

// [Title] is a method in the wrong file.
func (episode Episode) Title() {} // want "method \\[Episode.Title\\] should be in episode.go or a episode_\\*.go file, with \\[Episode\\]"

// [Clear] is a method of a generic type in the wrong file.
func (list *List[T]) Clear() { list.items = nil } // want "method \\[List.Clear\\] should be in list.go or a list_\\*.go file, with \\[List\\]"

// [describe] is a method of an unexported type, which may be anywhere.
func (helper episodeHelper) describe() {}

// [NewEpisode] is a plain function, which may be anywhere.
//
// Returns:
//   - [Episode]: a new episode.
func NewEpisode() Episode { return Episode{} }

// [ErrMisplaced] is a sentinel error outside errors.go.
var ErrMisplaced = errors.New("misplaced") // want "sentinel error \\[ErrMisplaced\\] should be in errors.go"

// [errQuiet] is unexported, so it may be anywhere.
var errQuiet = errors.New("quiet")

// [ErrorCount] starts with "Err" but is not an error.
var ErrorCount = 3

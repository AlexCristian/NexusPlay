package files

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

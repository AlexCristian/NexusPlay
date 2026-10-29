package files

// [Episode] is in its own file.
type Episode struct{}

// [Season] shares a file with another exported type.
type Season struct{} // want "exported type \\[Season\\] should be in its own file, season.go"

// [episodeHelper] is an unexported helper, allowed next to its type.
type episodeHelper struct{}

// [help] is a method of an unexported type, which may be anywhere.
func (helper episodeHelper) help() {}

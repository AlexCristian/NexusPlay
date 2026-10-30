package metadata

// [Episode] is one entry of a season's episode list, as returned by
// [Client.Season].
type Episode struct {
	// [Number] is the episode's number within its season in TMDB's numbering,
	// starting at 1. It can differ from the number in a file's name, such as
	// when a two-part pilot is one file but two TMDB episodes.
	Number int
	// [Info] holds the episode's name, air year and overview. Its
	// [Info.ID] is always 0 and [Info.Poster] always "". [Info] is embedded,
	// so its fields are used directly: [Episode.Title] is [Episode.Info.Title].
	Info
}

package library

// [Guess] is what a file's path says about its content, as [ParsePath] reads
// it.
type Guess struct {
	// [Title] is the film's title or the episode's title. For an episode whose
	// file name has none, it is made up as "Episode N".
	Title string
	// [Year] is the film's release year or the show's first year, such as
	// "1999", or "" if the path names none.
	Year string
	// [Show] is the show's name for a TV episode ("Unknown show" if the path
	// does not name it), or "" for a film.
	Show string
	// [Season] is an episode's season number, or 0.
	Season int
	// [Episode] is an episode's number within its season, or 0.
	Episode int
	// [EpisodeEnd] is the last episode in a file that holds several, such as 2
	// for "S01E01E02" (a two-part pilot), or 0 for a single episode.
	EpisodeEnd int
}

// [IsEpisode] reports whether the path looked like a TV episode.
//
// Returns:
//   - [bool]: [true] for an episode, [false] for a film.
func (guess Guess) IsEpisode() bool { return guess.Show != "" }

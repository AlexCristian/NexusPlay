package library

// Guess is what a file path says about its content.
type Guess struct {
	Title   string
	Year    string
	Show    string // set for TV episodes
	Season  int
	Episode int
	// EpisodeEnd is the last episode in a file that holds several, such as
	// 2 for "S01E01E02" (a two-part pilot); 0 for a single episode.
	EpisodeEnd int
}

// IsEpisode reports whether the path looked like a TV episode.
func (guess Guess) IsEpisode() bool { return guess.Show != "" }

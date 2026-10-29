package metadata

// [Info] is what a lookup returns: the details NexusPlay shows for a film,
// show, episode or collection.
type Info struct {
	// [ID] is TMDB's ID for the title, or 0 for an episode, which TMDB
	// identifies by its show, season and number instead.
	ID int
	// [Title] is the film's title, or the show's, episode's or collection's
	// name.
	Title string
	// [Year] is the four-digit year it was released or first aired, such as
	// "1999", or "" when TMDB has no date (collections never have one).
	Year string
	// [Overview] is TMDB's plot summary, or "" when it has none.
	Overview string
	// [Poster] is a file name in the poster folder, or "" when TMDB has no
	// poster, the download failed, or the lookup does not fetch posters
	// (episodes use their show's).
	Poster string
}

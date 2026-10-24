package library

import (
	"strconv"
	"strings"
	"unicode"

	"github.com/AlexCristian/NexusPlay/internal/metadata"
)

// Matching episodes by title. TMDB's numbering and a file's can disagree:
// a two-part pilot released as one file may be one TMDB episode or two, so
// every later episode of the season is off by one. When the file name has
// the episode's title, that title decides; the number is the fallback.

// minTitleScore is how similar a file's title and a TMDB episode name must
// be (0 to 1) to count as the same episode.
const minTitleScore = 0.6

// episodeByTitle returns the episode of eps whose name best matches title.
// Ties go to the episode numbered closest to number.
func episodeByTitle(eps []metadata.Episode, title string, number int) (metadata.Episode, bool) {
	want := titleWords(title)
	if len(want) == 0 {
		return metadata.Episode{}, false
	}
	var best metadata.Episode
	bestScore := 0.0
	for _, e := range eps {
		score := similarity(want, titleWords(e.Title))
		closer := abs(e.Number-number) < abs(best.Number-number)
		if score > bestScore || (score == bestScore && score > 0 && closer) {
			best, bestScore = e, score
		}
	}
	return best, bestScore >= minTitleScore
}

// episodeByNumber returns episode number of eps.
func episodeByNumber(eps []metadata.Episode, number int) (metadata.Episode, bool) {
	for _, e := range eps {
		if e.Number == number {
			return e, true
		}
	}
	return metadata.Episode{}, false
}

// fileEpisodeTitle is the episode title from the file name, or "" when the
// name had none (ParsePath then makes up "Episode N").
func fileEpisodeTitle(g Guess) string {
	if g.Title == "Episode "+strconv.Itoa(g.Episode) {
		return ""
	}
	return g.Title
}

// ignoredWords carry no meaning when comparing titles: "Encounter at
// Farpoint, Part 1 and 2" should match "Encounter at Farpoint".
var ignoredWords = map[string]bool{"the": true, "a": true, "an": true, "and": true, "part": true, "pt": true, "of": true}

// titleWords lowercases s and splits it into words, dropping punctuation
// and ignoredWords.
func titleWords(s string) map[string]bool {
	words := map[string]bool{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if !ignoredWords[w] {
			words[w] = true
		}
	}
	return words
}

// similarity is the Dice coefficient of two word sets: 1 when they are the
// same, 0 when they share nothing.
func similarity(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := 0
	for w := range a {
		if b[w] {
			shared++
		}
	}
	return 2 * float64(shared) / float64(len(a)+len(b))
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

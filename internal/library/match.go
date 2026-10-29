package library

import (
	_ "embed"
	"strconv"
	"strings"
	"unicode"

	"github.com/AlexCristian/NexusPlay/internal/collections"
	"github.com/AlexCristian/NexusPlay/internal/metadata"
)

// Matching episodes by title. TMDB's numbering and a file's can disagree:
// a two-part pilot released as one file may be one TMDB episode or two, so
// every later episode of the season is off by one. When the file name has
// the episode's title, that title decides; the number is the fallback.

// [minTitleScore] is how similar a file's title and a TMDB episode name must
// be (0 to 1) to count as the same episode.
const minTitleScore = 0.6

// [episodeByTitle] finds the episode in a season whose TMDB name best matches
// the episode title taken from a file name. Titles are compared as sets of
// words (see [titleWords] and [similarity]), so word order, case and
// punctuation do not matter.
//
// Parameters:
//   - [episodes]: every episode of one season, as returned by
//     [metadata.Client.Season], in any order.
//   - [title]: the episode title from the file name, such as "Encounter at
//     Farpoint, Part 1".
//   - [number]: the episode number from the file name. It only breaks ties:
//     when two episodes score the same, the one numbered closer to [number]
//     wins.
//
// Returns:
//   - [metadata.Episode]: the best-matching episode. Use it only if the
//     [bool] is [true].
//   - [bool]: [true] if the best match scores at least [minTitleScore],
//     [false] if it does not or [title] has no meaningful words.
func episodeByTitle(episodes []metadata.Episode, title string, number int) (metadata.Episode, bool) {
	wantedWords := titleWords(title)
	if wantedWords.Size() == 0 {
		return metadata.Episode{}, false
	}
	var best metadata.Episode
	bestScore := 0.0
	for _, episode := range episodes {
		score := similarity(wantedWords, titleWords(episode.Title))
		closer := absolute(episode.Number-number) < absolute(best.Number-number)
		if score > bestScore || (score == bestScore && score > 0 && closer) {
			best, bestScore = episode, score
		}
	}
	return best, bestScore >= minTitleScore
}

// [episodeByNumber] finds the episode with a given number in a season's
// episode list. It is the fallback when the file name has no episode title
// to match on, or [episodeByTitle] found no match for it.
//
// Parameters:
//   - [episodes]: every episode of one season, as returned by
//     [metadata.Client.Season], in any order.
//   - [number]: the episode number parsed from the file name, starting at 1.
//
// Returns:
//   - [metadata.Episode]: the first episode whose [metadata.Episode.Number]
//     equals [number], or a zero [metadata.Episode] if there is none.
//   - [bool]: [true] if an episode was found, [false] otherwise.
func episodeByNumber(episodes []metadata.Episode, number int) (metadata.Episode, bool) {
	for _, episode := range episodes {
		if episode.Number == number {
			return episode, true
		}
	}
	return metadata.Episode{}, false
}

// [fileEpisodeTitle] gets the episode title that was in a file's name.
//
// Parameters:
//   - [guess]: what [ParsePath] made of the file's path. When the name has
//     no episode title, [ParsePath] makes one up: "Episode N".
//
// Returns:
//   - [string]: [Guess.Title], or "" if it is the made-up "Episode N" title.
func fileEpisodeTitle(guess Guess) string {
	if guess.Title == "Episode "+strconv.Itoa(guess.Episode) {
		return ""
	}
	return guess.Title
}

// [ignoredWordsFile] is the contents of ignored_words.txt, built into the
// program by the compiler. The build fails if the file is missing.
//
//go:embed ignored_words.txt
var ignoredWordsFile string

// [ignoredWords] carry no meaning when comparing titles: "Encounter at
// Farpoint, Part 1 and 2" should match "Encounter at Farpoint". The words
// come from ignored_words.txt.
var ignoredWords = parseWordList(ignoredWordsFile)

// [parseWordList] turns the text of a word list file, such as
// ignored_words.txt, into a [collections.Set] of its words, so callers can
// check whether a word is on the list with [collections.Set.Contains].
//
// Parameters:
//   - [text]: the file's contents, one word per line. Blank lines and lines
//     starting with "#" are skipped, and spaces around each word are
//     trimmed.
//
// Returns:
//   - [collections.Set[string]]: every word on the list, lowercased so
//     lookups are case-insensitive as long as the word looked up is also
//     lowercase.
func parseWordList(text string) collections.Set[string] {
	words := collections.NewSet[string]()
	for _, line := range strings.Split(text, "\n") {
		word := strings.TrimSpace(line)
		if word == "" || strings.HasPrefix(word, "#") {
			continue
		}
		words.Add(strings.ToLower(word))
	}
	return words
}

// [titleWords] splits a title into the words used to compare it with other
// titles.
//
// Parameters:
//   - [title]: any title, such as a file name's episode title or a TMDB
//     episode name.
//
// Returns:
//   - [collections.Set[string]]: the distinct lowercase words of [title].
//     Anything other than a letter or digit separates words and is dropped,
//     as are the filler words in [ignoredWords]. The set is empty when
//     [title] has no meaningful words.
func titleWords(title string) collections.Set[string] {
	words := collections.NewSet[string]()
	for _, word := range strings.FieldsFunc(strings.ToLower(title), func(character rune) bool {
		return !unicode.IsLetter(character) && !unicode.IsDigit(character)
	}) {
		if !ignoredWords.Contains(word) {
			words.Add(word)
		}
	}
	return words
}

// [similarity] scores how alike two titles are, using the Dice coefficient of
// their words: twice the number of shared words, divided by the total number
// of words in both.
//
// Parameters:
//   - [first]: one title's words, as built by [titleWords].
//   - [second]: the other title's words, as built by [titleWords]. The order
//     of [first] and [second] does not matter.
//
// Returns:
//   - [float64]: a score from 0 (no words in common) to 1 (the same words),
//     or 0 if either set is empty.
func similarity(first, second collections.Set[string]) float64 {
	if first.Size() == 0 || second.Size() == 0 {
		return 0
	}
	shared := 0
	for word := range first.All() {
		if second.Contains(word) {
			shared++
		}
	}
	return 2 * float64(shared) / float64(first.Size()+second.Size())
}

// [absolute] gives the absolute value of a whole number.
//
// Parameters:
//   - [number]: any [int].
//
// Returns:
//   - [int]: [number] if it is 0 or more, and -[number] otherwise.
func absolute(number int) int {
	if number < 0 {
		return -number
	}
	return number
}

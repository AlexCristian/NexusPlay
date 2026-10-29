package library

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	// [episodeNumberPattern] matches "S02E03"-style episode numbers. The last
	// group holds the further episode numbers of a file with several:
	// "S01E01E02", "S01E01-E02" or "S01E01-02".
	episodeNumberPattern = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])s(\d{1,2})[\s._\-]*e(\d{1,3})((?:[\s._\-]*e\d{1,3}|-\d{1,3}\b)*)`)
	// [digitsPattern] matches each run of digits, to read the further episode
	// numbers.
	digitsPattern = regexp.MustCompile(`\d+`)
	// [crossPattern] matches "2x03"-style episode numbers.
	crossPattern = regexp.MustCompile(`(?i)(?:^|[\s._\-])(\d{1,2})x(\d{2,3})(?:$|[\s._\-])`)
	// [yearPattern] matches a year from 1900 to 2099 standing on its own, such as
	// "(1999)" or ".2014.".
	yearPattern = regexp.MustCompile(`(?:^|[\s.(\[_\-])((?:19|20)\d{2})(?:$|[\s.)\]_\-])`)
	// [seasonDirectoryPattern] matches season folder names such as "Season 2",
	// "Series 02" or "S2".
	seasonDirectoryPattern = regexp.MustCompile(`(?i)^(?:season|series|s)[\s._\-]*(\d{1,2})$`)
	// [releaseTagPattern] matches release tags such as "1080p", "BluRay" or
	// "x264" and everything after the first one, which file names add after
	// the title.
	releaseTagPattern = regexp.MustCompile(`(?i)[\s._\-\[(](?:480p|576p|720p|1080p|2160p|4k|bluray|blu-ray|brrip|bdrip|web-?dl|webrip|hdtv|dvdrip|x264|x265|h\.?264|h\.?265|hevc|aac|ac3|dts|proper|repack|extended|remastered)(?:[\s._\-\])]|$).*$`)
)

// [ParsePath] guesses what a file is from its path: a TV episode when its
// name has an "S02E03" or "2x03" episode number, otherwise a film.
//
// Parameters:
//   - [relativePath]: the path relative to the media folder, with forward
//     or back slashes, such as "Doctor Who/Season 2/Doctor.Who.S02E03.mkv"
//     or "Movies/The Matrix (1999).mp4".
//
// Returns:
//   - [Guess]: the guessed title, year, and for an episode the show, season
//     and episode numbers.
func ParsePath(relativePath string) Guess {
	relativePath = strings.ReplaceAll(relativePath, "\\", "/")
	directory, file := path.Split(relativePath)
	name := strings.TrimSuffix(file, path.Ext(file))
	match, isEpisode := matchEpisode(name)
	if !isEpisode {
		return parseMovie(name)
	}
	return episodeGuess(match, directory)
}

// [episodeMatch] is what an episode file name says on its own, before its
// folders are taken into account.
type episodeMatch struct {
	// [show] is the text before the episode number, tidied, or "".
	show string
	// [title] is the text after the episode number without release tags,
	// tidied, or "".
	title string
	// [season] is the season number.
	season int
	// [episode] is the episode number.
	episode int
	// [end] is the last episode of a multi-episode file, or 0.
	end int
}

// [episodePatterns] are tried in order to recognise an episode number:
// "S02E03" (and "S02E03E04") first, then "2x03".
var episodePatterns = []*regexp.Regexp{episodeNumberPattern, crossPattern}

// [matchEpisode] reads the episode number, and the text around it, from a
// file name.
//
// Parameters:
//   - [name]: the file name without folder or extension.
//
// Returns:
//   - [episodeMatch]: what the name says, or a zero [episodeMatch] if it has
//     no episode number.
//   - [bool]: [true] if [name] names a TV episode, [false] otherwise.
func matchEpisode(name string) (episodeMatch, bool) {
	for _, pattern := range episodePatterns {
		match := pattern.FindStringSubmatchIndex(name)
		if match == nil {
			continue
		}
		// Atoi cannot fail: the patterns only capture digits.
		season, _ := strconv.Atoi(name[match[2]:match[3]])
		episode, _ := strconv.Atoi(name[match[4]:match[5]])
		matched := episodeMatch{
			show:    clean(name[:match[0]]),
			title:   clean(releaseTagPattern.ReplaceAllString(name[match[1]:], "")),
			season:  season,
			episode: episode,
		}
		if len(match) > 7 && match[6] >= 0 {
			if more := digitsPattern.FindAllString(name[match[6]:match[7]], -1); len(more) > 0 {
				if last, _ := strconv.Atoi(more[len(more)-1]); last > episode {
					matched.end = last
				}
			}
		}
		return matched, true
	}
	return episodeMatch{}, false
}

// [parseMovie] guesses a film's title and year from its file name, after
// removing release tags. The last plausible year (see [plausibleYear]) not
// at the very start is the release year. An earlier one may be part of the
// title ("2001 A Space Odyssey 1968"), and so may a year that has not
// happened yet ("Blade Runner 2049").
//
// Parameters:
//   - [name]: the file name without folder or extension.
//
// Returns:
//   - [Guess]: the title (the whole name, tidied, if nothing else is left)
//     and the year, or "" for none.
func parseMovie(name string) Guess {
	var guess Guess
	trimmed := releaseTagPattern.ReplaceAllString(name, "")
	matches := yearPattern.FindAllStringSubmatchIndex(trimmed, -1)
	for i := len(matches) - 1; i >= 0; i-- {
		match := matches[i]
		if match[0] > 0 && plausibleYear(trimmed[match[2]:match[3]]) {
			guess.Year = trimmed[match[2]:match[3]]
			trimmed = trimmed[:match[0]]
			break
		}
	}
	guess.Title = clean(trimmed)
	if guess.Title == "" {
		guess.Title = clean(name)
	}
	return guess
}

// [episodeGuess] completes an episode's file name match with what its
// folders say. The show's name comes from the file name, or else from the
// nearest folder; the year from either, when they name the same show. A
// season of 0 in the file name (a special) gives way to a season folder.
//
// Parameters:
//   - [match]: what the file name says.
//   - [directory]: the folders above the file, relative to the media folder.
//
// Returns:
//   - [Guess]: the episode's details, with "Unknown show" if no show is
//     named and "Episode N" if the file name has no title.
func episodeGuess(match episodeMatch, directory string) Guess {
	guess := Guess{Title: match.title, Season: match.season, Episode: match.episode, EpisodeEnd: match.end}
	folder, folderSeason := showFromDirectories(directory)
	if guess.Season == 0 { // "S00E05" is a special; a "Season 2" folder knows better
		guess.Season = folderSeason
	}
	folderShow, folderYear := splitYear(folder)
	showName, showYear := splitYear(match.show)
	switch {
	case showName == "":
		guess.Show, guess.Year = folderShow, folderYear
	case strings.EqualFold(showName, folderShow):
		guess.Show, guess.Year = showName, firstNonEmpty(showYear, folderYear)
	default:
		guess.Show, guess.Year = showName, showYear
	}
	if guess.Show == "" {
		guess.Show = "Unknown show"
	}
	if guess.Title == "" {
		guess.Title = "Episode " + strconv.Itoa(guess.Episode)
	}
	return guess
}

// [showFromDirectories] reads the folders above an episode, nearest first:
// "Season 2"-style folders give the season, and the nearest other folder's
// name is the show, returned as it is (it may carry a year, as in "Cosmos
// (2014)").
//
// Parameters:
//   - [directory]: the folders above the file, separated by "/".
//
// Returns:
//   - [string] ([show]): the nearest folder that is not a season folder, or
//     "" if there is none.
//   - [int] ([season]): the nearest season folder's number, or 0.
func showFromDirectories(directory string) (show string, season int) {
	directories := strings.Split(strings.Trim(directory, "/"), "/")
	for i := len(directories) - 1; i >= 0; i-- {
		directory := directories[i]
		if directory == "" {
			continue
		}
		if seasonMatch := seasonDirectoryPattern.FindStringSubmatch(directory); seasonMatch != nil {
			if season == 0 {
				season, _ = strconv.Atoi(seasonMatch[1])
			}
			continue
		}
		return directory, season
	}
	return "", season
}

// [plausibleYear] reports whether a four-digit year could be a release year:
// no later than next year, since films are announced ahead of release.
//
// Parameters:
//   - [year]: the year, such as "1999".
//
// Returns:
//   - [bool]: [true] if it is a number no greater than next year, [false]
//     otherwise.
func plausibleYear(year string) bool {
	yearNumber, err := strconv.Atoi(year)
	return err == nil && yearNumber <= time.Now().Year()+1
}

// [splitYear] splits a show name such as "Cosmos (2014)" or "Cosmos.2014"
// into the name and the year.
//
// Parameters:
//   - [text]: a folder or file name part.
//
// Returns:
//   - [string] ([name]): the name before the year, tidied with [clean].
//   - [string] ([year]): the year, or "" if [text] has no plausible year
//     after some name.
func splitYear(text string) (name, year string) {
	if yearMatch := yearPattern.FindStringSubmatchIndex(text); yearMatch != nil && yearMatch[0] > 0 && plausibleYear(text[yearMatch[2]:yearMatch[3]]) {
		return clean(text[:yearMatch[0]]), text[yearMatch[2]:yearMatch[3]]
	}
	return clean(text), ""
}

// [firstNonEmpty] picks the first of two strings that is not "".
//
// Parameters:
//   - [first]: the preferred value.
//   - [second]: the fallback.
//
// Returns:
//   - [string]: [first] unless it is "", in which case [second].
func firstNonEmpty(first, second string) string {
	if first != "" {
		return first
	}
	return second
}

// [clean] tidies a name taken from a path, such as "The.Big_Show - " into
// "The Big Show". Dots count as spaces only when the text has no spaces.
//
// Parameters:
//   - [text]: the raw text.
//
// Returns:
//   - [string]: [text] with dots (if it has no spaces) and underscores made
//     spaces, spaces, dashes, dots and brackets around it trimmed, and runs
//     of spaces made single.
func clean(text string) string {
	if !strings.Contains(text, " ") {
		text = strings.NewReplacer(".", " ", "_", " ").Replace(text)
	} else {
		text = strings.ReplaceAll(text, "_", " ")
	}
	text = strings.Trim(text, " -.([])")
	return strings.Join(strings.Fields(text), " ")
}

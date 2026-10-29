// Package library discovers media files and keeps the database in step with
// the media folder.
//
// The Scanner walks the folder, probes new videos with ffprobe, adds them as
// unpublished media, imports sidecar subtitles and removes rows whose files
// are gone. ParsePath guesses a title, year, show, season and episode from a
// file's path; the control panel shows that guess for review.
//
// The scanner never publishes anything and never deletes files. It removes
// database rows only for files it could look for and did not find: rows
// under an unreadable folder, or every row when the folder is empty
// (an unmounted drive), are kept.
//
// The scan lock (Exclusive) serialises scans with the converter's file swap.
package library

import (
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"
)

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

var (
	// The last group holds more episode numbers of a file with several:
	// "S01E01E02", "S01E01-E02" or "S01E01-02".
	episodeRe              = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])s(\d{1,2})[\s._\-]*e(\d{1,3})((?:[\s._\-]*e\d{1,3}|-\d{1,3}\b)*)`)
	digitsRe               = regexp.MustCompile(`\d+`)
	crossRe                = regexp.MustCompile(`(?i)(?:^|[\s._\-])(\d{1,2})x(\d{2,3})(?:$|[\s._\-])`)
	yearRe                 = regexp.MustCompile(`(?:^|[\s.(\[_\-])((?:19|20)\d{2})(?:$|[\s.)\]_\-])`)
	seasonDirectoryPattern = regexp.MustCompile(`(?i)^(?:season|series|s)[\s._\-]*(\d{1,2})$`)
	junkRe                 = regexp.MustCompile(`(?i)[\s._\-\[(](?:480p|576p|720p|1080p|2160p|4k|bluray|blu-ray|brrip|bdrip|web-?dl|webrip|hdtv|dvdrip|x264|x265|h\.?264|h\.?265|hevc|aac|ac3|dts|proper|repack|extended|remastered)(?:[\s._\-\])]|$).*$`)
)

// ParsePath guesses title, year and episode numbers from a path relative to
// the media folder, such as "Doctor Who/Season 2/Doctor.Who.S02E03.mkv" or
// "Movies/The Matrix (1999).mp4".
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

// episodeMatch is what an episode file name says on its own.
type episodeMatch struct {
	show    string // text before the episode number; may be empty
	title   string // text after it, without release tags; may be empty
	season  int
	episode int
	end     int // the last episode of a multi-episode file, or 0
}

// episodePatterns recognise "S02E03" (and "S02E03E04") and "2x03".
var episodePatterns = []*regexp.Regexp{episodeRe, crossRe}

// matchEpisode reports whether a file name (without extension) names a TV
// episode.
func matchEpisode(name string) (episodeMatch, bool) {
	for _, pattern := range episodePatterns {
		match := pattern.FindStringSubmatchIndex(name)
		if match == nil {
			continue
		}
		// Atoi cannot fail: the patterns only capture digits.
		season, _ := strconv.Atoi(name[match[2]:match[3]])
		episode, _ := strconv.Atoi(name[match[4]:match[5]])
		episodeGuess := episodeMatch{
			show:    clean(name[:match[0]]),
			title:   clean(junkRe.ReplaceAllString(name[match[1]:], "")),
			season:  season,
			episode: episode,
		}
		if len(match) > 7 && match[6] >= 0 {
			if more := digitsRe.FindAllString(name[match[6]:match[7]], -1); len(more) > 0 {
				if last, _ := strconv.Atoi(more[len(more)-1]); last > episode {
					episodeGuess.end = last
				}
			}
		}
		return episodeGuess, true
	}
	return episodeMatch{}, false
}

// parseMovie guesses a film's title and year. The last plausible year is the
// release year. An earlier one may be part of the title ("2001 A Space
// Odyssey 1968"), and so may a year that has not happened yet ("Blade Runner
// 2049").
func parseMovie(name string) Guess {
	var guess Guess
	trimmed := junkRe.ReplaceAllString(name, "")
	matches := yearRe.FindAllStringSubmatchIndex(trimmed, -1)
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

// episodeGuess completes an episode match with what its folders say. The
// show name comes from the file name, or else from the nearest folder; the
// year from either, when they name the same show.
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

// showFromDirectories reads the folders above an episode, nearest first: "Season
// 2"-style folders give the season, and the nearest other folder's name is
// the show, returned as is (it may carry a year, as in "Cosmos (2014)").
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

// plausibleYear reports whether a four-digit year could be a release year:
// no later than next year (films are announced ahead of release).
func plausibleYear(year string) bool {
	yearNumber, err := strconv.Atoi(year)
	return err == nil && yearNumber <= time.Now().Year()+1
}

// splitYear splits a show name such as "Cosmos (2014)" or "Cosmos.2014"
// into the cleaned name and the year ("" when there is none).
func splitYear(text string) (name, year string) {
	if yearMatch := yearRe.FindStringSubmatchIndex(text); yearMatch != nil && yearMatch[0] > 0 && plausibleYear(text[yearMatch[2]:yearMatch[3]]) {
		return clean(text[:yearMatch[0]]), text[yearMatch[2]:yearMatch[3]]
	}
	return clean(text), ""
}

func firstNonEmpty(first, second string) string {
	if first != "" {
		return first
	}
	return second
}

// clean turns "The.Big_Show - " into "The Big Show".
func clean(text string) string {
	if !strings.Contains(text, " ") {
		text = strings.NewReplacer(".", " ", "_", " ").Replace(text)
	} else {
		text = strings.ReplaceAll(text, "_", " ")
	}
	text = strings.Trim(text, " -.([])")
	return strings.Join(strings.Fields(text), " ")
}

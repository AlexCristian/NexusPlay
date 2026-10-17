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
}

// IsEpisode reports whether the path looked like a TV episode.
func (g Guess) IsEpisode() bool { return g.Show != "" }

var (
	episodeRe = regexp.MustCompile(`(?i)(?:^|[\s._\-\[(])s(\d{1,2})[\s._\-]*e(\d{1,3})(?:[\s._\-]*e\d{1,3})*`)
	crossRe   = regexp.MustCompile(`(?i)(?:^|[\s._\-])(\d{1,2})x(\d{2,3})(?:$|[\s._\-])`)
	yearRe    = regexp.MustCompile(`(?:^|[\s.(\[_\-])((?:19|20)\d{2})(?:$|[\s.)\]_\-])`)
	seasonDir = regexp.MustCompile(`(?i)^(?:season|series|s)[\s._\-]*(\d{1,2})$`)
	junkRe    = regexp.MustCompile(`(?i)[\s._\-\[(](?:480p|576p|720p|1080p|2160p|4k|bluray|blu-ray|brrip|bdrip|web-?dl|webrip|hdtv|dvdrip|x264|x265|h\.?264|h\.?265|hevc|aac|ac3|dts|proper|repack|extended|remastered)(?:[\s._\-\])]|$).*$`)
)

// ParsePath guesses title, year and episode numbers from a path relative to
// the media folder, such as "Doctor Who/Season 2/Doctor.Who.S02E03.mkv" or
// "Movies/The Matrix (1999).mp4".
func ParsePath(rel string) Guess {
	rel = strings.ReplaceAll(rel, "\\", "/")
	dir, file := path.Split(rel)
	name := strings.TrimSuffix(file, path.Ext(file))
	ep, ok := matchEpisode(name)
	if !ok {
		return parseMovie(name)
	}
	return episodeGuess(ep, dir)
}

// episodeMatch is what an episode file name says on its own.
type episodeMatch struct {
	show    string // text before the episode number; may be empty
	title   string // text after it, without release tags; may be empty
	season  int
	episode int
}

// episodePatterns recognise "S02E03" (and "S02E03E04") and "2x03".
var episodePatterns = []*regexp.Regexp{episodeRe, crossRe}

// matchEpisode reports whether a file name (without extension) names a TV
// episode.
func matchEpisode(name string) (episodeMatch, bool) {
	for _, re := range episodePatterns {
		m := re.FindStringSubmatchIndex(name)
		if m == nil {
			continue
		}
		// Atoi cannot fail: the patterns only capture digits.
		season, _ := strconv.Atoi(name[m[2]:m[3]])
		episode, _ := strconv.Atoi(name[m[4]:m[5]])
		return episodeMatch{
			show:    clean(name[:m[0]]),
			title:   clean(junkRe.ReplaceAllString(name[m[1]:], "")),
			season:  season,
			episode: episode,
		}, true
	}
	return episodeMatch{}, false
}

// parseMovie guesses a film's title and year. The last plausible year is the
// release year. An earlier one may be part of the title ("2001 A Space
// Odyssey 1968"), and so may a year that has not happened yet ("Blade Runner
// 2049").
func parseMovie(name string) Guess {
	var g Guess
	trimmed := junkRe.ReplaceAllString(name, "")
	ms := yearRe.FindAllStringSubmatchIndex(trimmed, -1)
	for i := len(ms) - 1; i >= 0; i-- {
		m := ms[i]
		if m[0] > 0 && plausibleYear(trimmed[m[2]:m[3]]) {
			g.Year = trimmed[m[2]:m[3]]
			trimmed = trimmed[:m[0]]
			break
		}
	}
	g.Title = clean(trimmed)
	if g.Title == "" {
		g.Title = clean(name)
	}
	return g
}

// episodeGuess completes an episode match with what its folders say. The
// show name comes from the file name, or else from the nearest folder; the
// year from either, when they name the same show.
func episodeGuess(ep episodeMatch, dir string) Guess {
	g := Guess{Title: ep.title, Season: ep.season, Episode: ep.episode}
	folder, folderSeason := showFromDirs(dir)
	if g.Season == 0 { // "S00E05" is a special; a "Season 2" folder knows better
		g.Season = folderSeason
	}
	folderShow, folderYear := splitYear(folder)
	showName, showYear := splitYear(ep.show)
	switch {
	case showName == "":
		g.Show, g.Year = folderShow, folderYear
	case strings.EqualFold(showName, folderShow):
		g.Show, g.Year = showName, firstNonEmpty(showYear, folderYear)
	default:
		g.Show, g.Year = showName, showYear
	}
	if g.Show == "" {
		g.Show = "Unknown show"
	}
	if g.Title == "" {
		g.Title = "Episode " + strconv.Itoa(g.Episode)
	}
	return g
}

// showFromDirs reads the folders above an episode, nearest first: "Season
// 2"-style folders give the season, and the nearest other folder's name is
// the show, returned as is (it may carry a year, as in "Cosmos (2014)").
func showFromDirs(dir string) (show string, season int) {
	dirs := strings.Split(strings.Trim(dir, "/"), "/")
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		if d == "" {
			continue
		}
		if sm := seasonDir.FindStringSubmatch(d); sm != nil {
			if season == 0 {
				season, _ = strconv.Atoi(sm[1])
			}
			continue
		}
		return d, season
	}
	return "", season
}

// plausibleYear reports whether a four-digit year could be a release year:
// no later than next year (films are announced ahead of release).
func plausibleYear(y string) bool {
	n, err := strconv.Atoi(y)
	return err == nil && n <= time.Now().Year()+1
}

// splitYear splits a show name such as "Cosmos (2014)" or "Cosmos.2014"
// into the cleaned name and the year ("" when there is none).
func splitYear(s string) (name, year string) {
	if y := yearRe.FindStringSubmatchIndex(s); y != nil && y[0] > 0 && plausibleYear(s[y[2]:y[3]]) {
		return clean(s[:y[0]]), s[y[2]:y[3]]
	}
	return clean(s), ""
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// clean turns "The.Big_Show - " into "The Big Show".
func clean(s string) string {
	if !strings.Contains(s, " ") {
		s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
	} else {
		s = strings.ReplaceAll(s, "_", " ")
	}
	s = strings.Trim(s, " -.([])")
	return strings.Join(strings.Fields(s), " ")
}

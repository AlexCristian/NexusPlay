// Package library discovers media files and keeps the database in step with
// the media folder.
package library

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// SupportedExtensions are the video files the scanner picks up.
var SupportedExtensions = map[string]bool{
	".mp4": true, ".m4v": true, ".mkv": true, ".webm": true,
	".mov": true, ".avi": true, ".ts": true, ".wmv": true,
}

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

	var g Guess
	if m := episodeRe.FindStringSubmatchIndex(name); m != nil {
		g.Season, _ = strconv.Atoi(name[m[2]:m[3]])
		g.Episode, _ = strconv.Atoi(name[m[4]:m[5]])
		g.Show = clean(name[:m[0]])
		g.Title = clean(junkRe.ReplaceAllString(name[m[1]:], ""))
	} else if m := crossRe.FindStringSubmatchIndex(name); m != nil {
		g.Season, _ = strconv.Atoi(name[m[2]:m[3]])
		g.Episode, _ = strconv.Atoi(name[m[4]:m[5]])
		g.Show = clean(name[:m[0]])
		g.Title = clean(junkRe.ReplaceAllString(name[m[1]:], ""))
	} else {
		name = junkRe.ReplaceAllString(name, "")
		if m := yearRe.FindAllStringSubmatchIndex(name, -1); m != nil {
			// The last year-like number is the release year; an earlier one may be
			// part of the title ("2001 A Space Odyssey 1968").
			last := m[len(m)-1]
			if last[0] > 0 {
				g.Year = name[last[2]:last[3]]
				name = name[:last[0]]
			}
		}
		g.Title = clean(name)
		if g.Title == "" {
			g.Title = clean(strings.TrimSuffix(file, path.Ext(file)))
		}
		return g
	}

	// Episodes: the nearest folder that is not a "Season 2"-style folder names
	// the show (and may carry its year); a season folder supplies the season.
	var folder string
	dirs := strings.Split(strings.Trim(dir, "/"), "/")
	for i := len(dirs) - 1; i >= 0; i-- {
		d := dirs[i]
		if d == "" {
			continue
		}
		if sm := seasonDir.FindStringSubmatch(d); sm != nil {
			if g.Season == 0 {
				g.Season, _ = strconv.Atoi(sm[1])
			}
			continue
		}
		folder = clean(d)
		break
	}
	folderShow, folderYear := splitYear(folder)
	showName, showYear := splitYear(g.Show)
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

// splitYear splits "Cosmos (2014)" into "Cosmos" and "2014".
func splitYear(s string) (string, string) {
	if y := yearRe.FindStringSubmatchIndex(s); y != nil && y[0] > 0 {
		return clean(s[:y[0]]), s[y[2]:y[3]]
	}
	return s, ""
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

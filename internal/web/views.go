package web

// View models and formatting helpers for the templates. The .templ files
// hold markup only; anything a template computes lives here, where it can
// be read and tested as ordinary Go.

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/a-h/templ"

	"github.com/AlexCristian/NexusPlay/internal/convert"
	"github.com/AlexCristian/NexusPlay/internal/library"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// ---- Every page ----

// Page carries what every full page needs.
type Page struct {
	Title  string
	Viewer Viewer
	Query  string // the search box's text
}

// newPage describes the page being rendered for r.
func newPage(r *http.Request, title string) Page {
	return Page{Title: title, Viewer: viewer(r), Query: r.URL.Query().Get("q")}
}

// csrfHeaders is the hx-headers value that makes htmx send the CSRF token.
func csrfHeaders(viewer Viewer) string {
	if viewer.User == nil {
		return "{}"
	}
	return fmt.Sprintf(`{"X-CSRF-Token": %q}`, viewer.CSRF)
}

// posterURL is where a poster file is served, or the placeholder image.
func posterURL(name string) string {
	if name == "" {
		return asset("/static/img/no-poster.svg")
	}
	return "/posters/" + name
}

func watchURL(id int64) templ.SafeURL { return templ.SafeURL("/watch/" + itoa(id)) }

func collectionURL(id int64) string { return "/collections/" + itoa(id) }

func itoa(number int64) string { return strconv.FormatInt(number, 10) }

// duration formats a length in seconds for people: "8 s", "42 min",
// "1 h 5 min".
func duration(sec float64) string {
	seconds := int(sec + .5)
	switch {
	case seconds < 60:
		return strconv.Itoa(seconds) + " s"
	case seconds < 3600:
		return strconv.Itoa(seconds/60) + " min"
	case seconds%3600/60 == 0:
		return strconv.Itoa(seconds/3600) + " h"
	default:
		return strconv.Itoa(seconds/3600) + " h " + strconv.Itoa(seconds%3600/60) + " min"
	}
}

// clock formats a position in seconds as "0:05", "42:10" or "1:02:03".
func clock(sec float64) string {
	totalSeconds := int(sec)
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	if totalSeconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", totalSeconds/3600, totalSeconds/60%60, totalSeconds%60)
	}
	return fmt.Sprintf("%d:%02d", totalSeconds/60, totalSeconds%60)
}

// percent is how far pos is through total, 0–100, for a progress bar.
func percent(position, total float64) string {
	if total <= 0 {
		return "0%"
	}
	return strconv.FormatFloat(min(100, max(0, position/total*100)), 'f', 1, 64) + "%"
}

// fileSize formats a size in bytes with a unit that fits: "820 KB", "1.2 MB", "4.37 GB".
func fileSize(size int64) string {
	switch {
	case size >= 1<<30:
		return strconv.FormatFloat(float64(size)/(1<<30), 'f', 2, 64) + " GB"
	case size >= 1<<20:
		return strconv.FormatFloat(float64(size)/(1<<20), 'f', 1, 64) + " MB"
	default:
		return strconv.FormatInt((size+1023)/1024, 10) + " KB"
	}
}

// registrationClosedMessage explains why the sign-up form is not shown.
func registrationClosedMessage(maxAccounts int) string {
	return "Registration is closed: this server allows " + strconv.Itoa(maxAccounts) +
		" accounts and all of them are taken. Ask the administrator if you need one."
}

// ---- Library ----

// Season is one season's episodes in a collection.
type Season struct {
	Number   int
	Episodes []store.Media
}

// seasonLabel names a season; season 0 holds specials.
func seasonLabel(season int) string {
	if season == 0 {
		return "Specials"
	}
	return "Season " + strconv.Itoa(season)
}

// seasonSummary is the line under a collection's title: "2 Seasons, 2019".
// Specials (season 0) are not counted as a season.
func seasonSummary(collection store.Collection, seasons []Season) string {
	count := 0
	for _, season := range seasons {
		if season.Number != 0 {
			count++
		}
	}
	var summary string
	switch count {
	case 0:
		summary = "Specials"
	case 1:
		summary = "1 Season"
	default:
		summary = strconv.Itoa(count) + " Seasons"
	}
	if collection.Year != "" {
		summary += ", " + collection.Year
	}
	return summary
}

// displayTitle names an episode with its show, e.g. "Cosmos · S1E3 · The Harmony of the Worlds".
func displayTitle(media store.Media) string {
	if media.CollectionName == "" {
		return media.Title
	}
	return media.CollectionName + " · " + episodeLabel(media) + " · " + media.Title
}

// ---- Player ----

// Playback describes how the browser should load a video.
type Playback struct {
	Direct bool   // the file itself; otherwise an HLS conversion
	URL    string // the file or the HLS playlist
}

// episodeNumbers is an episode's number, or its range for a file holding
// several ("1–2" for a two-part pilot in one file).
func episodeNumbers(media store.Media) string {
	number := strconv.Itoa(media.Episode)
	if media.EpisodeEnd > media.Episode {
		number += "–" + strconv.Itoa(media.EpisodeEnd)
	}
	return number
}

// episodeLabel is an episode's short number, "S3E4" or "S1E1–2", shown
// before its title everywhere. Season 0 holds specials: "Special 3".
func episodeLabel(media store.Media) string {
	if media.Season == 0 {
		return "Special " + episodeNumbers(media)
	}
	return "S" + strconv.Itoa(media.Season) + "E" + episodeNumbers(media)
}

// fmtSeconds formats a resume position for player.js.
func fmtSeconds(seconds float64) string { return strconv.FormatFloat(seconds, 'f', 1, 64) }

// cueTime formats a subtitle time in milliseconds as H:MM:SS.
func cueTime(milliseconds int64) string {
	seconds := milliseconds / 1000
	return strconv.FormatInt(seconds/3600, 10) + ":" + pad2(seconds/60%60) + ":" + pad2(seconds%60)
}

func pad2(number int64) string {
	if number < 10 {
		return "0" + strconv.FormatInt(number, 10)
	}
	return strconv.FormatInt(number, 10)
}

// ---- Control panel ----

// blankZero shows 0 as an empty field (a film has no season or episode).
func blankZero(number int) string {
	if number == 0 {
		return ""
	}
	return strconv.Itoa(number)
}

// subscriptionCount is "your 1 subscription" / "your 3 subscriptions".
func subscriptionCount(count int) string {
	if count == 1 {
		return "your 1 subscription"
	}
	return "your " + strconv.Itoa(count) + " subscriptions"
}

// MediaForm is the data behind the edit and publish form.
type MediaForm struct {
	Media          store.Media
	CollectionName string
	Collections    []store.Collection
	Categories     []string
	Subtitles      []store.Subtitle
	MetaEnabled    bool
	Error          string
	Saved          bool
	Refreshed      bool // details were just fetched again from TMDB
}

// ownPoster is m's own poster, or "" when it shows its collection's. Only
// its own can be removed on its edit page.
func ownPoster(media store.Media) string {
	if !media.OwnPoster {
		return ""
	}
	return media.Poster
}

// guessFor is what the scanner makes of a file's path.
func guessFor(media store.Media) library.Guess { return library.ParsePath(media.Path) }

// detectedAs is what the scanner made of a pending file's name.
func detectedAs(media store.Media) string {
	guess := guessFor(media)
	if guess.IsEpisode() {
		return guess.Show + " · S" + strconv.Itoa(media.Season) + "E" + strconv.Itoa(media.Episode) + " · " + media.Title
	}
	if media.Year != "" {
		return media.Title + " (" + media.Year + ")"
	}
	return media.Title
}

// techSummary is a file's codecs, length and size: "h264 / aac · 42 min · 1.20 GB".
func techSummary(media store.Media) string {
	summary := media.VideoCodec
	if media.AudioCodec != "" {
		summary += " / " + media.AudioCodec
	}
	if media.Duration > 0 {
		summary += " · " + duration(media.Duration)
	}
	summary += " · " + fileSize(media.Size)
	return summary
}

// ConversionView is the control panel's view of the conversion queue.
type ConversionView struct {
	Status   convert.Status
	Failures []store.ConversionFailure
}

// busy reports whether conversions are running or waiting, which makes the
// panel refresh itself.
func (view ConversionView) busy() bool {
	return !view.Status.Off && (len(view.Status.Active) > 0 || view.Status.Queued > 0)
}

func queuedLabel(count int) string {
	if count == 1 {
		return "1 file"
	}
	return strconv.Itoa(count) + " files"
}

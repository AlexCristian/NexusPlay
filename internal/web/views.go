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
	Title string
	V     Viewer
	Query string // the search box's text
}

// newPage describes the page being rendered for r.
func newPage(r *http.Request, title string) Page {
	return Page{Title: title, V: viewer(r), Query: r.URL.Query().Get("q")}
}

// csrfHeaders is the hx-headers value that makes htmx send the CSRF token.
func csrfHeaders(v Viewer) string {
	if v.User == nil {
		return "{}"
	}
	return fmt.Sprintf(`{"X-CSRF-Token": %q}`, v.CSRF)
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

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// minutes formats a duration in seconds as "42 min".
func minutes(sec float64) string { return strconv.Itoa(int(sec/60)) + " min" }

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
func seasonLabel(n int) string {
	if n == 0 {
		return "Specials"
	}
	return "Season " + strconv.Itoa(n)
}

// seasonSummary is the line under a collection's title: "2 Seasons, 2019".
func seasonSummary(c store.Collection, seasons []Season) string {
	s := "1 Season"
	if len(seasons) != 1 {
		s = strconv.Itoa(len(seasons)) + " Seasons"
	}
	if c.Year != "" {
		s += ", " + c.Year
	}
	return s
}

// displayTitle names an episode with its show, e.g. "Cosmos · S1E3 · The Harmony of the Worlds".
func displayTitle(m store.Media) string {
	if m.CollectionName == "" {
		return m.Title
	}
	return fmt.Sprintf("%s · S%dE%d · %s", m.CollectionName, m.Season, m.Episode, m.Title)
}

// ---- Player ----

// Playback describes how the browser should load a video.
type Playback struct {
	Direct bool   // the file itself; otherwise an HLS conversion
	URL    string // the file or the HLS playlist
}

// episodeLabel is the line under an episode's title: "Season 3, episode 5".
func episodeLabel(m store.Media) string {
	if m.Season == 0 {
		return "Specials, episode " + strconv.Itoa(m.Episode)
	}
	return "Season " + strconv.Itoa(m.Season) + ", episode " + strconv.Itoa(m.Episode)
}

// fmtSeconds formats a resume position for player.js.
func fmtSeconds(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

// cueTime formats a subtitle time in milliseconds as H:MM:SS.
func cueTime(ms int64) string {
	s := ms / 1000
	return strconv.FormatInt(s/3600, 10) + ":" + pad2(s/60%60) + ":" + pad2(s%60)
}

func pad2(n int64) string {
	if n < 10 {
		return "0" + strconv.FormatInt(n, 10)
	}
	return strconv.FormatInt(n, 10)
}

// ---- Control panel ----

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
func ownPoster(m store.Media) string {
	if !m.OwnPoster {
		return ""
	}
	return m.Poster
}

// guessFor is what the scanner makes of a file's path.
func guessFor(m store.Media) library.Guess { return library.ParsePath(m.Path) }

// detectedAs is what the scanner made of a pending file's name.
func detectedAs(m store.Media) string {
	g := guessFor(m)
	if g.IsEpisode() {
		return g.Show + " · S" + strconv.Itoa(m.Season) + "E" + strconv.Itoa(m.Episode) + " · " + m.Title
	}
	if m.Year != "" {
		return m.Title + " (" + m.Year + ")"
	}
	return m.Title
}

// techSummary is a file's codecs, length and size: "h264 / aac · 42 min · 1.20 GB".
func techSummary(m store.Media) string {
	s := m.VideoCodec
	if m.AudioCodec != "" {
		s += " / " + m.AudioCodec
	}
	if m.Duration > 0 {
		s += " · " + minutes(m.Duration)
	}
	s += " · " + strconv.FormatFloat(float64(m.Size)/(1<<30), 'f', 2, 64) + " GB"
	return s
}

// ConversionView is the control panel's view of the conversion queue.
type ConversionView struct {
	Status   convert.Status
	Failures []store.ConversionFailure
}

// busy reports whether conversions are running or waiting, which makes the
// panel refresh itself.
func (v ConversionView) busy() bool {
	return !v.Status.Off && (len(v.Status.Active) > 0 || v.Status.Queued > 0)
}

func queuedLabel(n int) string {
	if n == 1 {
		return "1 file"
	}
	return strconv.Itoa(n) + " files"
}

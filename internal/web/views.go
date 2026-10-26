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

// duration formats a length in seconds for people: "8 s", "42 min",
// "1 h 5 min".
func duration(sec float64) string {
	s := int(sec + .5)
	switch {
	case s < 60:
		return strconv.Itoa(s) + " s"
	case s < 3600:
		return strconv.Itoa(s/60) + " min"
	case s%3600/60 == 0:
		return strconv.Itoa(s/3600) + " h"
	default:
		return strconv.Itoa(s/3600) + " h " + strconv.Itoa(s%3600/60) + " min"
	}
}

// clock formats a position in seconds as "0:05", "42:10" or "1:02:03".
func clock(sec float64) string {
	s := int(sec)
	if s < 0 {
		s = 0
	}
	if s >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", s/3600, s/60%60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// percent is how far pos is through total, 0–100, for a progress bar.
func percent(pos, total float64) string {
	if total <= 0 {
		return "0%"
	}
	return strconv.FormatFloat(min(100, max(0, pos/total*100)), 'f', 1, 64) + "%"
}

// fileSize formats a size in bytes with a unit that fits: "820 KB", "1.2 MB", "4.37 GB".
func fileSize(b int64) string {
	switch {
	case b >= 1<<30:
		return strconv.FormatFloat(float64(b)/(1<<30), 'f', 2, 64) + " GB"
	case b >= 1<<20:
		return strconv.FormatFloat(float64(b)/(1<<20), 'f', 1, 64) + " MB"
	default:
		return strconv.FormatInt((b+1023)/1024, 10) + " KB"
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
func seasonLabel(n int) string {
	if n == 0 {
		return "Specials"
	}
	return "Season " + strconv.Itoa(n)
}

// seasonSummary is the line under a collection's title: "2 Seasons, 2019".
// Specials (season 0) are not counted as a season.
func seasonSummary(c store.Collection, seasons []Season) string {
	n := 0
	for _, se := range seasons {
		if se.Number != 0 {
			n++
		}
	}
	var s string
	switch n {
	case 0:
		s = "Specials"
	case 1:
		s = "1 Season"
	default:
		s = strconv.Itoa(n) + " Seasons"
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
	return m.CollectionName + " · " + episodeLabel(m) + " · " + m.Title
}

// ---- Player ----

// Playback describes how the browser should load a video.
type Playback struct {
	Direct bool   // the file itself; otherwise an HLS conversion
	URL    string // the file or the HLS playlist
}

// episodeNumbers is an episode's number, or its range for a file holding
// several ("1–2" for a two-part pilot in one file).
func episodeNumbers(m store.Media) string {
	n := strconv.Itoa(m.Episode)
	if m.EpisodeEnd > m.Episode {
		n += "–" + strconv.Itoa(m.EpisodeEnd)
	}
	return n
}

// episodeLabel is an episode's short number, "S3E4" or "S1E1–2", shown
// before its title everywhere. Season 0 holds specials: "Special 3".
func episodeLabel(m store.Media) string {
	if m.Season == 0 {
		return "Special " + episodeNumbers(m)
	}
	return "S" + strconv.Itoa(m.Season) + "E" + episodeNumbers(m)
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

// blankZero shows 0 as an empty field (a film has no season or episode).
func blankZero(n int) string {
	if n == 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// subscriptionCount is "your 1 subscription" / "your 3 subscriptions".
func subscriptionCount(n int) string {
	if n == 1 {
		return "your 1 subscription"
	}
	return "your " + strconv.Itoa(n) + " subscriptions"
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
		s += " · " + duration(m.Duration)
	}
	s += " · " + fileSize(m.Size)
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

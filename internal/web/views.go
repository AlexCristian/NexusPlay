package web

// View models and formatting helpers for the templates. The .templ files
// hold markup only; anything a template computes lives here, where it can
// be read and tested as ordinary Go.

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/a-h/templ"

	"github.com/AlexCristian/NexusPlay/internal/library"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// ---- Every page ----

// [newPage] describes the full page being rendered for a request.
//
// Parameters:
//   - [r]: the request; its "q" parameter fills the search box.
//   - [title]: the page's title, or "" for the site's name alone.
//
// Returns:
//   - [Page]: the title, the viewer and the search box's text.
func newPage(r *http.Request, title string) Page {
	return Page{Title: title, Viewer: viewer(r), Query: r.URL.Query().Get("q")}
}

// [csrfHeaders] makes the hx-headers attribute value that has htmx send the
// CSRF token with every request. The token is URL-safe base64, so Go's %q
// quoting of it is also valid JSON.
//
// Parameters:
//   - [viewer]: who is viewing the page.
//
// Returns:
//   - [string]: a JSON object with the "X-CSRF-Token" header for a signed-in
//     viewer, or "{}" for an anonymous one.
func csrfHeaders(viewer Viewer) string {
	if viewer.User == nil {
		return "{}"
	}
	return fmt.Sprintf(`{"X-CSRF-Token": %q}`, viewer.CSRF)
}

// [posterURL] gives the address to show a poster from.
//
// Parameters:
//   - [name]: the poster's file name in the poster folder, or "".
//
// Returns:
//   - [string]: "/posters/" and [name], or the placeholder image's address
//     if [name] is "".
func posterURL(name string) string {
	if name == "" {
		return asset("/static/img/no-poster.svg")
	}
	return "/posters/" + name
}

// [watchURL] gives the player page's address for a media item.
//
// Parameters:
//   - [id]: the media item.
//
// Returns:
//   - [templ.SafeURL]: "/watch/" and [id], marked safe for an href.
func watchURL(id int64) templ.SafeURL { return templ.SafeURL("/watch/" + formatInteger(id)) }

// [collectionURL] gives a collection dialog's address.
//
// Parameters:
//   - [id]: the collection.
//
// Returns:
//   - [string]: "/collections/" and [id].
func collectionURL(id int64) string { return "/collections/" + formatInteger(id) }

// [formatInteger] writes a whole number in decimal, for URLs and labels.
//
// Parameters:
//   - [number]: the number.
//
// Returns:
//   - [string]: its decimal digits, with "-" if negative.
func formatInteger(number int64) string { return strconv.FormatInt(number, 10) }

// [duration] writes a length for people: "8 s", "42 min", "2 h" or
// "1 h 5 min".
//
// Parameters:
//   - [durationSeconds]: the length in seconds, rounded to the nearest
//     second.
//
// Returns:
//   - [string]: seconds under a minute, whole minutes under an hour, and
//     hours and minutes otherwise.
func duration(durationSeconds float64) string {
	seconds := int(durationSeconds + .5)
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

// [clock] writes a playback position like a clock: "0:05", "42:10" or
// "1:02:03".
//
// Parameters:
//   - [seconds]: the position in seconds; a negative one counts as 0 and
//     fractions are dropped.
//
// Returns:
//   - [string]: minutes and seconds, with hours in front from one hour on.
func clock(seconds float64) string {
	totalSeconds := int(seconds)
	if totalSeconds < 0 {
		totalSeconds = 0
	}
	if totalSeconds >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", totalSeconds/3600, totalSeconds/60%60, totalSeconds%60)
	}
	return fmt.Sprintf("%d:%02d", totalSeconds/60, totalSeconds%60)
}

// [percent] writes how far a position is through a total, for a progress
// bar's width.
//
// Parameters:
//   - [position]: how far, such as seconds watched.
//   - [total]: the whole, such as the video's length.
//
// Returns:
//   - [string]: the share from "0.0%" to "100.0%" with one decimal, or
//     "0%" if [total] is 0 or less.
func percent(position, total float64) string {
	if total <= 0 {
		return "0%"
	}
	return strconv.FormatFloat(min(100, max(0, position/total*100)), 'f', 1, 64) + "%"
}

// [fileSize] writes a size in bytes with a unit that fits: "820 KB",
// "1.2 MB" or "4.37 GB". Units are binary (1 KB is 1024 bytes).
//
// Parameters:
//   - [size]: the size in bytes.
//
// Returns:
//   - [string]: whole kilobytes (rounded up) below 1 MB, megabytes with one
//     decimal below 1 GB, and gigabytes with two decimals otherwise.
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

// [registrationClosedMessage] explains why the sign-up form is not shown.
//
// Parameters:
//   - [maxAccounts]: the account limit.
//
// Returns:
//   - [string]: the explanation, naming the limit.
func registrationClosedMessage(maxAccounts int) string {
	return "Registration is closed: this server allows " + strconv.Itoa(maxAccounts) +
		" accounts and all of them are taken. Ask the administrator if you need one."
}

// ---- Library ----

// [seasonLabel] names a season for its tab.
//
// Parameters:
//   - [season]: the season number.
//
// Returns:
//   - [string]: "Specials" for season 0, otherwise "Season N".
func seasonLabel(season int) string {
	if season == 0 {
		return "Specials"
	}
	return "Season " + strconv.Itoa(season)
}

// [seasonSummary] writes the line under a collection's title, such as "2
// Seasons, 2019". Specials (season 0) are not counted as a season, so a
// film collection, or one with no episodes, reads "Specials".
//
// Parameters:
//   - [collection]: the collection, for its year.
//   - [seasons]: the collection's seasons.
//
// Returns:
//   - [string]: "Specials", "1 Season" or "N Seasons", with ", " and the
//     year added when it is known.
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

// [displayTitle] names a media item with its show, such as "Cosmos · S1E3 ·
// The Harmony of the Worlds". Items in a film collection, which have no
// season or episode, are labelled as specials ("Special 0").
//
// Parameters:
//   - [media]: the media item.
//
// Returns:
//   - [string]: the collection, [episodeLabel] and title joined with " · ",
//     or the title alone for an item without a collection.
func displayTitle(media store.Media) string {
	if media.CollectionName == "" {
		return media.Title
	}
	return media.CollectionName + " · " + episodeLabel(media) + " · " + media.Title
}

// ---- Player ----

// [episodeNumbers] writes an episode's number, or its range for a file
// holding several, such as "1–2" for a two-part pilot in one file.
//
// Parameters:
//   - [media]: the episode.
//
// Returns:
//   - [string]: the number, with an en dash and the last number when
//     [store.Media.EpisodeEnd] is greater than [store.Media.Episode].
func episodeNumbers(media store.Media) string {
	number := strconv.Itoa(media.Episode)
	if media.EpisodeEnd > media.Episode {
		number += "–" + strconv.Itoa(media.EpisodeEnd)
	}
	return number
}

// [episodeLabel] writes an episode's short number, shown before its title
// everywhere: "S3E4" or "S1E1–2", and "Special 3" for season 0.
//
// Parameters:
//   - [media]: the episode.
//
// Returns:
//   - [string]: the label.
func episodeLabel(media store.Media) string {
	if media.Season == 0 {
		return "Special " + episodeNumbers(media)
	}
	return "S" + strconv.Itoa(media.Season) + "E" + episodeNumbers(media)
}

// [formatSeconds] writes a number of seconds for player.js to read, such as
// "125.0".
//
// Parameters:
//   - [seconds]: the number of seconds.
//
// Returns:
//   - [string]: the number with one decimal.
func formatSeconds(seconds float64) string { return strconv.FormatFloat(seconds, 'f', 1, 64) }

// [cueTime] writes a subtitle time as H:MM:SS, such as "0:01:05".
//
// Parameters:
//   - [milliseconds]: the time in milliseconds; fractions of a second are
//     dropped.
//
// Returns:
//   - [string]: hours, minutes and seconds.
func cueTime(milliseconds int64) string {
	seconds := milliseconds / 1000
	return fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
}

// ---- Control panel ----

// [blankZero] writes a number for a form field, leaving 0 blank: a film has
// no season or episode, and a blank season is read back as 0 (specials).
//
// Parameters:
//   - [number]: the number.
//
// Returns:
//   - [string]: "" for 0, otherwise the number.
func blankZero(number int) string {
	if number == 0 {
		return ""
	}
	return strconv.Itoa(number)
}

// [subscriptionPhrase] writes "your 1 subscription" or "your 3
// subscriptions".
//
// Parameters:
//   - [count]: the number of subscriptions.
//
// Returns:
//   - [string]: the phrase, singular for exactly 1.
func subscriptionPhrase(count int) string {
	if count == 1 {
		return "your 1 subscription"
	}
	return "your " + strconv.Itoa(count) + " subscriptions"
}

// [ownPoster] gets a media item's own poster, for its edit page, where only
// its own can be removed.
//
// Parameters:
//   - [media]: the media item.
//
// Returns:
//   - [string]: the poster's file name, or "" when the item shows its
//     collection's poster or none.
func ownPoster(media store.Media) string {
	if !media.OwnPoster {
		return ""
	}
	return media.Poster
}

// [guessFor] works out what the scanner makes of a media item's path.
//
// Parameters:
//   - [media]: the media item.
//
// Returns:
//   - [library.Guess]: [library.ParsePath]'s guess for its path.
func guessFor(media store.Media) library.Guess { return library.ParsePath(media.Path) }

// [detectedAs] describes what the scanner made of a pending file, for the
// control panel: "Show · S1E2 · Title" for an episode, or "Title (Year)"
// for a film.
//
// Parameters:
//   - [media]: the pending media item.
//
// Returns:
//   - [string]: the description, using the item's current season, episode,
//     title and year, and the show from its path.
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

// [techSummary] describes a file's codecs, length and size, such as "h264 /
// aac · 42 min · 1.20 GB".
//
// Parameters:
//   - [media]: the media item.
//
// Returns:
//   - [string]: the video codec (with " / " and the audio codec if there
//     is one), then the length if known and the size, each after " · ".
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

// [queuedLabel] writes "1 file" or "N files", for the conversion queue.
//
// Parameters:
//   - [count]: the number of files.
//
// Returns:
//   - [string]: the phrase, singular for exactly 1.
func queuedLabel(count int) string {
	if count == 1 {
		return "1 file"
	}
	return strconv.Itoa(count) + " files"
}

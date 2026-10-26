//go:build integration

package integration

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// TestPolishRound2 checks the options chosen in the second round of the UI
// polish review, each where it applies. The numbers are the review page's.
func TestPolishRound2(t *testing.T) {
	s := startServer(t, serverOpts{media: allFixtures})
	s.waitForMedia(5)
	s.publishAll()
	s.register("alex")
	sam := s.register("sam")
	b := newBrowser(t)
	ctx := context.Background()
	st := s.App.Store

	movie := s.mediaID(movieMP4)
	ep := s.mediaID(episode1)
	epMedia, err := st.MediaByID(ctx, ep)
	if err != nil {
		t.Fatal(err)
	}
	show := epMedia.CollectionID
	c, _ := st.CollectionByID(ctx, show)
	long := strings.Repeat("A long description of a show made of test patterns, written to be cut short. ", 8)
	if err := st.UpdateCollection(ctx, show, store.CollectionUpdate{Name: c.Name, Category: c.Category, Year: c.Year, Overview: long}); err != nil {
		t.Fatal(err)
	}
	// Sam paused the movie 5 seconds in and follows the show.
	sam.post("/me/progress", url.Values{"media_id": {fmt.Sprint(movie)}, "position": {"5"}})
	sam.post(fmt.Sprintf("/collections/%d/subscribe", show), url.Values{})

	type js = string
	eval := func(t *testing.T, tb *tab, expr js, out any) { t.Helper(); tb.eval(expr, out) }
	openShow := func(tb *tab) {
		tb.navigate(s.URL)
		tb.click(`.tab[data-tab="tab-1"]`)
		tb.click("#tab-1 .tile")
		tb.waitVisible("#modal[open] .episodes")
	}

	t.Run("15 the dialog stays put when switching seasons", func(t *testing.T) {
		tb := b.newTab(1200, 900)
		tb.t = t
		openShow(tb)
		time.Sleep(400 * time.Millisecond) // the dialog slides in for 0.3s
		var before, after float64
		eval(t, tb, `document.getElementById('modal').getBoundingClientRect().top`, &before)
		tb.click(`#modal .season-tab:last-child`)
		time.Sleep(400 * time.Millisecond)
		eval(t, tb, `document.getElementById('modal').getBoundingClientRect().top`, &after)
		if before != after || before > 0.12*900 {
			t.Errorf("dialog top %.0f → %.0f; want it fixed near 10%% of the screen", before, after)
		}
	})

	t.Run("18 19 33 admin pages", func(t *testing.T) {
		tb := b.newTab(390, 844)
		tb.t = t
		tb.login(s, "alex")
		tb.navigate(s.URL + "/admin")
		var shown bool
		eval(t, tb, `(c => !!c && getComputedStyle(c).display !== 'none')(document.querySelector('.pending-table td:nth-child(2)'))`, &shown)
		if shown {
			t.Error("phone control panel still shows the “Detected as” column")
		}
		tb = b.newTab(1200, 900)
		tb.t = t
		tb.login(s, "alex")
		tb.navigate(fmt.Sprintf("%s/admin/media/%d", s.URL, movie))
		var labels []string
		eval(t, tb, `[...document.querySelectorAll('.subtitle-table label')].map(l => l.firstChild.textContent.trim())`, &labels)
		if strings.Join(labels, ",") != "Language code,Label" {
			t.Errorf("subtitle track labels = %v", labels)
		}
		var muted string
		eval(t, tb, `getComputedStyle(document.querySelector('.panel .muted')).color`, &muted)
		if muted != "rgb(102, 102, 102)" {
			t.Errorf("help text color = %s, want #666", muted)
		}
		// 27: a film's season and episode are empty, and an episode range can be given.
		var f struct {
			Season, Episode string
			HasEnd          bool
		}
		eval(t, tb, `({season: document.querySelector('[name=season]').value, episode: document.querySelector('[name=episode]').value, hasEnd: !!document.querySelector('[name=episode_end]')})`, &f)
		if f.Season != "" || f.Episode != "" || !f.HasEnd {
			t.Errorf("film's fields: season %q, episode %q, range field %v", f.Season, f.Episode, f.HasEnd)
		}
		// 41: the title is edited in a field that grows.
		var tag string
		eval(t, tb, `document.querySelector('[name=title]').tagName + ' ' + getComputedStyle(document.querySelector('[name=title]')).fieldSizing`, &tag)
		if tag != "TEXTAREA content" {
			t.Errorf("title field = %s, want a growing textarea", tag)
		}
	})

	t.Run("20 the season row fades at the edge with more seasons", func(t *testing.T) {
		// The fixture show has 2 seasons; on a narrow phone they overflow.
		tb := b.newTab(320, 640)
		tb.t = t
		openShow(tb)
		var cls string
		eval(t, tb, `(r => r.scrollWidth > r.clientWidth ? r.className : 'fits')(document.querySelector('#modal .season-tabs'))`, &cls)
		if cls != "fits" && !strings.Contains(cls, "more-end") {
			t.Errorf("an overflowing season row has classes %q, want more-end", cls)
		}
	})

	t.Run("21 22 34 36 the player", func(t *testing.T) {
		tb := b.newTab(1200, 900)
		tb.t = t
		tb.navigate(fmt.Sprintf("%s/watch/%d", s.URL, movie))
		tb.waitJS(`!document.getElementById('controls').hidden`, 10*time.Second)
		var rail struct{ Height, Image string }
		eval(t, tb, `(s => ({height: s.height, image: s.backgroundImage}))(getComputedStyle(document.getElementById('ctl-seek')))`, &rail)
		if rail.Height != "14px" || !strings.Contains(rail.Image, "repeating-linear-gradient") {
			t.Errorf("seek bar: height %s, background %.60s; want 14px with fixed-width stripes", rail.Height, rail.Image)
		}
		var titles []string
		eval(t, tb, `[...document.querySelectorAll('#controls button, #controls input')].filter(e => !e.title).map(e => e.id)`, &titles)
		if len(titles) > 0 {
			t.Errorf("controls without a tooltip: %v", titles)
		}
		// 34: radio marks; Escape closes the menu and returns focus to its button.
		tb.run(chromedp.Focus("#ctl-captions", chromedp.ByQuery), chromedp.KeyEvent("\r"))
		tb.waitJS(`!document.getElementById('captions-menu').hidden`, 5*time.Second)
		tb.run(chromedp.KeyEvent("\x1b"))
		tb.waitJS(`document.getElementById('captions-menu').hidden && document.activeElement.id === 'ctl-captions'`, 5*time.Second)
		// 24: the amber focus ring.
		var ring string
		eval(t, tb, `getComputedStyle(document.activeElement).outlineColor`, &ring)
		if ring != "rgb(255, 179, 0)" {
			t.Errorf("focus ring color = %s, want amber", ring)
		}
		tb.navigate(s.URL)
		var clamp string
		eval(t, tb, `getComputedStyle(document.querySelector('.tile-title')).webkitLineClamp`, &clamp)
		if clamp != "2" {
			t.Errorf("tile titles clamp at %q lines, want 2", clamp)
		}
	})

	t.Run("25 the controls hide while playing", func(t *testing.T) {
		tb := b.newTab(1200, 900)
		tb.t = t
		tb.navigate(fmt.Sprintf("%s/watch/%d", s.URL, movie))
		tb.waitJS(`document.getElementById('player').readyState >= 2 && !document.getElementById('controls').hidden`, 20*time.Second)
		tb.click("#big-play")
		tb.waitJS(`document.getElementById('player-container').classList.contains('idle')`, 6*time.Second)
		tb.run(chromedp.MouseEvent("mouseMoved", 600, 400))
		tb.waitJS(`!document.getElementById('player-container').classList.contains('idle')`, 2*time.Second)
		tb.eval(`document.getElementById('player').pause()`, nil)
	})

	t.Run("23 the search drawer closes on Escape and on the page", func(t *testing.T) {
		tb := b.newTab(1200, 900)
		tb.t = t
		tb.navigate(s.URL)
		tb.waitHTMX("input[name=q]")
		typeQuery := func() {
			tb.run(chromedp.SendKeys(`input[name=q]`, "test", chromedp.ByQuery))
			tb.waitJS(`document.querySelector('#search-drawer.open')`, 5*time.Second)
		}
		typeQuery()
		tb.eval(`document.querySelector('input[name=q]').blur(); document.body.focus()`, nil)
		tb.run(chromedp.KeyEvent("\x1b"))
		tb.waitJS(`!document.querySelector('#search-drawer.open') && document.querySelector('input[name=q]').value === ''`, 5*time.Second)
		typeQuery()
		tb.eval(`document.getElementById('page-cover').click()`, nil)
		tb.waitJS(`!document.querySelector('#search-drawer.open')`, 5*time.Second)
	})

	t.Run("26 28 31 37 signed-in library", func(t *testing.T) {
		tb := b.newTab(1200, 900)
		tb.t = t
		tb.login(s, "sam")
		tb.navigate(s.URL)
		tb.click(`[popovertarget=notif-pop]`)
		tb.waitJS(`document.querySelector('#notif-pop-body .popover-empty')`, 5*time.Second)
		if got := tb.text("#notif-pop-body .popover-empty"); got != "No new episodes in your 1 subscription." {
			t.Errorf("subscriptions popover says %q", got)
		}
		tb.eval(`document.getElementById('notif-pop').hidePopover()`, nil)
		tb.click(`[popovertarget=resume-pop]`)
		tb.waitJS(`document.querySelector('#resume-pop-body .progress')`, 5*time.Second)
		if got := tb.text("#resume-pop-body small"); got != "0:05 of 0:12" {
			t.Errorf("resume position reads %q, want 0:05 of 0:12", got)
		}
		tb.eval(`document.getElementById('resume-pop').hidePopover()`, nil)
		// 37: switching tabs animates the strip's height and fades the panel.
		var animating bool
		eval(t, tb, `(() => { document.querySelector('.tab[data-tab="tab-1"]').click();
			return document.getAnimations().length > 0; })()`, &animating)
		if !animating {
			t.Error("switching tabs does not animate")
		}
		// 28: a long description is cut, with a button that expands it.
		tb.click("#tab-1 .tile")
		tb.waitVisible("#modal[open] .clamp-toggle")
		var hBefore, hAfter float64
		eval(t, tb, `document.querySelector('#modal .overview').getBoundingClientRect().height`, &hBefore)
		tb.click("#modal .clamp-toggle")
		eval(t, tb, `document.querySelector('#modal .overview').getBoundingClientRect().height`, &hAfter)
		if hAfter <= hBefore {
			t.Errorf("expanding the description: %.0f → %.0fpx", hBefore, hAfter)
		}
	})

	t.Run("29 the report's submit button", func(t *testing.T) {
		tb := b.newTab(1200, 900)
		tb.t = t
		tb.login(s, "sam")
		tb.navigate(fmt.Sprintf("%s/watch/%d", s.URL, movie))
		tb.waitJS(`document.getElementById('player').readyState >= 1 && !document.getElementById('controls').hidden`, 20*time.Second)
		tb.click("#ctl-captions")
		tb.click(`#captions-menu button:nth-child(2)`)
		tb.eval(`document.getElementById('player').currentTime = 2`, nil)
		tb.waitVisible("#report-subtitle")
		tb.click("#report-subtitle")
		tb.waitVisible("#modal[open] #subtitle-report form")
		if got := tb.text("#subtitle-report form button[type=submit]"); got != "Submit" {
			t.Errorf("submit button reads %q", got)
		}
	})

	t.Run("40 42 43 46 47 pages and the bar", func(t *testing.T) {
		tb := b.newTab(1200, 900)
		tb.t = t
		tb.navigate(s.URL + "/login")
		var st struct {
			Search, Addon string
			Bar           float64
		}
		eval(t, tb, `({search: getComputedStyle(document.querySelector('.above-panel')).visibility,
			addon: (document.querySelector('.auth .addon') || {}).textContent || '',
			bar: document.getElementById('top-bar').getBoundingClientRect().height})`, &st)
		if st.Search != "hidden" || st.Addon != "@" || st.Bar > 33 {
			t.Errorf("login page: search row %s, first add-on %q, bar %.0fpx; want hidden, @, 32px", st.Search, st.Addon, st.Bar)
		}
		tb.navigate(s.URL + "/watch/9999")
		var head string
		eval(t, tb, `getComputedStyle(document.querySelector('.message-heading')).backgroundColor`, &head)
		if head != "rgb(194, 31, 0)" {
			t.Errorf("error heading background = %s, want red", head)
		}
		tb.login(s, "sam")
		tb.navigate(s.URL)
		var weight string
		eval(t, tb, `getComputedStyle(document.querySelector('.top-text')).fontWeight`, &weight)
		if weight != "400" {
			t.Errorf("greeting weight = %s, want plain 400", weight)
		}
	})
}

//go:build integration

package integration

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// TestPolish checks the fixes from the UI polish review, each where it
// showed: the viewport, the account and the content that brought it out.
func TestPolish(t *testing.T) {
	s := startServer(t, serverOpts{media: allFixtures})
	s.waitForMedia(5)
	s.publishAll()
	s.register("alex")
	s.register("sam")
	b := newBrowser(t)
	ctx := context.Background()

	// Content that used to overflow: a long unbroken title and URL.
	long := strings.Repeat("Supercalifragilistic", 5)
	url := "https://example.com/" + strings.Repeat("averyveryverylongpathsegment/", 4)
	ep, err := s.App.Store.MediaByID(ctx, s.mediaID(episode1))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.App.Store.UpdateMedia(ctx, ep.ID, store.MediaUpdate{Title: long, CollectionID: ep.CollectionID,
		Season: ep.Season, Episode: ep.Episode, Overview: "Read more at " + url}, true); err != nil {
		t.Fatal(err)
	}

	t.Run("reflection stays in the panel", func(t *testing.T) {
		tb := b.newTab(1200, 900)
		tb.t = t
		tb.navigate(fmt.Sprintf("%s/watch/%d", s.URL, s.mediaID(movieMP4)))
		var overflow string
		tb.eval(`getComputedStyle(document.querySelector('.video-content')).overflow`, &overflow)
		if overflow != "hidden" {
			t.Errorf("the info panel does not clip the poster's reflection (overflow %s)", overflow)
		}
	})

	t.Run("messages are styled boxes", func(t *testing.T) {
		tb := b.newTab(1200, 900)
		tb.t = t
		tb.navigate(s.URL + "/login")
		tb.fill("input[name=email]", "alex@example.com")
		tb.fill("input[name=password]", "wrong password")
		tb.click("form button[type=submit]")
		tb.waitVisible(".alert-error")
		var st struct{ Bg, Edge string }
		tb.eval(`(s => ({bg: s.backgroundColor, edge: s.borderLeftColor}))(getComputedStyle(document.querySelector('.alert-error')))`, &st)
		if st.Bg == "rgba(0, 0, 0, 0)" || st.Edge != "rgb(194, 31, 0)" {
			t.Errorf("error message: background %s, edge %s; want a tinted box with a red edge", st.Bg, st.Edge)
		}
		// Links on the white panel are readable (they were 1.9:1).
		var ratio float64
		tb.eval(`(() => {
			const lum = c => { const v = c.match(/\d+/g).slice(0, 3).map(x => { x /= 255; return x <= .03928 ? x / 12.92 : ((x + .055) / 1.055) ** 2.4; });
				return .2126 * v[0] + .7152 * v[1] + .0722 * v[2]; };
			const l = lum(getComputedStyle(document.querySelector('.auth a')).color);
			return 1.05 / (l + .05);
		})()`, &ratio)
		if ratio < 4.5 {
			t.Errorf("link contrast on white = %.1f:1, want at least 4.5:1", ratio)
		}
	})

	t.Run("the close button shows on an error dialog", func(t *testing.T) {
		tb := b.newTab(1200, 900)
		tb.t = t
		tb.login(s, "sam")
		tb.navigate(s.URL)
		tb.waitJS(`window.htmx`, 5*time.Second)
		tb.eval(`htmx.ajax('GET', '/collections/9999', {target: '#modal-body'})`, nil)
		tb.waitVisible("#modal[open] .alert")
		var bg string
		tb.eval(`getComputedStyle(document.querySelector('#modal .modal-close')).backgroundColor`, &bg)
		if !strings.HasPrefix(bg, "rgba(0, 0, 0, 0.") {
			t.Errorf("close button background = %s, want a dark circle", bg)
		}
		compareScreenshot(t, "dialog-error", tb.screenshot())
	})

	t.Run("an unreachable server is reported", func(t *testing.T) {
		tb := b.newTab(1200, 900)
		tb.t = t
		tb.navigate(s.URL)
		tb.waitJS(`window.htmx`, 5*time.Second)
		// Every request now fails to connect, as when the server is down.
		tb.eval(`XMLHttpRequest.prototype.send = function () {
			setTimeout(() => this.dispatchEvent(new ProgressEvent('error')), 10); }`, nil)
		tb.click(`.tab[data-tab="tab-1"]`)
		tb.click("#tab-1 .tile")
		tb.waitJS(`(document.querySelector('#modal[open] .alert-error') || {textContent: ''}).textContent.includes('reach the server')`, 5*time.Second)
	})

	t.Run("landscape phone shows the player's controls", func(t *testing.T) {
		tb := b.newTab(844, 390)
		tb.t = t
		tb.navigate(fmt.Sprintf("%s/watch/%d", s.URL, s.mediaID(movieMP4)))
		tb.waitJS(`!document.getElementById('controls').hidden`, 10*time.Second)
		var bottom, height float64
		tb.eval(`document.getElementById('controls').getBoundingClientRect().bottom`, &bottom)
		tb.eval(`innerHeight`, &height)
		if bottom > height+1 {
			t.Errorf("control bar ends at %.0fpx in a %.0fpx-tall screen", bottom, height)
		}
	})

	t.Run("the top bar stays one row", func(t *testing.T) {
		for _, w := range []int64{700, 900, 1000, 1200} {
			tb := b.newTab(w, 800)
			tb.t = t
			tb.login(s, "alex")
			tb.navigate(s.URL)
			var h float64
			tb.eval(`document.getElementById('top-bar').getBoundingClientRect().height`, &h)
			if h > 46 {
				t.Errorf("at %dpx the admin's top bar is %.0fpx tall, want one row", w, h)
			}
		}
	})

	t.Run("phone fields are 16px so iOS does not zoom", func(t *testing.T) {
		tb := b.newTab(390, 844)
		tb.t = t
		tb.navigate(s.URL + "/login")
		var small []string
		tb.eval(`[...document.querySelectorAll('input:not([type=hidden]), textarea')]
			.filter(e => parseFloat(getComputedStyle(e).fontSize) < 16).map(e => e.name)`, &small)
		if len(small) > 0 {
			t.Errorf("fields under 16px on a phone: %v", small)
		}
	})

	t.Run("long words wrap", func(t *testing.T) {
		tb := b.newTab(390, 844)
		tb.t = t
		tb.navigate(fmt.Sprintf("%s/watch/%d", s.URL, ep.ID))
		var wide struct{ Page, View float64 }
		tb.eval(`({page: document.documentElement.scrollWidth, view: innerWidth})`, &wide)
		if wide.Page > wide.View {
			t.Errorf("a long title widens the phone page to %.0fpx (screen %.0fpx)", wide.Page, wide.View)
		}
		tb.navigate(s.URL)
		tb.click(`.tab[data-tab="tab-1"]`)
		tb.click("#tab-1 .tile")
		tb.waitVisible("#modal[open] .episodes")
		var over float64
		tb.eval(`(b => b.scrollWidth - b.clientWidth)(document.getElementById('modal-body'))`, &over)
		if over > 0 {
			t.Errorf("the collection dialog scrolls sideways by %.0fpx with a long episode title", over)
		}
	})

	t.Run("phone library tabs stay on one row", func(t *testing.T) {
		tb := b.newTab(320, 640)
		tb.t = t
		tb.navigate(s.URL)
		var tops []float64
		tb.eval(`[...document.querySelectorAll('#library .tab-list .tab')].map(e => e.offsetTop)`, &tops)
		for _, top := range tops[1:] {
			if top != tops[0] {
				t.Errorf("library tabs wrap onto more than one row: %v", tops)
				break
			}
		}
	})

	t.Run("popovers open under their button", func(t *testing.T) {
		tb := b.newTab(1200, 900)
		tb.t = t
		tb.login(s, "sam")
		tb.navigate(s.URL)
		for _, id := range []string{"resume-pop", "notif-pop"} {
			tb.click(fmt.Sprintf(`[popovertarget=%s]`, id))
			tb.waitVisible("#" + id)
			var at struct{ Btn, Left, Right, Top, Below float64 }
			tb.eval(fmt.Sprintf(`(() => { const b = document.querySelector('[popovertarget=%s]').getBoundingClientRect(),
				p = document.getElementById(%q).getBoundingClientRect();
				return {btn: b.left + b.width / 2, left: p.left, right: p.right, top: p.top, below: b.bottom}; })()`, id, id), &at)
			if at.Btn < at.Left || at.Btn > at.Right || at.Top < at.Below {
				t.Errorf("%s spans %.0f–%.0f from y %.0f; its button's centre is at x %.0f, bottom y %.0f", id, at.Left, at.Right, at.Top, at.Btn, at.Below)
			}
			tb.eval(fmt.Sprintf(`document.getElementById(%q).hidePopover()`, id), nil)
		}
		tb.checkProblems()
	})
}

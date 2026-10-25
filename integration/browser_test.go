//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chromedp/cdproto/cdp"
	cdplog "github.com/chromedp/cdproto/log"
	cdpruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/kb"
)

// chromePath finds the pinned headless Chrome (see scripts/install-chrome.sh).
func chromePath(t *testing.T) string {
	t.Helper()
	if p := os.Getenv("CHROME_PATH"); p != "" {
		return p
	}
	_, file, _, _ := runtime.Caller(0)
	p := filepath.Join(filepath.Dir(file), "..", ".cache", "chrome", "chrome-headless-shell-linux64", "chrome-headless-shell")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	if os.Getenv("NEXUSPLAY_REQUIRE_INTEGRATION") != "" {
		t.Fatal("headless Chrome not found: run ./scripts/install-chrome.sh or set CHROME_PATH")
	}
	t.Skip("headless Chrome not found: run ./scripts/install-chrome.sh or set CHROME_PATH")
	return ""
}

// browser is one headless Chrome shared by a test's tabs.
type browser struct {
	t   *testing.T
	ctx context.Context
}

func newBrowser(t *testing.T) *browser {
	t.Helper()
	opts := []chromedp.ExecAllocatorOption{
		chromedp.ExecPath(chromePath(t)),
		chromedp.NoFirstRun,
		chromedp.NoDefaultBrowserCheck,
		chromedp.Headless,
		chromedp.NoSandbox,
		chromedp.DisableGPU,
		chromedp.Flag("hide-scrollbars", true),
		chromedp.Flag("mute-audio", true),
		chromedp.Flag("autoplay-policy", "no-user-gesture-required"),
		// Rendering settings that keep screenshots identical across machines.
		chromedp.Flag("font-render-hinting", "none"),
		chromedp.Flag("force-color-profile", "srgb"),
		chromedp.Flag("force-device-scale-factor", "1"),
		chromedp.Flag("disable-lcd-text", true),
		chromedp.Flag("lang", "en-US"),
	}
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), opts...)
	ctx, cancel := chromedp.NewContext(allocCtx, chromedp.WithErrorf(func(format string, args ...any) {
		// chromedp logs DOM events newer than the protocol version it knows; they are harmless.
		if !strings.HasPrefix(format, "unhandled node event") {
			t.Logf("chromedp: "+format, args...)
		}
	}))
	if err := chromedp.Run(ctx); err != nil {
		cancelAlloc()
		t.Fatalf("starting Chrome: %v", err)
	}
	t.Cleanup(func() { cancel(); cancelAlloc() })
	return &browser{t: t, ctx: ctx}
}

// tab is one page in its own browser context, so each tab has its own cookies
// (one tab per simulated person). Console errors and CSP violations fail the test.
type tab struct {
	t   *testing.T
	ctx context.Context

	mu       sync.Mutex
	problems []string
}

func (b *browser) newTab(width, height int64) *tab {
	b.t.Helper()
	ctx, cancel := chromedp.NewContext(b.ctx, chromedp.WithNewBrowserContext())
	b.t.Cleanup(cancel)
	// The first Run creates the tab and ties it to the context it is given, so
	// it must use the long-lived context, not one of run's timeout contexts.
	if err := chromedp.Run(ctx); err != nil {
		b.t.Fatalf("opening a tab: %v", err)
	}
	tb := &tab{t: b.t, ctx: ctx}
	chromedp.ListenTarget(ctx, func(ev any) {
		var msg string
		switch e := ev.(type) {
		case *cdpruntime.EventExceptionThrown:
			msg = "exception: " + e.ExceptionDetails.Text
			if e.ExceptionDetails.Exception != nil {
				msg += " " + e.ExceptionDetails.Exception.Description
			}
		case *cdpruntime.EventConsoleAPICalled:
			if e.Type == cdpruntime.APITypeError {
				var parts []string
				for _, a := range e.Args {
					parts = append(parts, strings.Trim(string(a.Value), `"`)+a.Description)
				}
				msg = "console.error: " + strings.Join(parts, " ")
			}
		case *cdplog.EventEntryAdded:
			if e.Entry.Level == cdplog.LevelError {
				msg = fmt.Sprintf("%s: %s (%s)", e.Entry.Source, e.Entry.Text, e.Entry.URL)
			}
		}
		if msg != "" {
			tb.mu.Lock()
			tb.problems = append(tb.problems, msg)
			tb.mu.Unlock()
		}
	})
	tb.run(cdplog.Enable(), chromedp.EmulateViewport(width, height))
	return tb
}

func (tb *tab) run(actions ...chromedp.Action) {
	tb.t.Helper()
	ctx, cancel := context.WithTimeout(tb.ctx, 60*time.Second)
	defer cancel()
	if err := chromedp.Run(ctx, actions...); err != nil {
		tb.t.Fatalf("browser: %v", err)
	}
}

// checkProblems fails the test on logged problems, except those containing
// one of the allowed strings (an expected 403, say).
func (tb *tab) checkProblems(allowed ...string) {
	tb.t.Helper()
	tb.mu.Lock()
	defer tb.mu.Unlock()
outer:
	for _, p := range tb.problems {
		for _, a := range allowed {
			if strings.Contains(p, a) {
				continue outer
			}
		}
		tb.t.Errorf("browser reported: %s", p)
	}
	tb.problems = nil
}

func (tb *tab) navigate(url string) {
	tb.t.Helper()
	tb.run(chromedp.Navigate(url))
}

func (tb *tab) click(sel string) {
	tb.t.Helper()
	tb.run(chromedp.WaitVisible(sel, chromedp.ByQuery), chromedp.Click(sel, chromedp.ByQuery))
}

func (tb *tab) waitVisible(sel string) {
	tb.t.Helper()
	tb.run(chromedp.WaitVisible(sel, chromedp.ByQuery))
}

func (tb *tab) fill(sel, value string) {
	tb.t.Helper()
	tb.run(chromedp.WaitVisible(sel, chromedp.ByQuery), chromedp.SetValue(sel, value, chromedp.ByQuery))
}

func (tb *tab) text(sel string) string {
	tb.t.Helper()
	var s string
	tb.run(chromedp.WaitVisible(sel, chromedp.ByQuery), chromedp.Text(sel, &s, chromedp.ByQuery))
	return strings.TrimSpace(s)
}

// eval runs JavaScript through DevTools (unaffected by the page's CSP).
func (tb *tab) eval(js string, out any) {
	tb.t.Helper()
	tb.run(chromedp.Evaluate(js, out))
}

// waitJS polls a JavaScript expression until it is truthy.
func (tb *tab) waitJS(js string, timeout time.Duration) {
	tb.t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		var ok bool
		ctx, cancel := context.WithTimeout(tb.ctx, 5*time.Second)
		err := chromedp.Run(ctx, chromedp.Evaluate("!!("+js+")", &ok))
		cancel()
		if err == nil && ok {
			return
		}
		if time.Now().After(deadline) {
			var state json.RawMessage
			chromedp.Run(tb.ctx, chromedp.Evaluate(`JSON.stringify({url: location.href, status: (document.getElementById('player-status')||{}).textContent})`, &state))
			tb.t.Fatalf("timed out waiting for %s (err=%v, page=%s)", js, err, state)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// waitHTMX waits until htmx has wired up the element, so a click goes through
// htmx instead of the browser's default action.
func (tb *tab) waitHTMX(sel string) {
	tb.t.Helper()
	tb.waitJS(fmt.Sprintf(`(document.querySelector(%q)||{})['htmx-internal-data']`, sel), 5*time.Second)
}

func (tb *tab) count(sel string) int {
	tb.t.Helper()
	var nodes []*cdp.Node
	tb.run(chromedp.Nodes(sel, &nodes, chromedp.ByQueryAll, chromedp.AtLeast(0)))
	return len(nodes)
}

// TestBrowserJourney walks through NexusPlay the way people use it: the first
// visitor sets up the server, publishes the library and watches; a second
// viewer votes a subtitle correction through.
func TestBrowserJourney(t *testing.T) {
	s := startServer(t, serverOpts{media: allFixtures})
	s.waitForMedia(5)
	b := newBrowser(t)
	admin := b.newTab(1200, 900)

	// Registering through the modal: the first account administers the server.
	admin.navigate(s.URL)
	if got := admin.text(".empty h2"); got != "Your library is empty" {
		t.Errorf("empty state heading = %q", got)
	}
	admin.click(`.top-bar a[href="/register"]`)
	admin.waitVisible("#modal[open] input[name=nickname]")
	if !strings.Contains(admin.text("#modal .alert-info"), "becomes the administrator") {
		t.Error("first registration does not explain it creates the administrator")
	}
	admin.fill("#modal input[name=nickname]", "alex")
	admin.fill("#modal input[name=email]", "alex@example.com")
	admin.fill("#modal input[name=password]", "integration-pass")
	admin.click("#modal button[type=submit]")
	admin.waitVisible(`.top-bar a[href="/admin"]`)

	// Publishing every file from the control panel, accepting the scanner's guesses.
	for i := 0; i < 5; i++ {
		admin.navigate(s.URL + "/admin")
		admin.click(".admin-table a.button")
		admin.waitVisible("form.form-grid")
		admin.click(`button[name="publish"]`)
		admin.waitVisible(".alert-success")
	}
	admin.navigate(s.URL + "/admin")
	if got := admin.text(".admin p.muted"); !strings.Contains(got, "Nothing new") {
		t.Errorf("control panel after publishing everything: %q", got)
	}

	// Home page: categories as tabs, the show as one collection tile.
	admin.navigate(s.URL)
	if n := admin.count("#tab-recent .tile"); n != 3 {
		t.Errorf("Just added has %d tiles, want 3 (two movies and the show)", n)
	}
	admin.click(`.tab[data-tab="tab-1"]`)
	admin.waitVisible("#tab-1 .tile")
	if got := admin.text("#tab-1 .tile-title"); got != "Test Show" {
		t.Errorf("TV Shows tab shows %q", got)
	}

	// Collection dialog: seasons as tabs, subscribe ribbon.
	admin.click("#tab-1 .tile")
	admin.waitVisible("#modal[open] .collection")
	if got := admin.text("#modal .collection-sub"); got != "2 Seasons, 2019" {
		t.Errorf("collection summary = %q", got)
	}
	if n := admin.count("#modal .tab-panel:not([hidden]) .episodes tbody tr"); n != 2 {
		t.Errorf("season 1 lists %d episodes, want 2", n)
	}
	admin.click(`#modal .season-tab:nth-child(2)`)
	admin.waitJS(`document.querySelector('#modal .tab-panel:not([hidden]) td a').textContent.includes('Episode 1')`, 5*time.Second)
	admin.waitHTMX("#modal .ribbon")
	admin.click("#modal .ribbon")
	admin.waitVisible("#modal .ribbon-on")
	admin.click("#modal [data-close-modal]")
	admin.waitJS(`!document.getElementById('modal').open`, 5*time.Second)

	// Direct play: the MP4 plays as-is, and the playing state dims the top bar.
	movie := s.mediaID(movieMP4)
	admin.navigate(fmt.Sprintf("%s/watch/%d", s.URL, movie))
	admin.waitJS(`document.getElementById('player').readyState >= 2`, 20*time.Second)
	// The custom control bar replaces the browser's controls.
	admin.waitJS(`!document.getElementById('player').controls && !document.getElementById('controls').hidden`, 5*time.Second)
	admin.click("#big-play")
	admin.waitJS(`document.getElementById('player').currentTime > 0.5`, 10*time.Second)
	// Hidden for real, not just marked hidden (a CSS display rule once won).
	admin.waitJS(`document.getElementById('top-bar').classList.contains('playing') && getComputedStyle(document.getElementById('big-play')).display === 'none'`, 5*time.Second)
	admin.waitJS(`document.getElementById('ctl-time').textContent !== '00:00' && document.getElementById('ctl-duration').textContent === '00:12'`, 5*time.Second)
	admin.click("#ctl-play")
	admin.waitJS(`document.getElementById('player').paused && getComputedStyle(document.getElementById('big-play')).display !== 'none'`, 5*time.Second)

	// Subtitle correction: pick the English track from the subtitles menu,
	// go to the first line, report it.
	admin.click("#ctl-captions")
	admin.click(`#captions-menu button:nth-child(2)`)
	admin.waitJS(`document.getElementById('player-container').dataset.subtitles === document.getElementById('player').textTracks[0].id && document.getElementById('captions-menu').hidden`, 5*time.Second)
	admin.eval(`document.getElementById('player').currentTime = 2`, nil)
	admin.waitVisible("#report-subtitle")
	admin.click("#report-subtitle")
	admin.waitVisible("#modal[open] #subtitle-report")
	admin.waitHTMX("#subtitle-report form")
	if got := admin.text("#subtitle-report form .original-sub"); got != "Hello thre!" {
		t.Errorf("report dialog shows %q, want the line on screen", got)
	}
	admin.fill("#subtitle-report textarea", "Hello there!")
	admin.click("#subtitle-report button[type=submit]")
	admin.waitJS(`(document.querySelector('#subtitle-report .alert-success')||{}).textContent`, 5*time.Second)

	// Resume: pausing saves the position; it shows under "Previously watching".
	admin.eval(`(() => { const v = document.getElementById('player'); v.currentTime = 9; v.play(); })()`, nil)
	admin.waitJS(`document.getElementById('player').currentTime > 9`, 10*time.Second)
	admin.eval(`document.getElementById('player').pause()`, nil)
	eventually(t, 5*time.Second, func() error {
		list, err := s.App.Store.InProgress(context.Background(), 1, 10)
		if err != nil || len(list) != 1 || list[0].Position < 9 {
			return fmt.Errorf("in progress = %+v, %v", list, err)
		}
		return nil
	})
	admin.navigate(s.URL)
	admin.click(`button[popovertarget="resume-pop"]`)
	admin.waitVisible("#resume-pop-body li")
	if got := admin.text("#resume-pop-body li"); !strings.Contains(got, "The Test Movie") {
		t.Errorf("Previously watching = %q", got)
	}
	admin.checkProblems()

	// A second viewer votes for the correction; with two votes it is applied.
	viewer := b.newTab(1200, 900)
	viewer.navigate(s.URL + "/register")
	viewer.fill("input[name=nickname]", "sam")
	viewer.fill("input[name=email]", "sam@example.com")
	viewer.fill("input[name=password]", "integration-pass")
	viewer.click("form button[type=submit]")
	viewer.waitVisible(`.top-bar form[action="/logout"] button`)
	if viewer.count(`.top-bar a[href="/admin"]`) != 0 {
		t.Error("the second account must not be an administrator")
	}
	viewer.navigate(fmt.Sprintf("%s/watch/%d", s.URL, movie))
	viewer.waitJS(`document.getElementById('player').readyState >= 1`, 20*time.Second)
	viewer.eval(`(() => { const v = document.getElementById('player'); v.textTracks[0].mode = 'showing'; v.currentTime = 2; })()`, nil)
	viewer.click("#report-subtitle")
	viewer.waitVisible("#subtitle-report .changes .vote-box")
	viewer.waitHTMX("#subtitle-report .changes .vote-box")
	viewer.click("#subtitle-report .changes .vote-box")
	viewer.waitJS(`document.querySelector('#subtitle-report .alert')`, 5*time.Second)
	if got := viewer.text("#subtitle-report .alert"); !strings.Contains(got, "Change applied") {
		t.Fatalf("after voting: %q", got)
	}
	sub, _ := s.App.Store.Subtitles(context.Background(), movie)
	viewer.navigate(fmt.Sprintf("%s/subtitles/%d", s.URL, sub[0].ID))
	if got := viewer.text("body"); !strings.Contains(got, "Hello there!") {
		t.Errorf("subtitle file after the vote:\n%s", got)
	}

	// HLS: the AVI is transcoded on the fly and plays through hls.js.
	viewer.navigate(fmt.Sprintf("%s/watch/%d", s.URL, s.mediaID(movieAVI)))
	viewer.waitJS(`document.getElementById('player').readyState >= 2`, 60*time.Second)
	var start float64
	viewer.eval(`document.getElementById('player').currentTime`, &start)
	if start > 1 {
		t.Errorf("transcoded video started at %.1fs, want the beginning", start)
	}
	viewer.eval(`document.getElementById('player').play()`, nil)
	viewer.waitJS(`document.getElementById('player').currentTime > 0.5`, 20*time.Second)
	viewer.checkProblems()

	// Admin-only pages stay closed to regular users.
	viewer.navigate(s.URL + "/admin")
	if got := viewer.text("h1"); got != "Access denied" {
		t.Errorf("regular user at /admin sees %q", got)
	}
	viewer.checkProblems("403")
}

// TestSearchAndLogout covers logging in from the full-page form, live search
// and logging out.
func TestSearchAndLogout(t *testing.T) {
	s := startServer(t, serverOpts{media: allFixtures})
	s.waitForMedia(5)
	s.publishAll()
	s.register("alex")
	b := newBrowser(t)
	tb := b.newTab(1200, 900)

	tb.navigate(s.URL + "/login")
	tb.fill("input[name=email]", "alex@example.com")
	tb.fill("input[name=password]", "wrong password")
	tb.click("form button[type=submit]")
	tb.waitJS(`(document.querySelector('#auth-form .alert-error')||{textContent:''}).textContent.includes('Wrong')`, 5*time.Second)
	tb.fill("input[name=password]", "integration-pass")
	tb.click("form button[type=submit]")
	tb.waitVisible(`.top-bar form[action="/logout"] button`)

	// Typing opens the search drawer over the page and dims the page;
	// a query with no results, and Escape, close it again.
	tb.run(chromedp.SendKeys(`input[name=q]`, "old", chromedp.ByQuery))
	tb.waitJS(`document.querySelector('#search-drawer.open #search-results .tab-label')`, 5*time.Second)
	if n := tb.count("#search-results .tile"); n != 1 {
		t.Errorf("search for 'old' found %d tiles, want 1", n)
	}
	if tb.count("#page-cover.dimmed") != 1 {
		t.Error("the page under the open drawer is not dimmed")
	}
	tb.run(chromedp.SendKeys(`input[name=q]`, "zzz", chromedp.ByQuery))
	tb.waitJS(`!document.querySelector('#search-drawer.open') && !document.querySelector('#page-cover.dimmed')`, 5*time.Second)
	tb.run(chromedp.SendKeys(`input[name=q]`, "\b\b\b", chromedp.ByQuery))
	tb.waitJS(`document.querySelector('#search-drawer.open')`, 5*time.Second)
	tb.run(chromedp.KeyEvent(kb.Escape))
	tb.waitJS(`!document.querySelector('#search-drawer.open') && document.querySelector('input[name=q]').value === ''`, 5*time.Second)

	// Without JavaScript, /search is the home page with the drawer open.
	// There the library box ends in its dark strip, so check its corners.
	tb.navigate(s.URL + "/search?q=old")
	assertCleanCorners(t, tb, tb.screenshot(), "#page-cover .library-panel")
	tb.navigate(s.URL + "/search?q=nothing-matches")
	if got := tb.text("#search-results .message p"); got != "Nothing matches your search." {
		t.Errorf("no-results message = %q", got)
	}

	tb.click(`.top-bar form[action="/logout"] button`)
	tb.waitVisible(`.top-bar a[href="/login"]`)
	tb.checkProblems()
}

// TestTranscodedVideoPlaysWhileConverting covers the first play of a file
// that needs converting, while ffmpeg is still writing the playlist. Two
// regressions are guarded here: Chrome's own HLS player (which Chrome now
// claims to support) failing on the growing playlist, and hls.js treating the
// growing playlist as live and starting near its end instead of at 0.
func TestTranscodedVideoPlaysWhileConverting(t *testing.T) {
	// Encode slowly enough that playback starts before the transcode finishes.
	slow := filepath.Join(t.TempDir(), "slow-ffmpeg")
	ffmpeg, _ := exec.LookPath("ffmpeg")
	os.WriteFile(slow, []byte("#!/bin/sh\nexec "+ffmpeg+" -re \"$@\"\n"), 0o755)

	s := startServer(t, serverOpts{media: []string{movieAVI}, ffmpeg: slow})
	s.waitForMedia(1)
	s.publishAll()
	b := newBrowser(t)
	tb := b.newTab(1200, 900)
	tb.navigate(fmt.Sprintf("%s/watch/%d", s.URL, s.mediaID(movieAVI)))
	tb.waitJS(`document.getElementById('player').readyState >= 2 || document.getElementById('player').error`, 60*time.Second)
	var start float64
	tb.eval(`document.getElementById('player').currentTime`, &start)
	if start > 1 {
		t.Errorf("playback started at %.1fs, want the beginning", start)
	}
	tb.eval(`document.getElementById('player').play()`, nil)
	tb.waitJS(`document.getElementById('player').currentTime > 1`, 20*time.Second)
	// While ffmpeg is still writing, the browser knows only part of the
	// length; the controls show the whole length from the server.
	var browserLen, shown float64
	tb.eval(`document.getElementById('player').duration`, &browserLen)
	tb.eval(`(t => +t[0] * 60 + +t[1])(document.getElementById('ctl-duration').textContent.split(':'))`, &shown)
	m, err := s.App.Store.MediaByID(context.Background(), s.mediaID(movieAVI))
	if err != nil {
		t.Fatal(err)
	}
	if want := m.Duration; shown < want-1 || shown > want+1 {
		t.Errorf("controls show a length of %.0fs (browser knows %.1fs), want the file's %.0fs", shown, browserLen, want)
	}
	var errMsg string
	tb.eval(`(document.getElementById('player').error || {message: ''}).message`, &errMsg)
	if errMsg != "" {
		t.Errorf("video error: %s", errMsg)
	}
	tb.checkProblems()
}

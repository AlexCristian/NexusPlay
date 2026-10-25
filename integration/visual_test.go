//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"

	"github.com/AlexCristian/NexusPlay/internal/library"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// Screenshot comparison. Each screen is rendered in the pinned Chrome and
// compared with testdata/screenshots/<name>.png. A pixel counts as different
// when any colour channel moves by more than channelTolerance (absorbing
// anti-aliasing noise); a screen fails when more than maxDiffRatio of its
// pixels differ. On failure the actual image and a diff (changed pixels in
// red) are written next to the references under failures/.
//
// To accept intended UI changes: go test -tags integration ./integration/ -run TestScreens -update
const (
	channelTolerance = 8
	maxDiffRatio     = 0.002
	screenshotDir    = "testdata/screenshots"
)

// stabilize removes sources of run-to-run noise: transitions, the blinking
// caret, video frames (hidden, keeping their layout), how much of the video
// the seek bar shows as buffered, and fallback system
// fonts (monospace and form controls use the bundled Open Sans instead).
const stabilize = `(() => {
  document.querySelectorAll('style[data-stabilize]').forEach(e => e.remove()); // a previous screenshot's
  const s = document.createElement('style');
  s.dataset.stabilize = '';
  s.textContent = '*, *::before, *::after { transition: none !important; animation: none !important; caret-color: transparent !important; }' +
    ' video { visibility: hidden !important; }' +
    ' .path, code, input, textarea, button, select { font-family: "Open Sans", sans-serif !important; }' +
    ' .rail { --loaded: 0% !important; }';
  document.head.appendChild(s);
  // Lazy images below the fold would never load in a headless screenshot.
  document.querySelectorAll('img[loading="lazy"]').forEach(i => { i.loading = 'eager'; });
  return true;
})()`

// roundedParts names, for some screens, a rounded box whose corners join a
// colored header to a body of another color: the place a light fringe shows.
var roundedParts = map[string]string{
	"collection-dialog":            "#modal",
	"collection-dialog-subscribed": "#modal",
	"register-dialog":              "#modal",
	"subtitle-report":              "#modal",
	"login-page":                   ".auth-panel",
}

func TestScreens(t *testing.T) {
	s := startServer(t, serverOpts{media: allFixtures})
	s.waitForMedia(5)
	s.publishAll(movieAVI) // leave one file pending for the control panel
	s.register("alex")     // the administrator
	s.register("sam")      // a regular viewer

	empty := startServer(t, serverOpts{})
	b := newBrowser(t)

	anon := b.newTab(1200, 900)
	admin := b.newTab(1200, 900)
	viewer := b.newTab(1200, 900)
	admin.login(s, "alex")
	viewer.login(s, "sam")
	movie := s.mediaID(movieMP4)

	screens := []struct {
		name string
		tab  *tab
		load func(*tab)
	}{
		{"home-empty", anon, func(tb *tab) { tb.navigate(empty.URL) }},
		{"home-anonymous", anon, func(tb *tab) { tb.navigate(s.URL) }},
		{"home-tv-tab", viewer, func(tb *tab) {
			tb.navigate(s.URL)
			tb.click(`.tab[data-tab="tab-1"]`)
		}},
		{"register-dialog", anon, func(tb *tab) {
			tb.navigate(s.URL)
			tb.click(`.top-bar a[href="/register"]`)
			tb.waitVisible("#modal[open] input[name=nickname]")
		}},
		{"login-page", anon, func(tb *tab) { tb.navigate(s.URL + "/login") }},
		{"search-results", viewer, func(tb *tab) {
			tb.navigate(s.URL)
			tb.waitHTMX("input[name=q]")
			tb.run(chromedp.SendKeys(`input[name=q]`, "test", chromedp.ByQuery))
			tb.waitJS(`document.querySelector('#search-drawer.open .tile')`, 5*time.Second)
		}},
		{"collection-dialog", viewer, func(tb *tab) {
			tb.navigate(s.URL)
			tb.click(`.tab[data-tab="tab-1"]`)
			tb.click("#tab-1 .tile")
			tb.waitVisible("#modal[open] .episodes")
		}},
		{"collection-dialog-subscribed", viewer, func(tb *tab) {
			tb.waitHTMX("#modal .ribbon")
			tb.click("#modal .ribbon")
			tb.waitVisible("#modal .ribbon-on")
			// Move the mouse away so the screenshot shows the resting state, not
			// the hover preview of unsubscribing.
			tb.run(chromedp.MouseClickXY(5, 600))
			tb.waitJS(`!document.getElementById('modal').open`, 5*time.Second)
			tb.click("#tab-1 .tile")
			tb.waitVisible("#modal[open] .ribbon-on")
		}},
		{"watch-movie", admin, func(tb *tab) {
			tb.navigate(fmt.Sprintf("%s/watch/%d", s.URL, movie))
			tb.waitJS(`document.getElementById('player').readyState >= 1`, 20*time.Second)
			tb.eval(`document.getElementById('player').textTracks[0].mode = 'showing'`, nil)
			tb.waitVisible("#report-subtitle")
		}},
		{"subtitle-report", viewer, func(tb *tab) {
			s.proposeCorrection(tb.t, movie, 2000, "alex", "Hello there!")
			tb.navigate(fmt.Sprintf("%s/watch/%d", s.URL, movie))
			tb.waitJS(`document.getElementById('player').readyState >= 1`, 20*time.Second)
			tb.eval(`(() => { const v = document.getElementById('player'); v.textTracks[0].mode = 'showing'; v.currentTime = 2; })()`, nil)
			tb.click("#report-subtitle")
			tb.waitVisible("#modal[open] #subtitle-report")
		}},
		{"resume-popover", viewer, func(tb *tab) {
			tb.navigate(s.URL)
			tb.click(`button[popovertarget="resume-pop"]`)
			// The subtitle-report screen paused the movie at 0:02, which saved progress.
			tb.waitVisible("#resume-pop-body li")
		}},
		{"control-panel", admin, func(tb *tab) { tb.navigate(s.URL + "/admin") }},
		{"review-media", admin, func(tb *tab) {
			tb.navigate(fmt.Sprintf("%s/admin/media/%d", s.URL, s.mediaID(movieAVI)))
		}},
		{"edit-collection", admin, func(tb *tab) {
			tb.navigate(fmt.Sprintf("%s/admin/collections/%d", s.URL, s.collectionWithPoster(tb.t)))
		}},
		{"access-denied", viewer, func(tb *tab) { tb.navigate(s.URL + "/admin") }},
	}
	for _, sc := range screens {
		t.Run(sc.name, func(t *testing.T) {
			sc.tab.t = t
			sc.load(sc.tab)
			shot := sc.tab.screenshot()
			compareScreenshot(t, sc.name, shot)
			if corner, ok := roundedParts[sc.name]; ok {
				assertCleanCorners(t, sc.tab, shot, corner)
			}
		})
	}

	// A failed conversion, listed with its reason and a Retry button. The
	// ffmpeg here fails every conversion the same way, so the screen is stable.
	t.Run("control-panel-conversion-failed", func(t *testing.T) {
		broken := filepath.Join(t.TempDir(), "ffmpeg-fails-conversions")
		ffmpeg, _ := exec.LookPath("ffmpeg")
		os.WriteFile(broken, []byte("#!/bin/sh\ncase \"$*\" in *-progress*) echo 'Conversion failed!' >&2; exit 1;; esac\nexec "+ffmpeg+" \"$@\"\n"), 0o755)
		fs := startServer(t, serverOpts{media: []string{movieAVI}, convert: true, ffmpeg: broken})
		fs.waitForMedia(1)
		fs.register("alex")
		eventually(t, 30*time.Second, func() error {
			f, err := fs.App.Store.ConversionFailures(context.Background())
			if err != nil || len(f) != 1 {
				return fmt.Errorf("waiting for the failure to be recorded: %+v %v", f, err)
			}
			return nil
		})
		tb := b.newTab(1200, 900)
		tb.login(fs, "alex")
		tb.navigate(fs.URL + "/admin")
		compareScreenshot(t, "control-panel-conversion-failed", tb.screenshot())
	})

	// Phone width.
	phone := b.newTab(390, 844)
	t.Run("home-phone", func(t *testing.T) {
		phone.t = t
		phone.navigate(s.URL)
		compareScreenshot(t, "home-phone", phone.screenshot())
	})
	t.Run("collection-dialog-phone", func(t *testing.T) {
		phone.t = t
		phone.navigate(s.URL)
		phone.click(`.tab[data-tab="tab-1"]`)
		phone.click("#tab-1 .tile")
		phone.waitVisible("#modal[open] .episodes")
		// The season tabs scroll right up to the dialog's edge, and opening
		// the dialog focuses the dialog, not its close button (which then
		// showed a focus ring).
		var gap float64
		phone.eval(`document.getElementById('modal').getBoundingClientRect().right -
			document.querySelector('#modal .season-tabs').getBoundingClientRect().right`, &gap)
		if gap > 1 {
			t.Errorf("season tabs end %.0fpx before the dialog's edge", gap)
		}
		var focused string
		phone.eval(`document.activeElement.id || document.activeElement.className`, &focused)
		if focused != "modal" {
			t.Errorf("focus on open = %q, want the dialog", focused)
		}
		compareScreenshot(t, "collection-dialog-phone", phone.screenshot())
	})
	// An episode's page on a phone, signed in as the administrator (the
	// longest top bar).
	t.Run("watch-episode-phone", func(t *testing.T) {
		adminPhone := b.newTab(390, 844)
		adminPhone.t = t
		adminPhone.login(s, "alex")
		adminPhone.navigate(fmt.Sprintf("%s/watch/%d", s.URL, s.mediaID(episode1)))
		adminPhone.waitJS(`document.getElementById('player').readyState >= 1`, 20*time.Second)
		compareScreenshot(t, "watch-episode-phone", adminPhone.screenshot())
	})
}

// login signs a tab in through the login page.
func (tb *tab) login(s *server, nick string) {
	tb.t.Helper()
	tb.navigate(s.URL + "/login")
	tb.fill("input[name=email]", nick+"@example.com")
	tb.fill("input[name=password]", "integration-pass")
	tb.click("form button[type=submit]")
	tb.waitVisible(`.top-bar form[action="/logout"] button`)
}

// screenshot captures the whole page once fonts and images have loaded.
func (tb *tab) screenshot() []byte {
	tb.t.Helper()
	return tb.screenshotWith(stabilize)
}

// screenshotWith is screenshot with another stabilizing script.
func (tb *tab) screenshotWith(stabilize string) []byte {
	tb.t.Helper()
	tb.eval(stabilize, nil)
	tb.waitJS(`document.fonts.status === 'loaded' && [...document.images].every(i => i.complete)`, 10*time.Second)
	// Let htmx settle classes and the layout finish after the style change.
	tb.waitJS(`!document.querySelector('.htmx-settling, .htmx-swapping, .htmx-request')`, 5*time.Second)
	time.Sleep(150 * time.Millisecond)
	var buf []byte
	tb.run(chromedp.FullScreenshot(&buf, 100)) // quality 100 means PNG
	return buf
}

func compareScreenshot(t *testing.T, name string, actualPNG []byte) {
	t.Helper()
	ref := filepath.Join(screenshotDir, name+".png")
	if *update {
		if err := os.MkdirAll(screenshotDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(ref, actualPNG, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", ref)
		return
	}
	wantPNG, err := os.ReadFile(ref)
	if os.IsNotExist(err) {
		saveFailure(t, name, actualPNG, nil)
		t.Fatalf("no reference screenshot %s; run with -update to create it", ref)
	} else if err != nil {
		t.Fatal(err)
	}
	want, err := png.Decode(bytes.NewReader(wantPNG))
	if err != nil {
		t.Fatalf("decoding %s: %v", ref, err)
	}
	got, err := png.Decode(bytes.NewReader(actualPNG))
	if err != nil {
		t.Fatalf("decoding screenshot: %v", err)
	}
	diff, changed, total := diffImages(want, got)
	if diff == nil {
		saveFailure(t, name, actualPNG, nil)
		t.Fatalf("size changed: reference %v, now %v", want.Bounds().Size(), got.Bounds().Size())
	}
	if ratio := float64(changed) / float64(total); ratio > maxDiffRatio {
		saveFailure(t, name, actualPNG, diff)
		t.Errorf("%s differs from the reference in %d pixels (%.3f%%, limit %.3f%%)",
			name, changed, ratio*100, maxDiffRatio*100)
	}
}

// diffImages returns an image highlighting changed pixels in red over a faded
// copy of the reference, the number of changed pixels and the total. The
// image is nil when the sizes differ.
func diffImages(want, got image.Image) (*image.RGBA, int, int) {
	wb, gb := want.Bounds(), got.Bounds()
	if wb.Size() != gb.Size() {
		return nil, 0, 0
	}
	out := image.NewRGBA(image.Rect(0, 0, wb.Dx(), wb.Dy()))
	changed := 0
	for y := 0; y < wb.Dy(); y++ {
		for x := 0; x < wb.Dx(); x++ {
			r1, g1, b1, _ := want.At(wb.Min.X+x, wb.Min.Y+y).RGBA()
			r2, g2, b2, _ := got.At(gb.Min.X+x, gb.Min.Y+y).RGBA()
			if far(r1, r2) || far(g1, g2) || far(b1, b2) {
				changed++
				out.Set(x, y, color.RGBA{R: 255, A: 255})
				continue
			}
			gray := uint8((r1>>8 + g1>>8 + b1>>8) / 3)
			gray = 200 + gray/5 // faded so the red stands out
			out.Set(x, y, color.RGBA{R: gray, G: gray, B: gray, A: 255})
		}
	}
	return out, changed, wb.Dx() * wb.Dy()
}

func far(a, b uint32) bool {
	d := int(a>>8) - int(b>>8)
	return d > channelTolerance || d < -channelTolerance
}

func saveFailure(t *testing.T, name string, actualPNG []byte, diff *image.RGBA) {
	t.Helper()
	dir := os.Getenv("NEXUSPLAY_SCREENSHOT_FAILURES")
	if dir == "" {
		dir = filepath.Join(screenshotDir, "failures")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Log(err)
		return
	}
	actual := filepath.Join(dir, name+".actual.png")
	os.WriteFile(actual, actualPNG, 0o644)
	if diff != nil {
		var buf bytes.Buffer
		png.Encode(&buf, diff)
		os.WriteFile(filepath.Join(dir, name+".diff.png"), buf.Bytes(), 0o644)
	}
	t.Logf("wrote %s (and the diff, if any) for inspection", actual)
}

// collectionWithPoster gives the first collection a plain poster image, so
// its edit page shows the poster and the box to remove it. The poster is
// removed when the test ends, so other screens stay as they are.
func (s *server) collectionWithPoster(t *testing.T) int64 {
	t.Helper()
	ctx := context.Background()
	colls, err := s.App.Store.Collections(ctx)
	if err != nil || len(colls) == 0 {
		t.Fatalf("no collection to edit: %v", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 140, 210))
	for i := 0; i < len(img.Pix); i += 4 {
		copy(img.Pix[i:], []byte{0x3a, 0x5f, 0x8a, 0xff})
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.App.Cfg.PosterDir(), "test-poster.png"), buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	c := colls[0]
	err = s.App.Store.UpdateCollection(ctx, c.ID, store.CollectionUpdate{
		Name: c.Name, Category: c.Category, Year: c.Year, Overview: c.Overview, Poster: "test-poster.png"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		err := s.App.Store.UpdateCollection(ctx, c.ID, store.CollectionUpdate{
			Name: c.Name, Category: c.Category, Year: c.Year, Overview: c.Overview, RemovePoster: true})
		if err != nil {
			t.Error(err)
		}
	})
	return c.ID
}

// proposeCorrection has user nick propose text for the line of the media's
// first subtitle track that shows at ms.
func (s *server) proposeCorrection(t *testing.T, mediaID, ms int64, nick, text string) {
	t.Helper()
	ctx := context.Background()
	st := s.App.Store
	subs, err := st.Subtitles(ctx, mediaID)
	if err != nil || len(subs) == 0 {
		t.Fatalf("no subtitles to correct: %v", err)
	}
	cue, err := st.CueAt(ctx, subs[0].ID, ms)
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.UserByEmail(ctx, nick+"@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.ProposeChange(ctx, cue.ID, u.ID, text, 4); err != nil {
		t.Fatal(err)
	}
}

// A show with more seasons than fit across a phone: the season row scrolls
// sideways up to the dialog's edge, and picking a season scrolls it into
// the middle. Double episodes show as a range ("1–2").
func TestLongSeasonList(t *testing.T) {
	s := startServer(t, serverOpts{})
	ctx := context.Background()
	st := s.App.Store
	coll, err := st.CreateCollection(ctx, store.Collection{Name: "Long Show", Category: "TV Shows", Year: "1987"})
	if err != nil {
		t.Fatal(err)
	}
	for season := 1; season <= 7; season++ {
		eps := []string{"S%02dE01.mkv", "S%02dE02.mkv"}
		if season == 1 {
			eps = []string{"S%02dE01E02 Encounter.mkv", "S%02dE03 The Naked Now.mkv"}
		}
		for _, name := range eps {
			path := fmt.Sprintf("Long Show/Season %d/Long Show "+name, season, season)
			g := library.ParsePath(path)
			id, _, err := st.InsertScannedMedia(ctx, store.ScannedMedia{Path: path, Title: g.Title, Season: g.Season, Episode: g.Episode, VideoCodec: "h264"})
			if err != nil {
				t.Fatal(err)
			}
			u := store.MediaUpdate{Title: g.Title, CollectionID: coll, Season: g.Season, Episode: g.Episode}
			if err := st.UpdateMedia(ctx, id, u, true); err != nil {
				t.Fatal(err)
			}
		}
	}
	b := newBrowser(t)
	tb := b.newTab(390, 844)
	tb.navigate(s.URL)
	tb.waitHTMX("#library .tile")
	tb.click("#library .tile")
	tb.waitVisible("#modal[open] .episodes")

	if got := tb.text("#modal .tab-panel:not([hidden]) tbody tr:first-child td:first-child"); got != "1–2" {
		t.Errorf("double episode numbered %q, want 1–2", got)
	}
	var overflow bool
	tb.eval(`(r => r.scrollWidth > r.clientWidth)(document.querySelector('#modal .season-tabs'))`, &overflow)
	if !overflow {
		t.Fatal("seven seasons fit on one phone row; the test needs a row that scrolls")
	}
	tb.click(`#modal .season-tab:last-child`)
	// Smooth scrolling: wait until it settles with the last season in view.
	tb.waitJS(`(() => {
		const row = document.querySelector('#modal .season-tabs');
		const last = row.lastElementChild.getBoundingClientRect();
		const box = document.getElementById('modal').getBoundingClientRect();
		return row.scrollLeft + row.clientWidth >= row.scrollWidth - 1 && last.right <= box.right && last.left >= row.getBoundingClientRect().left;
	})()`, 5*time.Second)
	compareScreenshot(t, "collection-dialog-seasons-phone", tb.screenshot())
}

// assertCleanCorners checks the four rounded corners of the element sel in
// a screenshot: no pixel may be brighter than both the element's color just
// inside the corner and the page's color just outside it. A brighter pixel
// is a light fringe: a background under a darker part showing through the
// antialiased curve.
func assertCleanCorners(t *testing.T, tb *tab, shot []byte, sel string) {
	t.Helper()
	var r struct{ Left, Top, Right, Bottom, TL, TR, BL, BR float64 }
	tb.eval(fmt.Sprintf(`(e => { const b = e.getBoundingClientRect(), st = getComputedStyle(e);
		return {left: b.left, top: b.top, right: b.right, bottom: b.bottom,
			tl: parseFloat(st.borderTopLeftRadius), tr: parseFloat(st.borderTopRightRadius),
			bl: parseFloat(st.borderBottomLeftRadius), br: parseFloat(st.borderBottomRightRadius)}; })(document.querySelector(%q))`, sel), &r)
	img, err := png.Decode(bytes.NewReader(shot))
	if err != nil {
		t.Fatal(err)
	}
	light := func(x, y int) int {
		cr, cg, cb, _ := img.At(x, y).RGBA()
		return int((cr + cg + cb) / 3 >> 8)
	}
	if max(r.TL, r.TR, r.BL, r.BR) < 2 {
		t.Fatalf("%s has no rounded corners", sel)
	}
	l, tp, rt, bt := int(r.Left), int(r.Top), int(r.Right)-1, int(r.Bottom)-1
	for _, c := range []struct {
		name   string
		x, y   int // the corner pixel
		dx, dy int // pointing inwards
		radius float64
	}{{"top left", l, tp, 1, 1, r.TL}, {"top right", rt, tp, -1, 1, r.TR}, {"bottom left", l, bt, 1, -1, r.BL}, {"bottom right", rt, bt, -1, -1, r.BR}} {
		rad := int(c.radius)
		if rad < 2 {
			continue // a square corner
		}
		inside := light(c.x+c.dx*rad, c.y+c.dy*rad)
		outside := light(c.x-c.dx*2, c.y-c.dy*2)
		limit := max(inside, outside) + 12
		for i := 0; i < rad; i++ {
			for j := 0; j < rad; j++ {
				if v := light(c.x+c.dx*i, c.y+c.dy*j); v > limit {
					t.Errorf("%s: light fringe at its %s corner: brightness %d, inside %d, outside %d", sel, c.name, v, inside, outside)
					return
				}
			}
		}
	}
}

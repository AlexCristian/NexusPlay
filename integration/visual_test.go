//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
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
// caret, video frames (hidden, keeping their layout), and fallback system
// fonts (monospace and form controls use the bundled Open Sans instead).
const stabilize = `(() => {
  const s = document.createElement('style');
  s.textContent = '*, *::before, *::after { transition: none !important; animation: none !important; caret-color: transparent !important; }' +
    ' video { visibility: hidden !important; }' +
    ' .path, code, input, textarea, button, select { font-family: "Open Sans", sans-serif !important; }';
  document.head.appendChild(s);
  // Lazy images below the fold would never load in a headless screenshot.
  document.querySelectorAll('img[loading="lazy"]').forEach(i => { i.loading = 'eager'; });
  return true;
})()`

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
		{"search-results", viewer, func(tb *tab) { tb.navigate(s.URL + "/search?q=test") }},
		{"collection-dialog", viewer, func(tb *tab) {
			tb.navigate(s.URL)
			tb.click(`.tab[data-tab="tab-1"]`)
			tb.click("#tab-1 .tile")
			tb.waitVisible("#modal[open] .episodes")
		}},
		{"watch-movie", admin, func(tb *tab) {
			tb.navigate(fmt.Sprintf("%s/watch/%d", s.URL, movie))
			tb.waitJS(`document.getElementById('player').readyState >= 1`, 20*time.Second)
			tb.eval(`document.getElementById('player').textTracks[0].mode = 'showing'`, nil)
			tb.waitVisible("#report-subtitle")
		}},
		{"subtitle-report", viewer, func(tb *tab) {
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
		{"access-denied", viewer, func(tb *tab) { tb.navigate(s.URL + "/admin") }},
	}
	for _, sc := range screens {
		t.Run(sc.name, func(t *testing.T) {
			sc.tab.t = t
			sc.load(sc.tab)
			compareScreenshot(t, sc.name, sc.tab.screenshot())
		})
	}

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
		compareScreenshot(t, "collection-dialog-phone", phone.screenshot())
	})
}

// login signs a tab in through the login page.
func (tb *tab) login(s *server, nick string) {
	tb.t.Helper()
	tb.navigate(s.URL + "/login")
	tb.fill("input[name=email]", nick+"@example.com")
	tb.fill("input[name=password]", "integration-pass")
	tb.click("form button[type=submit]")
	tb.waitVisible(".top-bar .top-text")
}

// screenshot captures the whole page once fonts and images have loaded.
func (tb *tab) screenshot() []byte {
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

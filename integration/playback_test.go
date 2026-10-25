//go:build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// Screenshots of video actually playing, with the frames visible: still
// color bars, so every run looks the same. They catch anything left over
// the picture (the play button, the loading spinner, status text) and a
// wrong length or position in the controls.

// playingStabilize keeps video frames visible but hides what moves while
// playing: the elapsed time and how far the seek bar is filled.
var playingStabilize = strings.Replace(stabilize, `' video { visibility: hidden !important; }' +`,
	`' #ctl-time { visibility: hidden !important; } .rail { --fill: 0% !important; }' +`, 1)

// pausedStabilize keeps everything visible: a paused player is still.
var pausedStabilize = strings.Replace(stabilize, `' video { visibility: hidden !important; }' +`, `'' +`, 1)

func TestPlaybackScreens(t *testing.T) {
	if !strings.Contains(stabilize, `' video { visibility: hidden !important; }' +`) {
		t.Fatal("the stabilize script changed; update the playback variants")
	}
	// ffmpeg at real-time speed, so the episode is still being converted
	// while it plays, as on a slow NAS.
	slow := filepath.Join(t.TempDir(), "slow-ffmpeg")
	ffmpeg, _ := exec.LookPath("ffmpeg")
	os.WriteFile(slow, []byte("#!/bin/sh\nexec "+ffmpeg+" -re \"$@\"\n"), 0o755)

	s := startServer(t, serverOpts{media: []string{barsMP4, barsEpisode}, ffmpeg: slow})
	s.waitForMedia(2)
	s.publishAll()
	s.register("alex")
	// A synopsis under the episode's title, as TMDB provides.
	ctx := context.Background()
	ep, err := s.App.Store.MediaByID(ctx, s.mediaID(barsEpisode))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.App.Store.UpdateMedia(ctx, ep.ID, store.MediaUpdate{Title: ep.Title, CollectionID: ep.CollectionID,
		Season: ep.Season, Episode: ep.Episode, Overview: "Seven colored bars and a tone, for thirty seconds."}, true); err != nil {
		t.Fatal(err)
	}
	b := newBrowser(t)

	// Plays the video from the start and waits until it is past at.
	playUntil := func(tb *tab, at float64) {
		tb.waitJS(`!document.getElementById('controls').hidden`, 10*time.Second)
		tb.click("#big-play")
		tb.waitJS(fmt.Sprintf(`(v => !v.paused && v.currentTime > %g)(document.getElementById('player'))`, at), 60*time.Second)
	}
	// Pauses and goes to exactly at seconds.
	pauseAt := func(tb *tab, at float64) {
		tb.eval(fmt.Sprintf(`(v => { v.pause(); v.currentTime = %g; })(document.getElementById('player'))`, at), nil)
		tb.waitJS(fmt.Sprintf(`(v => v.paused && !v.seeking && Math.abs(v.currentTime - %g) < 0.05)(document.getElementById('player'))`, at), 10*time.Second)
	}
	// Nothing may cover the picture while it plays.
	assertClearWhilePlaying := func(t *testing.T, tb *tab) {
		t.Helper()
		var covered []string
		tb.eval(`['big-play', 'player-spinner', 'player-status'].map(id => document.getElementById(id))
			.filter(e => e && getComputedStyle(e).display !== 'none').map(e => e.id)`, &covered)
		if len(covered) > 0 {
			t.Errorf("shown over the playing video: %v", covered)
		}
	}
	// The browser must still be receiving the converted file, or the test
	// would not cover a length that grows as it arrives.
	assertStillConverting := func(t *testing.T, tb *tab) {
		t.Helper()
		var d float64
		tb.eval(`document.getElementById('player').duration`, &d)
		if d > 29 {
			t.Fatalf("the conversion finished (browser length %.1fs) before the screenshot", d)
		}
	}

	t.Run("direct", func(t *testing.T) {
		tb := b.newTab(1200, 900)
		tb.t = t
		tb.login(s, "alex")
		tb.navigate(fmt.Sprintf("%s/watch/%d", s.URL, s.mediaID(barsMP4)))
		playUntil(tb, 1.5)
		assertClearWhilePlaying(t, tb)
		compareScreenshot(t, "playback-playing", tb.screenshotWith(playingStabilize))
		pauseAt(tb, 3)
		compareScreenshot(t, "playback-paused", tb.screenshotWith(pausedStabilize))
	})

	t.Run("converting-phone", func(t *testing.T) {
		tb := b.newTab(390, 844)
		tb.t = t
		tb.login(s, "alex")
		tb.navigate(fmt.Sprintf("%s/watch/%d", s.URL, s.mediaID(barsEpisode)))
		// Before playing, only the play button is over the picture: no
		// "Preparing video" text, which stayed up forever on iOS.
		tb.waitJS(`!document.getElementById('controls').hidden`, 10*time.Second)
		time.Sleep(time.Second) // the converted stream is being fetched now
		var shown []string
		tb.eval(`['player-spinner', 'player-status'].map(id => document.getElementById(id))
			.filter(e => e && getComputedStyle(e).display !== 'none').map(e => e.id + ': ' + e.textContent)`, &shown)
		if len(shown) > 0 {
			t.Errorf("shown over the video before playing: %v", shown)
		}
		playUntil(tb, 2)
		assertClearWhilePlaying(t, tb)
		assertStillConverting(t, tb)
		// While playing, the top bar is the original's solid dark gray with
		// solid red links and icons (nothing see-through).
		var bar struct{ Bg, Link, Icon string }
		tb.eval(`(() => {
			const st = e => getComputedStyle(e);
			const icon = document.querySelector('#top-bar .label-short .icon');
			return {bg: st(document.getElementById('top-bar')).backgroundColor,
				link: st(document.querySelector('#top-bar .brand')).color,
				icon: st(icon).color + ' ' + st(icon).opacity};
		})()`, &bar)
		if bar.Bg != "rgb(25, 25, 25)" || bar.Link != "rgb(190, 30, 0)" || bar.Icon != "rgb(190, 30, 0) 1" {
			t.Errorf("top bar while playing: background %s, links %s, icons %s; want rgb(25, 25, 25) with rgb(190, 30, 0)",
				bar.Bg, bar.Link, bar.Icon)
		}
		if got := tb.text("#ctl-duration"); got != "00:30" {
			t.Errorf("length shown while converting = %s, want the whole 00:30", got)
		}
		var gap float64
		tb.eval(`document.querySelector('.meta-text .overview').getBoundingClientRect().top -
			document.querySelector('.meta-text h1').getBoundingClientRect().bottom`, &gap)
		if gap < 10 {
			t.Errorf("synopsis starts %.0fpx under the episode title, want some space", gap)
		}
		compareScreenshot(t, "playback-converting-phone-playing", tb.screenshotWith(playingStabilize))
		pauseAt(tb, 3)
		assertStillConverting(t, tb)
		// At 3 of 30 seconds the seek bar is a tenth full, not a share of
		// the part converted so far.
		var fill string
		tb.eval(`document.getElementById('ctl-seek').style.getPropertyValue('--fill')`, &fill)
		if fill != "10%" {
			t.Errorf("seek bar fill at 3s of 30s = %q, want 10%%", fill)
		}
		compareScreenshot(t, "playback-converting-phone-paused", tb.screenshotWith(pausedStabilize))
		tb.checkProblems()
	})
}

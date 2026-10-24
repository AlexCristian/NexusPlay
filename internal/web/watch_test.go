package web

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

// An episode's page: the server's length for the player, the episode title
// on its own line, and only the play button shown over the picture.
func TestWatchEpisodeMarkup(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	id, _, _ := e.store.InsertScannedMedia(ctx, store.ScannedMedia{Path: "Show/Show S01E04.mp4", Title: "E4", VideoCodec: "h264", Duration: 2672.3})
	coll, _ := e.store.CreateCollection(ctx, store.Collection{Name: "Star Trek: The Next Generation", Category: "TV Shows"})
	e.store.UpdateMedia(ctx, id, store.MediaUpdate{Title: "Who Watches the Watchers", CollectionID: coll, Season: 3, Episode: 4}, true)

	resp, page := e.client().do("GET", "/watch/"+itoa(id), nil, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status %d", resp.StatusCode)
	}
	for _, want := range []string{
		`data-duration="2672.3"`,
		`<h1>Star Trek: The Next Generation<span class="episode-title">Who Watches the Watchers</span></h1>`,
		`Season 3, episode 4`,
		`id="player-spinner" role="status" aria-label="Loading" hidden`,
		`id="player-status" class="player-status" role="alert" hidden`,
		`id="controls" hidden`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if strings.Contains(page, "Preparing") || strings.Contains(page, "Generation-") || strings.Contains(page, " - Who") {
		t.Error("page has the old status text or hyphenated title")
	}
}

// Static files are linked with a content hash, so an update is fetched at
// once; hashed URLs are cached for good, others for a day.
func TestStaticAssetsAreVersioned(t *testing.T) {
	e := newEnv(t, nil)
	c := e.client()
	_, page := c.do("GET", "/", nil, nil)
	m := regexp.MustCompile(`href="(/static/css/app\.css\?v=([0-9a-f]{10}))"`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("the stylesheet link has no content hash")
	}
	if m[2] != assetHashes["/static/css/app.css"] {
		t.Errorf("hash %s, want %s", m[2], assetHashes["/static/css/app.css"])
	}
	resp, _ := c.do("GET", m[1], nil, nil)
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("hashed URL Cache-Control = %q", cc)
	}
	resp, _ = c.do("GET", "/static/css/app.css?v=stale", nil, nil)
	if cc := resp.Header.Get("Cache-Control"); cc != "public, max-age=86400" {
		t.Errorf("unhashed URL Cache-Control = %q", cc)
	}
	for _, p := range []string{"/static/js/app.js", "/static/js/player.js", "/static/vendor/htmx.min.js", "/static/vendor/hls.light.min.js"} {
		if !strings.HasPrefix(asset(p), p+"?v=") {
			t.Errorf("asset(%s) = %s", p, asset(p))
		}
	}
}

// On phones the top bar's links are icons, so each has a name for screen
// readers and a tooltip.
func TestTopBarLinksHaveNames(t *testing.T) {
	e := newEnv(t, nil)
	admin := e.client()
	admin.register("alex")
	_, page := admin.do("GET", "/", nil, nil)
	for _, name := range []string{"Previously watching", "My subscriptions", "Control panel", "Log out"} {
		if !strings.Contains(page, `aria-label="`+name+`" title="`+name+`"`) {
			t.Errorf("top bar link %q has no aria-label and title", name)
		}
	}
}

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
	environment := newEnvironment(t, nil)
	ctx := context.Background()
	id, _, _ := environment.store.InsertScannedMedia(ctx, store.ScannedMedia{Path: "Show/Show S01E04.mp4", Title: "E4", VideoCodec: "h264", Duration: 2672.3})
	collectionID, _ := environment.store.CreateCollection(ctx, store.Collection{Name: "Star Trek: The Next Generation", Category: "TV Shows"})
	environment.store.UpdateMedia(ctx, id, store.MediaUpdate{Title: "Who Watches the Watchers", CollectionID: collectionID, Season: 3, Episode: 4}, true)

	response, page := environment.client().sendForm("GET", "/watch/"+formatInteger(id), nil, nil)
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status %d", response.StatusCode)
	}
	for _, want := range []string{
		`data-duration="2672.3"`,
		`<h1>Star Trek: The Next Generation<span class="episode-title">S3E4: Who Watches the Watchers</span></h1>`,
		`id="player-spinner" role="status" aria-label="Loading" hidden`,
		`id="player-status" class="player-status" role="alert" hidden`,
		`id="controls" hidden`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if strings.Contains(page, "Preparing") || strings.Contains(page, "Generation-") || strings.Contains(page, " - Who") || strings.Contains(page, "Season 3, episode 4") {
		t.Error("page has the old status text or hyphenated title")
	}
}

// Static files are linked with a content hash, so an update is fetched at
// once; hashed URLs are cached for good, others for a day.
func TestStaticAssetsAreVersioned(t *testing.T) {
	environment := newEnvironment(t, nil)
	client := environment.client()
	_, page := client.sendForm("GET", "/", nil, nil)
	match := regexp.MustCompile(`href="(/static/css/app\.css\?v=([0-9a-f]{10}))"`).FindStringSubmatch(page)
	if match == nil {
		t.Fatal("the stylesheet link has no content hash")
	}
	if match[2] != assetHashes["/static/css/app.css"] {
		t.Errorf("hash %s, want %s", match[2], assetHashes["/static/css/app.css"])
	}
	response, _ := client.sendForm("GET", match[1], nil, nil)
	if cacheControl := response.Header.Get("Cache-Control"); !strings.Contains(cacheControl, "immutable") {
		t.Errorf("hashed URL Cache-Control = %q", cacheControl)
	}
	response, _ = client.sendForm("GET", "/static/css/app.css?v=stale", nil, nil)
	if cacheControl := response.Header.Get("Cache-Control"); cacheControl != "public, max-age=86400" {
		t.Errorf("unhashed URL Cache-Control = %q", cacheControl)
	}
	for _, path := range []string{"/static/js/app.js", "/static/js/player.js", "/static/vendor/htmx.min.js", "/static/vendor/hls.light.min.js"} {
		if !strings.HasPrefix(asset(path), path+"?v=") {
			t.Errorf("asset(%s) = %s", path, asset(path))
		}
	}
}

// On phones the top bar's links are icons, so each has a name for screen
// readers and a tooltip.
func TestTopBarLinksHaveNames(t *testing.T) {
	environment := newEnvironment(t, nil)
	admin := environment.client()
	admin.register("alex")
	_, page := admin.sendForm("GET", "/", nil, nil)
	for _, name := range []string{"Previously watching", "My subscriptions", "Control panel", "Log out"} {
		if !strings.Contains(page, `aria-label="`+name+`" title="`+name+`"`) {
			t.Errorf("top bar link %q has no aria-label and title", name)
		}
	}
}

func TestEpisodeNumbers(t *testing.T) {
	for _, testCase := range []struct {
		season, episode, end int
		want                 string
	}{
		{1, 1, 2, "S1E1–2"},
		{1, 3, 0, "S1E3"},
		{3, 4, 0, "S3E4"},
		{0, 3, 0, "Special 3"},
		{0, 1, 2, "Special 1–2"},
	} {
		media := store.Media{Season: testCase.season, Episode: testCase.episode, EpisodeEnd: testCase.end}
		if got := episodeLabel(media); got != testCase.want {
			t.Errorf("episodeLabel(S%dE%d, end %d) = %q, want %q", testCase.season, testCase.episode, testCase.end, got, testCase.want)
		}
	}
	media := store.Media{Title: "Encounter at Farpoint", CollectionName: "TNG", Season: 1, Episode: 1, EpisodeEnd: 2}
	if got := displayTitle(media); got != "TNG · S1E1–2 · Encounter at Farpoint" {
		t.Errorf("displayTitle = %q", got)
	}
}

func TestFormatting(t *testing.T) {
	for _, testCase := range []struct {
		got, want string
	}{
		{duration(8), "8 s"}, {duration(2532), "42 min"}, {duration(3600), "1 h"}, {duration(3900), "1 h 5 min"},
		{clock(5), "0:05"}, {clock(2530), "42:10"}, {clock(3723), "1:02:03"},
		{percent(5, 8), "62.5%"}, {percent(9, 8), "100.0%"}, {percent(1, 0), "0%"},
		{fileSize(840 << 10), "840 KB"}, {fileSize(1258291), "1.2 MB"}, {fileSize(4692251771), "4.37 GB"},
		{blankZero(0), ""}, {blankZero(7), "7"},
		{subscriptionPhrase(1), "your 1 subscription"}, {subscriptionPhrase(3), "your 3 subscriptions"},
		{sentence("you have already voted on this line"), "You have already voted on this line."},
		{seasonSummary(store.Collection{Year: "1987"}, []Season{{Number: 0}, {Number: 1}, {Number: 2}}), "2 Seasons, 1987"},
		{seasonSummary(store.Collection{}, []Season{{Number: 0}}), "Specials"},
	} {
		if testCase.got != testCase.want {
			t.Errorf("got %q, want %q", testCase.got, testCase.want)
		}
	}
}

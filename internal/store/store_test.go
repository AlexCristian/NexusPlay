package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	store, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func mustUser(t *testing.T, store *Store, email, nick string) User {
	t.Helper()
	user, err := store.CreateUser(context.Background(), email, nick, "hash", 0)
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func mustMedia(t *testing.T, store *Store, path string) int64 {
	t.Helper()
	id, _, err := store.InsertScannedMedia(context.Background(), ScannedMedia{Path: path, Title: path, VideoCodec: "h264"})
	if err != nil || id == 0 {
		t.Fatalf("insert %s: id=%d err=%v", path, id, err)
	}
	return id
}

func TestMigrationsAreIdempotent(t *testing.T) {
	directory := t.TempDir()
	for i := 0; i < 2; i++ {
		store, err := Open(context.Background(), filepath.Join(directory, "x.db"))
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		store.Close()
	}
}

func TestFirstUserIsAdmin(t *testing.T) {
	store := newStore(t)
	alice := mustUser(t, store, "a@example.com", "alice")
	bob := mustUser(t, store, "b@example.com", "bob")
	if !alice.IsAdmin || bob.IsAdmin {
		t.Errorf("admin flags: first=%v second=%v, want true/false", alice.IsAdmin, bob.IsAdmin)
	}
	if _, err := store.CreateUser(context.Background(), "A@EXAMPLE.COM", "other", "h", 0); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate email (other case): got %v, want ErrDuplicate", err)
	}
	if _, err := store.CreateUser(context.Background(), "c@example.com", "BOB", "h", 0); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate nickname: got %v, want ErrDuplicate", err)
	}
}

func TestScannedMediaIsUniqueByPath(t *testing.T) {
	store := newStore(t)
	mustMedia(t, store, "a.mp4")
	if _, inserted, err := store.InsertScannedMedia(context.Background(), ScannedMedia{Path: "a.mp4", Title: "again"}); err != nil || inserted {
		t.Errorf("second insert: inserted=%v err=%v, want false and nil", inserted, err)
	}
}

func TestPublishNotifiesSubscribers(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	admin := mustUser(t, store, "a@example.com", "admin")
	fan := mustUser(t, store, "f@example.com", "fan")
	collectionID, err := store.CreateCollection(ctx, Collection{Name: "Cosmos", Category: "TV Shows"})
	if err != nil {
		t.Fatal(err)
	}
	if subscribed, err := store.ToggleSubscription(ctx, fan.ID, collectionID); err != nil || !subscribed {
		t.Fatalf("subscribe: on=%v err=%v", subscribed, err)
	}
	episodeID := mustMedia(t, store, "Cosmos/S01E01.mp4")
	if err := store.UpdateMedia(ctx, episodeID, MediaUpdate{Title: "Pilot", CollectionID: collectionID, Season: 1, Episode: 1}, true); err != nil {
		t.Fatal(err)
	}
	notes, err := store.Notifications(ctx, fan.ID, 10)
	if err != nil || len(notes) != 1 || notes[0].ID != episodeID {
		t.Fatalf("fan notifications = %+v, %v", notes, err)
	}
	if notes[0].CollectionName != "Cosmos" {
		t.Errorf("collection name = %q", notes[0].CollectionName)
	}
	if notifications, _ := store.Notifications(ctx, admin.ID, 10); len(notifications) != 0 {
		t.Errorf("unsubscribed user got %d notifications", len(notifications))
	}
	// Saving again must not notify twice.
	if err := store.UpdateMedia(ctx, episodeID, MediaUpdate{Title: "Pilot", CollectionID: collectionID, Season: 1, Episode: 1}, true); err != nil {
		t.Fatal(err)
	}
	if err := store.ClearNotification(ctx, fan.ID, episodeID); err != nil {
		t.Fatal(err)
	}
	if notifications, _ := store.Notifications(ctx, fan.ID, 10); len(notifications) != 0 {
		t.Errorf("after clearing: %d notifications", len(notifications))
	}
}

func TestCategoriesGroupCollections(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	user := mustUser(t, store, "a@example.com", "a")
	collectionID, _ := store.CreateCollection(ctx, Collection{Name: "Doctor Who", Category: "TV Shows"})
	movie := mustMedia(t, store, "m.mp4")
	firstEpisodeID := mustMedia(t, store, "dw1.mp4")
	secondEpisodeID := mustMedia(t, store, "dw2.mp4")
	hidden := mustMedia(t, store, "unpublished.mp4")
	store.UpdateMedia(ctx, movie, MediaUpdate{Title: "Alien", Category: "Movies"}, true)
	store.UpdateMedia(ctx, firstEpisodeID, MediaUpdate{Title: "Rose", CollectionID: collectionID, Season: 1, Episode: 1}, true)
	store.UpdateMedia(ctx, secondEpisodeID, MediaUpdate{Title: "The End of the World", CollectionID: collectionID, Season: 1, Episode: 2}, true)
	_ = hidden
	store.SaveProgress(ctx, user.ID, movie, 100, true)

	categories, err := store.Categories(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(categories) != 2 || categories[0].Name != "Movies" || categories[1].Name != "TV Shows" {
		t.Fatalf("categories = %+v", categories)
	}
	if len(categories[0].Tiles) != 1 || !categories[0].Tiles[0].Watched {
		t.Errorf("movies = %+v, want one watched tile", categories[0].Tiles)
	}
	if len(categories[1].Tiles) != 1 || !categories[1].Tiles[0].IsCollection {
		t.Errorf("tv = %+v, want one collection tile", categories[1].Tiles)
	}
	recent, err := store.RecentTiles(ctx, user.ID, 10)
	if err != nil || len(recent) != 2 {
		t.Errorf("recent = %+v, %v; want the movie and one collection tile", recent, err)
	}
	found, err := store.Search(ctx, user.ID, "doctor", 10)
	if err != nil || len(found) != 1 || found[0].Name != "TV Shows" || len(found[0].Tiles) != 1 || found[0].Tiles[0].Title != "Doctor Who" {
		t.Errorf("search = %+v, %v", found, err)
	}
	if found, _ := store.Search(ctx, user.ID, "unpublished", 10); len(found) != 0 {
		t.Errorf("search found unpublished media: %+v", found)
	}
	if found, _ := store.Search(ctx, user.ID, "%", 10); len(found) != 0 {
		t.Errorf("LIKE wildcard was not escaped: %+v", found)
	}
}

func TestProgress(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	user := mustUser(t, store, "a@example.com", "a")
	mediaID := mustMedia(t, store, "m.mp4")
	store.UpdateMedia(ctx, mediaID, MediaUpdate{Title: "M"}, true)
	if err := store.SaveProgress(ctx, user.ID, mediaID, 42.5, false); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveProgress(ctx, user.ID, mediaID, 50, false); err != nil {
		t.Fatal(err)
	}
	if position, _ := store.Progress(ctx, user.ID, mediaID); position != 50 {
		t.Errorf("position = %v, want 50", position)
	}
	if list, _ := store.InProgress(ctx, user.ID, 10); len(list) != 1 || list[0].Position != 50 {
		t.Errorf("in progress = %+v", list)
	}
	if err := store.SaveProgress(ctx, user.ID, mediaID, 99, true); err != nil {
		t.Fatal(err)
	}
	if position, _ := store.Progress(ctx, user.ID, mediaID); position != 0 {
		t.Errorf("after finishing, position = %v, want 0", position)
	}
	if watched, _ := store.IsWatched(ctx, user.ID, mediaID); !watched {
		t.Error("not marked watched after finishing")
	}
}

func TestSubtitleVoting(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var users []User
	for _, nickname := range []string{"a", "b", "c", "d"} {
		users = append(users, mustUser(t, store, nickname+"@example.com", nickname+"user"))
	}
	mediaID := mustMedia(t, store, "m.mp4")
	subtitleID, _, err := store.AddSubtitle(ctx, NewSubtitle{MediaID: mediaID, Language: "en", Label: "English", Cues: []subtitle.Cue{
		{StartMS: 1000, EndMS: 2000, Text: "Helo"},
		{StartMS: 3000, EndMS: 4000, Text: "World"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cue, err := store.CueAt(ctx, subtitleID, 1500)
	if err != nil || cue.Text != "Helo" {
		t.Fatalf("CueAt = %+v, %v", cue, err)
	}
	const required = 3

	if _, err := store.ProposeChange(ctx, cue.ID, users[0].ID, "Helo", required); !errors.Is(err, ErrUnchanged) {
		t.Errorf("unchanged proposal: got %v", err)
	}
	applied, err := store.ProposeChange(ctx, cue.ID, users[0].ID, "Hello", required)
	if err != nil || applied {
		t.Fatalf("propose: applied=%v err=%v", applied, err)
	}
	if _, err := store.ProposeChange(ctx, cue.ID, users[1].ID, "Hallo", required); err != nil {
		t.Fatal(err)
	}
	changes, _ := store.Changes(ctx, cue.ID, users[0].ID)
	if len(changes) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	var hello Change
	for _, change := range changes {
		if change.ProposedText == "Hello" {
			hello = change
		}
	}
	if !hello.VotedByMe || hello.Votes != 1 {
		t.Errorf("proposer's own vote not counted: %+v", hello)
	}
	if _, err := store.VoteChange(ctx, hello.ID, users[0].ID, required); !errors.Is(err, ErrAlreadyVoted) {
		t.Errorf("double vote: got %v", err)
	}
	if _, err := store.VoteChange(ctx, hello.ID, users[1].ID, required); !errors.Is(err, ErrAlreadyVoted) {
		t.Errorf("vote after proposing a rival on the same line: got %v", err)
	}
	if applied, err := store.VoteChange(ctx, hello.ID, users[2].ID, required); err != nil || applied {
		t.Fatalf("second vote: applied=%v err=%v", applied, err)
	}
	applied, err = store.VoteChange(ctx, hello.ID, users[3].ID, required)
	if err != nil || !applied {
		t.Fatalf("third vote: applied=%v err=%v", applied, err)
	}
	cues, _ := store.Cues(ctx, subtitleID)
	if cues[0].Text != "Hello" || cues[1].Text != "World" {
		t.Errorf("cues after applying = %+v", cues)
	}
	if left, _ := store.Changes(ctx, cue.ID, 0); len(left) != 0 {
		t.Errorf("competing proposals were not discarded: %+v", left)
	}
}

func TestSidecarImportIsIdempotent(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	mediaID := mustMedia(t, store, "m.mp4")
	cues := []subtitle.Cue{{StartMS: 0, EndMS: 1, Text: "x"}}
	sidecar := NewSubtitle{MediaID: mediaID, Language: "en", Label: "English", Source: "m.en.srt", Cues: cues}
	id1, inserted, err := store.AddSubtitle(ctx, sidecar)
	if err != nil || id1 == 0 || !inserted {
		t.Fatalf("first import: id=%d inserted=%v err=%v", id1, inserted, err)
	}
	if _, inserted, err := store.AddSubtitle(ctx, sidecar); err != nil || inserted {
		t.Errorf("second import: inserted=%v err=%v, want false and nil", inserted, err)
	}
	if subtitles, _ := store.Subtitles(ctx, mediaID); len(subtitles) != 1 {
		t.Errorf("%d tracks after importing the same sidecar twice, want 1", len(subtitles))
	}
	srcs, _ := store.SubtitleSources(ctx)
	if !srcs.Contains("m.en.srt") {
		t.Error("source not recorded")
	}
}

func TestDeletingMediaCascades(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	user := mustUser(t, store, "a@example.com", "a")
	mediaID := mustMedia(t, store, "m.mp4")
	store.AddSubtitle(ctx, NewSubtitle{MediaID: mediaID, Language: "en", Label: "English", Cues: []subtitle.Cue{{StartMS: 0, EndMS: 1, Text: "x"}}})
	store.SaveProgress(ctx, user.ID, mediaID, 5, false)
	if err := store.DeleteMedia(ctx, mediaID); err != nil {
		t.Fatal(err)
	}
	if subtitles, _ := store.Subtitles(ctx, mediaID); len(subtitles) != 0 {
		t.Error("subtitles survived media deletion")
	}
	if list, _ := store.InProgress(ctx, user.ID, 10); len(list) != 0 {
		t.Error("progress survived media deletion")
	}
}

// "movies" and "Movies" are one home-page tab, whichever spelling came first.
func TestCategoriesIgnoreLetterCase(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	for i, cat := range []string{"Movies", "movies", " MOVIES ", "", "Documentaries"} {
		id := mustMedia(t, store, fmt.Sprintf("m%d.mp4", i))
		if err := store.UpdateMedia(ctx, id, MediaUpdate{Title: fmt.Sprintf("Film %d", i), Category: cat}, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := store.CreateCollection(ctx, Collection{Name: "Cosmos", Category: "documentaries"}); err != nil {
		t.Fatal(err)
	}
	if names, _ := store.CategoryNames(ctx); strings.Join(names, ",") != "Documentaries,Movies" {
		t.Errorf("stored categories = %v, want one spelling each", names)
	}
	categories, err := store.Categories(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, category := range categories {
		got = append(got, fmt.Sprintf("%s:%d", category.Name, len(category.Tiles)))
	}
	if want := "Documentaries:1,Movies:3,Other:1"; strings.Join(got, ",") != want {
		t.Errorf("categories = %v, want %s", got, want)
	}
}

// An empty poster keeps the current one; RemovePoster clears it.
func TestUpdateMediaRemovesPoster(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	id := mustMedia(t, store, "m.mp4")
	poster := func() string {
		t.Helper()
		media, err := store.MediaByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return media.Poster
	}
	for _, step := range []struct {
		mediaUpdate MediaUpdate
		want        string
	}{
		{MediaUpdate{Title: "M", Poster: "a.jpg"}, "a.jpg"},
		{MediaUpdate{Title: "M"}, "a.jpg"},
		{MediaUpdate{Title: "M", RemovePoster: true}, ""},
		{MediaUpdate{Title: "M", Poster: "b.jpg"}, "b.jpg"},
	} {
		if err := store.UpdateMedia(ctx, id, step.mediaUpdate, false); err != nil {
			t.Fatal(err)
		}
		if got := poster(); got != step.want {
			t.Errorf("after %+v: poster = %q, want %q", step.mediaUpdate, got, step.want)
		}
	}
}

func TestUpdateCollectionRemovesPoster(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	id, err := store.CreateCollection(ctx, Collection{Name: "Show", Category: "TV Shows", Poster: "a.jpg"})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		collectionUpdate CollectionUpdate
		want             string
	}{
		{CollectionUpdate{Name: "Show", Category: "TV Shows"}, "a.jpg"},
		{CollectionUpdate{Name: "Show", Category: "TV Shows", RemovePoster: true}, ""},
		{CollectionUpdate{Name: "Show", Category: "TV Shows", Poster: "b.jpg"}, "b.jpg"},
	} {
		if err := store.UpdateCollection(ctx, id, step.collectionUpdate); err != nil {
			t.Fatal(err)
		}
		collection, err := store.CollectionByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if collection.Poster != step.want {
			t.Errorf("after %+v: poster = %q, want %q", step.collectionUpdate, collection.Poster, step.want)
		}
	}
}

// Recent tiles are newest first, one per collection (placed by its newest
// episode), and a show with many new episodes does not crowd out the rest.
func TestRecentTiles(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	user := mustUser(t, store, "a@example.com", "a")
	clock := time.Unix(1_700_000_000, 0)
	store.now = func() time.Time { clock = clock.Add(time.Minute); return clock }
	publish := func(path string, mediaUpdate MediaUpdate) int64 {
		t.Helper()
		id := mustMedia(t, store, path)
		if err := store.UpdateMedia(ctx, id, mediaUpdate, true); err != nil {
			t.Fatal(err)
		}
		return id
	}
	show, _ := store.CreateCollection(ctx, Collection{Name: "Cosmos", Category: "TV Shows"})
	old := publish("old.mp4", MediaUpdate{Title: "Old Film"})
	publish("e1.mp4", MediaUpdate{Title: "E1", CollectionID: show, Episode: 1})
	film := publish("film.mp4", MediaUpdate{Title: "Film"})
	for i := 2; i <= 30; i++ {
		publish(fmt.Sprintf("e%d.mp4", i), MediaUpdate{Title: fmt.Sprintf("E%d", i), CollectionID: show, Episode: i})
	}
	mustMedia(t, store, "unpublished.mp4")
	store.SaveProgress(ctx, user.ID, film, 1, true)

	got, err := store.RecentTiles(ctx, user.ID, 10)
	if err != nil {
		t.Fatal(err)
	}
	want := []Tile{
		{IsCollection: true, ID: show, Title: "Cosmos"},
		{ID: film, Title: "Film", Watched: true},
		{ID: old, Title: "Old Film"},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("recent tiles:\n got %+v\nwant %+v", got, want)
	}
	// The 29 newest rows are all episodes; three tiles still come back.
	if got, _ := store.RecentTiles(ctx, user.ID, 3); len(got) != 3 {
		t.Errorf("limit 3 gave %d tiles: %+v", len(got), got)
	}
	if got, _ := store.RecentTiles(ctx, user.ID, 2); len(got) != 2 {
		t.Errorf("limit 2 gave %d tiles", len(got))
	}
}

// An episode without a poster shows its collection's, and says so.
func TestMediaPosterFallsBackToCollection(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	show, _ := store.CreateCollection(ctx, Collection{Name: "Cosmos", Category: "TV Shows", Poster: "show.jpg"})
	episodeID := mustMedia(t, store, "e1.mp4")
	if err := store.UpdateMedia(ctx, episodeID, MediaUpdate{Title: "E1", CollectionID: show}, false); err != nil {
		t.Fatal(err)
	}
	media, _ := store.MediaByID(ctx, episodeID)
	if media.Poster != "show.jpg" || media.OwnPoster {
		t.Errorf("poster = %q, own = %v; want the collection's", media.Poster, media.OwnPoster)
	}
	store.UpdateMedia(ctx, episodeID, MediaUpdate{Title: "E1", CollectionID: show, Poster: "ep.jpg"}, false)
	if media, _ = store.MediaByID(ctx, episodeID); media.Poster != "ep.jpg" || !media.OwnPoster {
		t.Errorf("poster = %q, own = %v; want its own", media.Poster, media.OwnPoster)
	}
}

func TestPosterNames(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	store.CreateCollection(ctx, Collection{Name: "Show", Category: "TV Shows", Poster: "show.jpg"})
	id := mustMedia(t, store, "m.mp4")
	store.UpdateMedia(ctx, id, MediaUpdate{Title: "M", Poster: "film.jpg"}, false)
	mustMedia(t, store, "none.mp4")
	got, err := store.PosterNames(ctx)
	if err != nil || got.Size() != 2 || !got.Contains("show.jpg") || !got.Contains("film.jpg") {
		t.Errorf("PosterNames = %v, %v", got, err)
	}
}

// The episode range is stored with the episode, can be edited, and the
// backfill fills it only while the number still matches the file name.
func TestEpisodeEnd(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	id, _, err := store.InsertScannedMedia(ctx, ScannedMedia{Path: "a.mkv", Title: "A", Season: 1, Episode: 1, EpisodeEnd: 2})
	if err != nil {
		t.Fatal(err)
	}
	if media, _ := store.MediaByID(ctx, id); media.EpisodeEnd != 2 {
		t.Errorf("EpisodeEnd after insert = %d", media.EpisodeEnd)
	}
	store.UpdateMedia(ctx, id, MediaUpdate{Title: "A", Season: 1, Episode: 5}, false)
	store.SetEpisodeEnd(ctx, id, 1, 2) // renumbered: the file name's range no longer applies
	if media, _ := store.MediaByID(ctx, id); media.EpisodeEnd != 0 || media.Episode != 5 {
		t.Errorf("after renumbering and backfill: episode %d, end %d; want 5 and 0", media.Episode, media.EpisodeEnd)
	}
	other := mustMedia(t, store, "b.mkv")
	store.UpdateMedia(ctx, other, MediaUpdate{Title: "B", Season: 1, Episode: 1}, false)
	store.SetEpisodeEnd(ctx, other, 1, 2)
	if media, _ := store.MediaByID(ctx, other); media.EpisodeEnd != 2 {
		t.Errorf("backfill: end %d, want 2", media.EpisodeEnd)
	}
}

func TestSubscriptionCount(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	user := mustUser(t, store, "a@example.com", "a")
	collectionID, _ := store.CreateCollection(ctx, Collection{Name: "Show", Category: "TV Shows"})
	if count, _ := store.SubscriptionCount(ctx, user.ID); count != 0 {
		t.Errorf("count = %d", count)
	}
	store.ToggleSubscription(ctx, user.ID, collectionID)
	if count, _ := store.SubscriptionCount(ctx, user.ID); count != 1 {
		t.Errorf("count after subscribing = %d", count)
	}
}

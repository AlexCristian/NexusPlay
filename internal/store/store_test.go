package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func mustUser(t *testing.T, s *Store, email, nick string) User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), email, nick, "hash")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func mustMedia(t *testing.T, s *Store, path string) int64 {
	t.Helper()
	id, err := s.InsertScannedMedia(context.Background(), ScannedMedia{Path: path, Title: path, VideoCodec: "h264"})
	if err != nil || id == 0 {
		t.Fatalf("insert %s: id=%d err=%v", path, id, err)
	}
	return id
}

func TestMigrationsAreIdempotent(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 2; i++ {
		s, err := Open(context.Background(), filepath.Join(dir, "x.db"))
		if err != nil {
			t.Fatalf("open %d: %v", i, err)
		}
		s.Close()
	}
}

func TestFirstUserIsAdmin(t *testing.T) {
	s := newStore(t)
	a := mustUser(t, s, "a@example.com", "alice")
	b := mustUser(t, s, "b@example.com", "bob")
	if !a.IsAdmin || b.IsAdmin {
		t.Errorf("admin flags: first=%v second=%v, want true/false", a.IsAdmin, b.IsAdmin)
	}
	if _, err := s.CreateUser(context.Background(), "A@EXAMPLE.COM", "other", "h"); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate email (other case): got %v, want ErrDuplicate", err)
	}
	if _, err := s.CreateUser(context.Background(), "c@example.com", "BOB", "h"); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate nickname: got %v, want ErrDuplicate", err)
	}
}

func TestScannedMediaIsUniqueByPath(t *testing.T) {
	s := newStore(t)
	mustMedia(t, s, "a.mp4")
	id, err := s.InsertScannedMedia(context.Background(), ScannedMedia{Path: "a.mp4", Title: "again"})
	if err != nil || id != 0 {
		t.Errorf("second insert: id=%d err=%v, want 0 and nil", id, err)
	}
}

func TestPublishNotifiesSubscribers(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	admin := mustUser(t, s, "a@example.com", "admin")
	fan := mustUser(t, s, "f@example.com", "fan")
	coll, err := s.CreateCollection(ctx, Collection{Name: "Cosmos", Category: "TV Shows"})
	if err != nil {
		t.Fatal(err)
	}
	if on, err := s.ToggleSubscription(ctx, fan.ID, coll); err != nil || !on {
		t.Fatalf("subscribe: on=%v err=%v", on, err)
	}
	ep := mustMedia(t, s, "Cosmos/S01E01.mp4")
	if err := s.UpdateMedia(ctx, ep, MediaUpdate{Title: "Pilot", CollectionID: coll, Season: 1, Episode: 1}, true); err != nil {
		t.Fatal(err)
	}
	notes, err := s.Notifications(ctx, fan.ID, 10)
	if err != nil || len(notes) != 1 || notes[0].ID != ep {
		t.Fatalf("fan notifications = %+v, %v", notes, err)
	}
	if notes[0].CollectionName != "Cosmos" {
		t.Errorf("collection name = %q", notes[0].CollectionName)
	}
	if n, _ := s.Notifications(ctx, admin.ID, 10); len(n) != 0 {
		t.Errorf("unsubscribed user got %d notifications", len(n))
	}
	// Saving again must not notify twice.
	if err := s.UpdateMedia(ctx, ep, MediaUpdate{Title: "Pilot", CollectionID: coll, Season: 1, Episode: 1}, true); err != nil {
		t.Fatal(err)
	}
	if err := s.ClearNotification(ctx, fan.ID, ep); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.Notifications(ctx, fan.ID, 10); len(n) != 0 {
		t.Errorf("after clearing: %d notifications", len(n))
	}
}

func TestCategoriesGroupCollections(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	u := mustUser(t, s, "a@example.com", "a")
	coll, _ := s.CreateCollection(ctx, Collection{Name: "Doctor Who", Category: "TV Shows"})
	movie := mustMedia(t, s, "m.mp4")
	e1 := mustMedia(t, s, "dw1.mp4")
	e2 := mustMedia(t, s, "dw2.mp4")
	hidden := mustMedia(t, s, "unpublished.mp4")
	s.UpdateMedia(ctx, movie, MediaUpdate{Title: "Alien", Category: "Movies"}, true)
	s.UpdateMedia(ctx, e1, MediaUpdate{Title: "Rose", CollectionID: coll, Season: 1, Episode: 1}, true)
	s.UpdateMedia(ctx, e2, MediaUpdate{Title: "The End of the World", CollectionID: coll, Season: 1, Episode: 2}, true)
	_ = hidden
	s.SaveProgress(ctx, u.ID, movie, 100, true)

	cats, err := s.Categories(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(cats) != 2 || cats[0].Name != "Movies" || cats[1].Name != "TV Shows" {
		t.Fatalf("categories = %+v", cats)
	}
	if len(cats[0].Tiles) != 1 || !cats[0].Tiles[0].Watched {
		t.Errorf("movies = %+v, want one watched tile", cats[0].Tiles)
	}
	if len(cats[1].Tiles) != 1 || !cats[1].Tiles[0].IsCollection {
		t.Errorf("tv = %+v, want one collection tile", cats[1].Tiles)
	}
	recent, err := s.RecentTiles(ctx, u.ID, 10)
	if err != nil || len(recent) != 2 {
		t.Errorf("recent = %+v, %v; want the movie and one collection tile", recent, err)
	}
	found, err := s.Search(ctx, u.ID, "doctor", 10)
	if err != nil || len(found) != 1 || found[0].Title != "Doctor Who" {
		t.Errorf("search = %+v, %v", found, err)
	}
	if found, _ := s.Search(ctx, u.ID, "unpublished", 10); len(found) != 0 {
		t.Errorf("search found unpublished media: %+v", found)
	}
	if found, _ := s.Search(ctx, u.ID, "%", 10); len(found) != 0 {
		t.Errorf("LIKE wildcard was not escaped: %+v", found)
	}
}

func TestProgress(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	u := mustUser(t, s, "a@example.com", "a")
	m := mustMedia(t, s, "m.mp4")
	s.UpdateMedia(ctx, m, MediaUpdate{Title: "M"}, true)
	if err := s.SaveProgress(ctx, u.ID, m, 42.5, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveProgress(ctx, u.ID, m, 50, false); err != nil {
		t.Fatal(err)
	}
	if pos, _ := s.Progress(ctx, u.ID, m); pos != 50 {
		t.Errorf("position = %v, want 50", pos)
	}
	if list, _ := s.InProgress(ctx, u.ID, 10); len(list) != 1 || list[0].Position != 50 {
		t.Errorf("in progress = %+v", list)
	}
	if err := s.SaveProgress(ctx, u.ID, m, 99, true); err != nil {
		t.Fatal(err)
	}
	if pos, _ := s.Progress(ctx, u.ID, m); pos != 0 {
		t.Errorf("after finishing, position = %v, want 0", pos)
	}
	if w, _ := s.IsWatched(ctx, u.ID, m); !w {
		t.Error("not marked watched after finishing")
	}
}

func TestSubtitleVoting(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	var users []User
	for _, n := range []string{"a", "b", "c", "d"} {
		users = append(users, mustUser(t, s, n+"@example.com", n+"user"))
	}
	m := mustMedia(t, s, "m.mp4")
	sub, err := s.AddSubtitle(ctx, m, "en", "English", "", []subtitle.Cue{
		{StartMS: 1000, EndMS: 2000, Text: "Helo"},
		{StartMS: 3000, EndMS: 4000, Text: "World"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cue, err := s.CueAt(ctx, sub, 1500)
	if err != nil || cue.Text != "Helo" {
		t.Fatalf("CueAt = %+v, %v", cue, err)
	}
	const required = 3

	if _, err := s.ProposeChange(ctx, cue.ID, users[0].ID, "Helo", required); !errors.Is(err, ErrUnchanged) {
		t.Errorf("unchanged proposal: got %v", err)
	}
	applied, err := s.ProposeChange(ctx, cue.ID, users[0].ID, "Hello", required)
	if err != nil || applied {
		t.Fatalf("propose: applied=%v err=%v", applied, err)
	}
	if _, err := s.ProposeChange(ctx, cue.ID, users[1].ID, "Hallo", required); err != nil {
		t.Fatal(err)
	}
	changes, _ := s.Changes(ctx, cue.ID, users[0].ID)
	if len(changes) != 2 {
		t.Fatalf("changes = %+v", changes)
	}
	var hello Change
	for _, c := range changes {
		if c.ProposedText == "Hello" {
			hello = c
		}
	}
	if !hello.VotedByMe || hello.Votes != 1 {
		t.Errorf("proposer's own vote not counted: %+v", hello)
	}
	if _, err := s.VoteChange(ctx, hello.ID, users[0].ID, required); !errors.Is(err, ErrAlreadyVoted) {
		t.Errorf("double vote: got %v", err)
	}
	if _, err := s.VoteChange(ctx, hello.ID, users[1].ID, required); !errors.Is(err, ErrAlreadyVoted) {
		t.Errorf("vote after proposing a rival on the same line: got %v", err)
	}
	if applied, err := s.VoteChange(ctx, hello.ID, users[2].ID, required); err != nil || applied {
		t.Fatalf("second vote: applied=%v err=%v", applied, err)
	}
	applied, err = s.VoteChange(ctx, hello.ID, users[3].ID, required)
	if err != nil || !applied {
		t.Fatalf("third vote: applied=%v err=%v", applied, err)
	}
	cues, _ := s.Cues(ctx, sub)
	if cues[0].Text != "Hello" || cues[1].Text != "World" {
		t.Errorf("cues after applying = %+v", cues)
	}
	if left, _ := s.Changes(ctx, cue.ID, 0); len(left) != 0 {
		t.Errorf("competing proposals were not discarded: %+v", left)
	}
}

func TestSidecarImportIsIdempotent(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	m := mustMedia(t, s, "m.mp4")
	cues := []subtitle.Cue{{StartMS: 0, EndMS: 1, Text: "x"}}
	id1, err := s.AddSubtitle(ctx, m, "en", "English", "m.en.srt", cues)
	if err != nil || id1 == 0 {
		t.Fatalf("first import: %d %v", id1, err)
	}
	id2, err := s.AddSubtitle(ctx, m, "en", "English", "m.en.srt", cues)
	if err != nil || id2 != 0 {
		t.Errorf("second import: id=%d err=%v, want 0 and nil", id2, err)
	}
	srcs, _ := s.SubtitleSources(ctx)
	if !srcs["m.en.srt"] {
		t.Error("source not recorded")
	}
}

func TestDeletingMediaCascades(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	u := mustUser(t, s, "a@example.com", "a")
	m := mustMedia(t, s, "m.mp4")
	s.AddSubtitle(ctx, m, "en", "English", "", []subtitle.Cue{{StartMS: 0, EndMS: 1, Text: "x"}})
	s.SaveProgress(ctx, u.ID, m, 5, false)
	if err := s.DeleteMedia(ctx, m); err != nil {
		t.Fatal(err)
	}
	if subs, _ := s.Subtitles(ctx, m); len(subs) != 0 {
		t.Error("subtitles survived media deletion")
	}
	if list, _ := s.InProgress(ctx, u.ID, 10); len(list) != 0 {
		t.Error("progress survived media deletion")
	}
}

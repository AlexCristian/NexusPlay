package library

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/metadata"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// fakeMeta is TMDB with one film and one show (TNG, whose pilot is one
// episode). fail makes every lookup fail as if the network were down.
type fakeMeta struct {
	mu      sync.Mutex
	fail    bool
	lookups int
}

func (f *fakeMeta) start() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lookups++
	if f.fail {
		return errors.New("connection reset")
	}
	return nil
}

func (f *fakeMeta) Movie(ctx context.Context, title, year string) (metadata.Info, error) {
	if err := f.start(); err != nil {
		return metadata.Info{}, err
	}
	if title != "Alien" {
		return metadata.Info{}, metadata.ErrNoMatch
	}
	return metadata.Info{Title: "Alien", Year: "1979", Overview: "In space.", Poster: "alien.jpg"}, nil
}

func (f *fakeMeta) Show(ctx context.Context, name, year string) (metadata.Info, error) {
	if err := f.start(); err != nil {
		return metadata.Info{}, err
	}
	return metadata.Info{ID: 655, Title: "Star Trek: The Next Generation", Year: "1987", Poster: "tng.jpg"}, nil
}

func (f *fakeMeta) Episode(ctx context.Context, showID, season, episode int) (metadata.Info, error) {
	for _, e := range tngS1 {
		if e.Number == episode {
			return e.Info, nil
		}
	}
	return metadata.Info{}, metadata.ErrNoMatch
}

func (f *fakeMeta) Season(ctx context.Context, showID, season int) ([]metadata.Episode, error) {
	return tngS1, nil
}

func newMetaScanner(t *testing.T, meta Metadata) (*Scanner, *store.Store, string) {
	t.Helper()
	sc, st, media := newUnitScanner(t, &fakeProber{})
	sc.deps.Meta = meta
	sc.deps.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	return sc, st, media
}

func mediaByPath(t *testing.T, st *store.Store, rel string) store.Media {
	t.Helper()
	paths, _ := st.MediaPaths(context.Background())
	m, err := st.MediaByID(context.Background(), paths[rel])
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return m
}

// A lookup that failed is retried by a later scan, once an hour has passed.
func TestFailedLookupIsRetried(t *testing.T) {
	meta := &fakeMeta{fail: true}
	sc, st, media := newMetaScanner(t, meta)
	clock := time.Now()
	sc.now = func() time.Time { return clock }
	ctx := context.Background()
	writeSettled(t, filepath.Join(media, "Movies", "Alien (1979).mp4"), "video")

	sc.Scan(ctx)
	if m := mediaByPath(t, st, "Movies/Alien (1979).mp4"); m.Poster != "" {
		t.Fatalf("poster after a failed lookup: %q", m.Poster)
	}
	meta.fail = false
	before := meta.lookups
	sc.Scan(ctx)
	if meta.lookups != before {
		t.Error("retried within the hour")
	}
	clock = clock.Add(2 * time.Hour)
	sc.Scan(ctx)
	if m := mediaByPath(t, st, "Movies/Alien (1979).mp4"); m.Poster != "alien.jpg" || m.Overview != "In space." {
		t.Errorf("after the retry: poster %q, overview %q", m.Poster, m.Overview)
	}
}

// Files scanned while lookups were off get their details once a key is set
// (the scanner gets a Metadata).
func TestLookupAfterKeyIsSet(t *testing.T) {
	sc, st, media := newUnitScanner(t, &fakeProber{})
	ctx := context.Background()
	writeSettled(t, filepath.Join(media, "Movies", "Alien (1979).mp4"), "video")
	sc.Scan(ctx)

	sc.deps.Meta = &fakeMeta{}
	sc.Scan(ctx)
	if m := mediaByPath(t, st, "Movies/Alien (1979).mp4"); m.Poster != "alien.jpg" {
		t.Errorf("poster = %q, want it looked up once lookups were on", m.Poster)
	}
}

// Episodes are matched by the title in the file name, so a two-part pilot
// in one file does not shift the rest of the season.
func TestEpisodeMatchedByTitle(t *testing.T) {
	sc, st, media := newMetaScanner(t, &fakeMeta{})
	ctx := context.Background()
	dir := filepath.Join(media, "Star Trek The Next Generation", "Season 1")
	writeSettled(t, filepath.Join(dir, "TNG S01E01E02 Encounter at Farpoint.mkv"), "video")
	writeSettled(t, filepath.Join(dir, "TNG S01E03 The Naked Now.mkv"), "video")
	writeSettled(t, filepath.Join(dir, "TNG S01E04.mkv"), "video") // no title: by number
	sc.Scan(ctx)

	for file, want := range map[string]string{
		"TNG S01E01E02 Encounter at Farpoint.mkv": "Encounter at Farpoint",
		"TNG S01E03 The Naked Now.mkv":            "The Naked Now",
		"TNG S01E04.mkv":                          "The Last Outpost",
	} {
		m := mediaByPath(t, st, "Star Trek The Next Generation/Season 1/"+file)
		if m.Title != want || m.Poster != "tng.jpg" {
			t.Errorf("%s: title %q, poster %q; want %q", file, m.Title, m.Poster, want)
		}
	}
}

// Refresh looks a published item up again, using its corrected episode.
func TestRefreshMetadata(t *testing.T) {
	sc, st, media := newMetaScanner(t, &fakeMeta{})
	ctx := context.Background()
	writeSettled(t, filepath.Join(media, "TNG", "Season 1", "TNG S01E09.mkv"), "video")
	sc.Scan(ctx)
	m := mediaByPath(t, st, "TNG/Season 1/TNG S01E09.mkv")
	if err := st.UpdateMedia(ctx, m.ID, store.MediaUpdate{Title: "Mine", Season: 1, Episode: 2}, true); err != nil {
		t.Fatal(err)
	}
	if err := sc.RefreshMetadata(ctx, m.ID); err != nil {
		t.Fatal(err)
	}
	if m, _ = st.MediaByID(ctx, m.ID); m.Title != "The Naked Now" || !m.Published {
		t.Errorf("after refresh: %q (published %v), want The Naked Now", m.Title, m.Published)
	}
	if err := sc.RefreshMetadata(ctx, 9999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown media: %v", err)
	}
}

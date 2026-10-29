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

// fakeMetadata is TMDB with one film and one show (TNG, whose pilot is one
// episode). fail makes every lookup fail as if the network were down.
type fakeMetadata struct {
	mutex   sync.Mutex
	fail    bool
	lookups int
}

func (fakeMetadata *fakeMetadata) start() error {
	fakeMetadata.mutex.Lock()
	defer fakeMetadata.mutex.Unlock()
	fakeMetadata.lookups++
	if fakeMetadata.fail {
		return errors.New("connection reset")
	}
	return nil
}

func (fakeMetadata *fakeMetadata) Movie(ctx context.Context, title, year string) (metadata.Info, error) {
	if err := fakeMetadata.start(); err != nil {
		return metadata.Info{}, err
	}
	if title != "Alien" {
		return metadata.Info{}, metadata.ErrNoMatch
	}
	return metadata.Info{Title: "Alien", Year: "1979", Overview: "In space.", Poster: "alien.jpg"}, nil
}

func (fakeMetadata *fakeMetadata) Show(ctx context.Context, name, year string) (metadata.Info, error) {
	if err := fakeMetadata.start(); err != nil {
		return metadata.Info{}, err
	}
	return metadata.Info{ID: 655, Title: "Star Trek: The Next Generation", Year: "1987", Poster: "tng.jpg"}, nil
}

func (fakeMetadata *fakeMetadata) Episode(ctx context.Context, showID, season, episode int) (metadata.Info, error) {
	for _, listed := range tngS1 {
		if listed.Number == episode {
			return listed.Info, nil
		}
	}
	return metadata.Info{}, metadata.ErrNoMatch
}

func (fakeMetadata *fakeMetadata) Season(ctx context.Context, showID, season int) ([]metadata.Episode, error) {
	return tngS1, nil
}

func newMetadataScanner(t *testing.T, lookups Metadata) (*Scanner, *store.Store, string) {
	t.Helper()
	scanner, store, media := newUnitScanner(t, &fakeProber{})
	scanner.dependencies.Metadata = lookups
	scanner.dependencies.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	return scanner, store, media
}

func mediaByPath(t *testing.T, store *store.Store, relativePath string) store.Media {
	t.Helper()
	paths, _ := store.MediaPaths(context.Background())
	media, err := store.MediaByID(context.Background(), paths[relativePath])
	if err != nil {
		t.Fatalf("%s: %v", relativePath, err)
	}
	return media
}

// A lookup that failed is retried by a later scan, once an hour has passed.
func TestFailedLookupIsRetried(t *testing.T) {
	lookups := &fakeMetadata{fail: true}
	scanner, store, media := newMetadataScanner(t, lookups)
	clock := time.Now()
	scanner.now = func() time.Time { return clock }
	ctx := context.Background()
	writeSettled(t, filepath.Join(media, "Movies", "Alien (1979).mp4"), "video")

	scanner.Scan(ctx)
	if media := mediaByPath(t, store, "Movies/Alien (1979).mp4"); media.Poster != "" {
		t.Fatalf("poster after a failed lookup: %q", media.Poster)
	}
	lookups.fail = false
	before := lookups.lookups
	scanner.Scan(ctx)
	if lookups.lookups != before {
		t.Error("retried within the hour")
	}
	clock = clock.Add(2 * time.Hour)
	scanner.Scan(ctx)
	if media := mediaByPath(t, store, "Movies/Alien (1979).mp4"); media.Poster != "alien.jpg" || media.Overview != "In space." {
		t.Errorf("after the retry: poster %q, overview %q", media.Poster, media.Overview)
	}
}

// Files scanned while lookups were off get their details once a key is set
// (the scanner gets a Metadata).
func TestLookupAfterKeyIsSet(t *testing.T) {
	scanner, store, media := newUnitScanner(t, &fakeProber{})
	ctx := context.Background()
	writeSettled(t, filepath.Join(media, "Movies", "Alien (1979).mp4"), "video")
	scanner.Scan(ctx)

	scanner.dependencies.Metadata = &fakeMetadata{}
	scanner.Scan(ctx)
	if media := mediaByPath(t, store, "Movies/Alien (1979).mp4"); media.Poster != "alien.jpg" {
		t.Errorf("poster = %q, want it looked up once lookups were on", media.Poster)
	}
}

// Episodes are matched by the title in the file name, so a two-part pilot
// in one file does not shift the rest of the season.
func TestEpisodeMatchedByTitle(t *testing.T) {
	scanner, store, media := newMetadataScanner(t, &fakeMetadata{})
	ctx := context.Background()
	directory := filepath.Join(media, "Star Trek The Next Generation", "Season 1")
	writeSettled(t, filepath.Join(directory, "TNG S01E01E02 Encounter at Farpoint.mkv"), "video")
	writeSettled(t, filepath.Join(directory, "TNG S01E03 The Naked Now.mkv"), "video")
	writeSettled(t, filepath.Join(directory, "TNG S01E04.mkv"), "video") // no title: by number
	scanner.Scan(ctx)

	for file, want := range map[string]string{
		"TNG S01E01E02 Encounter at Farpoint.mkv": "Encounter at Farpoint",
		"TNG S01E03 The Naked Now.mkv":            "The Naked Now",
		"TNG S01E04.mkv":                          "The Last Outpost",
	} {
		media := mediaByPath(t, store, "Star Trek The Next Generation/Season 1/"+file)
		if media.Title != want || media.Poster != "tng.jpg" {
			t.Errorf("%s: title %q, poster %q; want %q", file, media.Title, media.Poster, want)
		}
	}
}

// Refresh looks a published item up again, using its corrected episode.
func TestRefreshMetadata(t *testing.T) {
	scanner, database, media := newMetadataScanner(t, &fakeMetadata{})
	ctx := context.Background()
	writeSettled(t, filepath.Join(media, "TNG", "Season 1", "TNG S01E09.mkv"), "video")
	scanner.Scan(ctx)
	mediaItem := mediaByPath(t, database, "TNG/Season 1/TNG S01E09.mkv")
	if err := database.UpdateMedia(ctx, mediaItem.ID, store.MediaUpdate{Title: "Mine", Season: 1, Episode: 2}, true); err != nil {
		t.Fatal(err)
	}
	if err := scanner.RefreshMetadata(ctx, mediaItem.ID); err != nil {
		t.Fatal(err)
	}
	if mediaItem, _ = database.MediaByID(ctx, mediaItem.ID); mediaItem.Title != "The Naked Now" || !mediaItem.Published {
		t.Errorf("after refresh: %q (published %v), want The Naked Now", mediaItem.Title, mediaItem.Published)
	}
	if err := scanner.RefreshMetadata(ctx, 9999); !errors.Is(err, store.ErrNotFound) {
		t.Errorf("unknown media: %v", err)
	}
}

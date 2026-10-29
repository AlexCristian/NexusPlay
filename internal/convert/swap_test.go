package convert

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/AlexCristian/NexusPlay/internal/store"
)

func newSwapFixture(t *testing.T, sourceName string) (*Converter, store.Media, string) {
	t.Helper()
	ctx := context.Background()
	directory := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(directory, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	media := filepath.Join(directory, "media")
	if err := os.MkdirAll(media, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(media, sourceName), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, _, err := database.InsertScannedMedia(ctx, store.ScannedMedia{Path: sourceName, Title: "Movie", VideoCodec: "mpeg4"})
	if err != nil || id == 0 {
		t.Fatalf("insert: %d %v", id, err)
	}
	mediaItem, err := database.MediaByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	converter := New(Dependencies{Store: database, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, Config{MediaDirectory: media})
	return converter, mediaItem, media
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		return "<missing: " + err.Error() + ">"
	}
	return string(content)
}

// A file that appeared at the target name while the conversion ran (for
// example the user's own Movie.mp4) must never be overwritten or deleted.
func TestSwapNeverOverwritesAnExistingTarget(t *testing.T) {
	converter, mediaItem, media := newSwapFixture(t, "Movie.mkv")
	source := filepath.Join(media, "Movie.mkv")
	target := filepath.Join(media, "Movie.mp4")
	temporaryDirectory := filepath.Join(media, ".Movie"+temporarySuffix)
	os.WriteFile(temporaryDirectory, []byte("converted"), 0o644)
	os.WriteFile(target, []byte("the user's own file"), 0o644) // appeared mid-conversion

	err := converter.swap(context.Background(), mediaItem, source, temporaryDirectory, target, store.ReplacedFile{Path: "Movie.mp4", VideoCodec: "h264"})
	if !errors.Is(err, ErrTargetExists) {
		t.Fatalf("swap error = %v, want ErrTargetExists", err)
	}
	if got := readFile(t, target); got != "the user's own file" {
		t.Errorf("target = %q, the user's file was replaced", got)
	}
	if got := readFile(t, source); got != "original" {
		t.Errorf("original = %q, want it untouched", got)
	}
	if current, _ := converter.dependencies.Store.MediaByID(context.Background(), mediaItem.ID); current.Path != "Movie.mkv" {
		t.Errorf("row now points at %q, want the original", current.Path)
	}
}

// A database failure after the converted file is in place removes only the
// file this conversion created, and leaves the original alone.
func TestSwapDatabaseFailureRemovesOnlyItsOwnFile(t *testing.T) {
	converter, mediaItem, media := newSwapFixture(t, "Movie.mkv")
	source := filepath.Join(media, "Movie.mkv")
	target := filepath.Join(media, "Movie.mp4")
	temporaryDirectory := filepath.Join(media, ".Movie"+temporarySuffix)
	os.WriteFile(temporaryDirectory, []byte("converted"), 0o644)
	// Another row already uses the target path, so the update hits the unique index.
	if _, _, err := converter.dependencies.Store.(*store.Store).InsertScannedMedia(context.Background(), store.ScannedMedia{Path: "Movie.mp4", Title: "x"}); err != nil {
		t.Fatal(err)
	}

	err := converter.swap(context.Background(), mediaItem, source, temporaryDirectory, target, store.ReplacedFile{Path: "Movie.mp4", VideoCodec: "h264"})
	if err == nil {
		t.Fatal("swap succeeded, want a database error")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Errorf("the converted file this job created should be removed, stat err = %v", statErr)
	}
	if got := readFile(t, source); got != "original" {
		t.Errorf("original = %q, want it untouched", got)
	}
}

func TestSwapReplacesAndDeletesOriginal(t *testing.T) {
	converter, mediaItem, media := newSwapFixture(t, "Movie.mkv")
	source := filepath.Join(media, "Movie.mkv")
	target := filepath.Join(media, "Movie.mp4")
	temporaryDirectory := filepath.Join(media, ".Movie"+temporarySuffix)
	os.WriteFile(temporaryDirectory, []byte("converted"), 0o644)

	if err := converter.swap(context.Background(), mediaItem, source, temporaryDirectory, target, store.ReplacedFile{Path: "Movie.mp4", VideoCodec: "h264"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, target); got != "converted" {
		t.Errorf("target = %q", got)
	}
	for _, gone := range []string{source, temporaryDirectory} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s should be gone, stat err = %v", filepath.Base(gone), err)
		}
	}
	if current, _ := converter.dependencies.Store.MediaByID(context.Background(), mediaItem.ID); current.Path != "Movie.mp4" || current.VideoCodec != "h264" {
		t.Errorf("row = %+v", current)
	}
}

// A same-name MP4 (wrong codecs inside) is replaced in one atomic rename.
func TestSwapSameNameReplacesInPlace(t *testing.T) {
	converter, mediaItem, media := newSwapFixture(t, "Movie.mp4")
	path := filepath.Join(media, "Movie.mp4")
	temporaryDirectory := filepath.Join(media, ".Movie"+temporarySuffix)
	os.WriteFile(temporaryDirectory, []byte("converted"), 0o644)

	if err := converter.swap(context.Background(), mediaItem, path, temporaryDirectory, path, store.ReplacedFile{Path: "Movie.mp4", VideoCodec: "h264"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "converted" {
		t.Errorf("file = %q", got)
	}
}

// When the row moved on while converting (a rescan replaced it), the swap
// stops without touching any file, and says why.
func TestSwapRefusesWhenTheRowChanged(t *testing.T) {
	converter, mediaItem, media := newSwapFixture(t, "Movie.mkv")
	source := filepath.Join(media, "Movie.mkv")
	target := filepath.Join(media, "Movie.mp4")
	temporaryDirectory := filepath.Join(media, ".Movie"+temporarySuffix)
	os.WriteFile(temporaryDirectory, []byte("converted"), 0o644)
	mediaItem.Path = "Something else.mkv" // the job's snapshot no longer matches the row

	err := converter.swap(context.Background(), mediaItem, source, temporaryDirectory, target, store.ReplacedFile{Path: "Movie.mp4"})
	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("swap error = %v, want ErrSourceChanged", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Error("no file should be placed when the row changed")
	}
}

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

func newSwapFixture(t *testing.T, srcName string) (*Converter, store.Media, string) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	media := filepath.Join(dir, "media")
	if err := os.MkdirAll(media, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(media, srcName), []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	id, _, err := st.InsertScannedMedia(ctx, store.ScannedMedia{Path: srcName, Title: "Movie", VideoCodec: "mpeg4"})
	if err != nil || id == 0 {
		t.Fatalf("insert: %d %v", id, err)
	}
	m, err := st.MediaByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	c := New(Deps{Store: st, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, Config{MediaDir: media})
	return c, m, media
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return "<missing: " + err.Error() + ">"
	}
	return string(b)
}

// A file that appeared at the target name while the conversion ran (for
// example the user's own Movie.mp4) must never be overwritten or deleted.
func TestSwapNeverOverwritesAnExistingTarget(t *testing.T) {
	c, m, media := newSwapFixture(t, "Movie.mkv")
	src := filepath.Join(media, "Movie.mkv")
	target := filepath.Join(media, "Movie.mp4")
	tmp := filepath.Join(media, ".Movie"+tmpSuffix)
	os.WriteFile(tmp, []byte("converted"), 0o644)
	os.WriteFile(target, []byte("the user's own file"), 0o644) // appeared mid-conversion

	err := c.swap(context.Background(), m, src, tmp, target, store.ReplacedFile{Path: "Movie.mp4", VideoCodec: "h264"})
	if !errors.Is(err, ErrTargetExists) {
		t.Fatalf("swap error = %v, want ErrTargetExists", err)
	}
	if got := readFile(t, target); got != "the user's own file" {
		t.Errorf("target = %q, the user's file was replaced", got)
	}
	if got := readFile(t, src); got != "original" {
		t.Errorf("original = %q, want it untouched", got)
	}
	if cur, _ := c.deps.Store.MediaByID(context.Background(), m.ID); cur.Path != "Movie.mkv" {
		t.Errorf("row now points at %q, want the original", cur.Path)
	}
}

// A database failure after the converted file is in place removes only the
// file this conversion created, and leaves the original alone.
func TestSwapDatabaseFailureRemovesOnlyItsOwnFile(t *testing.T) {
	c, m, media := newSwapFixture(t, "Movie.mkv")
	src := filepath.Join(media, "Movie.mkv")
	target := filepath.Join(media, "Movie.mp4")
	tmp := filepath.Join(media, ".Movie"+tmpSuffix)
	os.WriteFile(tmp, []byte("converted"), 0o644)
	// Another row already uses the target path, so the update hits the unique index.
	if _, _, err := c.deps.Store.(*store.Store).InsertScannedMedia(context.Background(), store.ScannedMedia{Path: "Movie.mp4", Title: "x"}); err != nil {
		t.Fatal(err)
	}

	err := c.swap(context.Background(), m, src, tmp, target, store.ReplacedFile{Path: "Movie.mp4", VideoCodec: "h264"})
	if err == nil {
		t.Fatal("swap succeeded, want a database error")
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Errorf("the converted file this job created should be removed, stat err = %v", statErr)
	}
	if got := readFile(t, src); got != "original" {
		t.Errorf("original = %q, want it untouched", got)
	}
}

func TestSwapReplacesAndDeletesOriginal(t *testing.T) {
	c, m, media := newSwapFixture(t, "Movie.mkv")
	src := filepath.Join(media, "Movie.mkv")
	target := filepath.Join(media, "Movie.mp4")
	tmp := filepath.Join(media, ".Movie"+tmpSuffix)
	os.WriteFile(tmp, []byte("converted"), 0o644)

	if err := c.swap(context.Background(), m, src, tmp, target, store.ReplacedFile{Path: "Movie.mp4", VideoCodec: "h264"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, target); got != "converted" {
		t.Errorf("target = %q", got)
	}
	for _, gone := range []string{src, tmp} {
		if _, err := os.Stat(gone); !os.IsNotExist(err) {
			t.Errorf("%s should be gone, stat err = %v", filepath.Base(gone), err)
		}
	}
	if cur, _ := c.deps.Store.MediaByID(context.Background(), m.ID); cur.Path != "Movie.mp4" || cur.VideoCodec != "h264" {
		t.Errorf("row = %+v", cur)
	}
}

// A same-name MP4 (wrong codecs inside) is replaced in one atomic rename.
func TestSwapSameNameReplacesInPlace(t *testing.T) {
	c, m, media := newSwapFixture(t, "Movie.mp4")
	path := filepath.Join(media, "Movie.mp4")
	tmp := filepath.Join(media, ".Movie"+tmpSuffix)
	os.WriteFile(tmp, []byte("converted"), 0o644)

	if err := c.swap(context.Background(), m, path, tmp, path, store.ReplacedFile{Path: "Movie.mp4", VideoCodec: "h264"}); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, path); got != "converted" {
		t.Errorf("file = %q", got)
	}
}

// When the row moved on while converting (a rescan replaced it), the swap
// stops without touching any file, and says why.
func TestSwapRefusesWhenTheRowChanged(t *testing.T) {
	c, m, media := newSwapFixture(t, "Movie.mkv")
	src := filepath.Join(media, "Movie.mkv")
	target := filepath.Join(media, "Movie.mp4")
	tmp := filepath.Join(media, ".Movie"+tmpSuffix)
	os.WriteFile(tmp, []byte("converted"), 0o644)
	m.Path = "Something else.mkv" // the job's snapshot no longer matches the row

	err := c.swap(context.Background(), m, src, tmp, target, store.ReplacedFile{Path: "Movie.mp4"})
	if !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("swap error = %v, want ErrSourceChanged", err)
	}
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Error("no file should be placed when the row changed")
	}
}

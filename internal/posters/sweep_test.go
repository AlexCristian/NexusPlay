package posters

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/collections"
)

type fakeStore struct{ names collections.Set[string] }

func (store fakeStore) PosterNames(context.Context) (collections.Set[string], error) {
	return store.names, nil
}

func TestSweepRemovesOnlyOldUnusedFiles(t *testing.T) {
	directory := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)
	write := func(name string, mod time.Time) {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte("img"), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(path, mod, mod)
	}
	write("used.jpg", old)
	write("unused.jpg", old)
	write("new-upload.png", time.Now()) // not saved to a row yet
	write(".poster-123", old)           // an interrupted download
	os.Mkdir(filepath.Join(directory, "sub"), 0o755)

	sweeper := NewSweeper(fakeStore{names: collections.NewSet("used.jpg")}, Config{Directory: directory}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	removed, err := sweeper.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	entries, _ := os.ReadDir(directory)
	for _, entry := range entries {
		left = append(left, entry.Name())
	}
	want := []string{"new-upload.png", "sub", "used.jpg"}
	if removed != 2 || !slices.Equal(left, want) {
		t.Errorf("removed %d, left %v; want 2 removed and %v left", removed, left, want)
	}
}

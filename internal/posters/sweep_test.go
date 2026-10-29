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
	dir := t.TempDir()
	old := time.Now().Add(-2 * time.Hour)
	write := func(name string, mod time.Time) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("img"), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(p, mod, mod)
	}
	write("used.jpg", old)
	write("unused.jpg", old)
	write("new-upload.png", time.Now()) // not saved to a row yet
	write(".poster-123", old)           // an interrupted download
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)

	s := NewSweeper(fakeStore{names: collections.NewSet("used.jpg")}, Config{Directory: dir}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	n, err := s.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var left []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		left = append(left, e.Name())
	}
	want := []string{"new-upload.png", "sub", "used.jpg"}
	if n != 2 || !slices.Equal(left, want) {
		t.Errorf("removed %d, left %v; want 2 removed and %v left", n, left, want)
	}
}

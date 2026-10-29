package store

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Queries that select extra columns after mediaCols used to re-list the
// scan by hand and forgot AddedAt and PublishedAt.
func TestListQueriesKeepEveryMediaColumn(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	user := mustUser(t, store, "a@example.com", "a")
	id := mustMedia(t, store, "m.mp4")
	if err := store.UpdateMedia(ctx, id, MediaUpdate{Title: "M"}, true); err != nil {
		t.Fatal(err)
	}
	store.SaveProgress(ctx, user.ID, id, 10, false)
	store.SetConvertFailed(ctx, id, "boom")

	want, err := store.MediaByID(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if want.AddedAt.IsZero() || want.PublishedAt.IsZero() {
		t.Fatalf("MediaByID lost timestamps: %+v", want)
	}
	resume, err := store.InProgress(ctx, user.ID, 10)
	if err != nil || len(resume) != 1 {
		t.Fatalf("InProgress = %+v, %v", resume, err)
	}
	failed, err := store.ConversionFailures(ctx)
	if err != nil || len(failed) != 1 {
		t.Fatalf("ConversionFailures = %+v, %v", failed, err)
	}
	for name, got := range map[string]Media{"InProgress": resume[0].Media, "ConversionFailures": failed[0].Media} {
		if !got.AddedAt.Equal(want.AddedAt) || !got.PublishedAt.Equal(want.PublishedAt) {
			t.Errorf("%s: AddedAt=%v PublishedAt=%v, want %v and %v", name, got.AddedAt, got.PublishedAt, want.AddedAt, want.PublishedAt)
		}
	}
	if resume[0].Position != 10 || failed[0].Error != "boom" {
		t.Errorf("extra columns: position=%v error=%q", resume[0].Position, failed[0].Error)
	}
}

func TestTruncateRunes(t *testing.T) {
	for _, testCase := range []struct {
		input string
		limit int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello", 3, "hel"},
		{"ăîș", 3, "ă"}, // each letter is 2 bytes; never cut one in half
		{"ăîș", 4, "ăî"},
		{"日本", 4, "日"},
	} {
		if got := truncateRunes(testCase.input, testCase.limit); got != testCase.want {
			t.Errorf("truncateRunes(%q, %d) = %q, want %q", testCase.input, testCase.limit, got, testCase.want)
		}
	}
}

// Write transactions that first read (creating users, voting) must not fail
// with SQLITE_BUSY when they run at the same time.
func TestConcurrentWriteTransactions(t *testing.T) {
	ctx := context.Background()
	store := newStore(t)
	var waitGroup sync.WaitGroup
	problems := make(chan error, 40)
	for i := 0; i < 40; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			email := time.Now().Format("150405.000000000") + string(rune('a'+i%26)) + "@x.com"
			_, err := store.CreateUser(ctx, email, email, "hash", 0)
			problems <- err
		}()
	}
	waitGroup.Wait()
	close(problems)
	for err := range problems {
		if err != nil {
			t.Errorf("concurrent CreateUser: %v", err)
		}
	}
	admins := 0
	users, _ := queryAll(ctx, store.database, scanUser, `SELECT `+userColumns+` FROM users`)
	for _, user := range users {
		if user.IsAdmin {
			admins++
		}
	}
	if len(users) != 40 || admins != 1 {
		t.Errorf("%d users, %d admins; want 40 and exactly 1", len(users), admins)
	}
}

// A data folder with URL-special characters must open the right file, with
// its settings applied.
func TestOpenPathWithQueryCharacters(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "we?ird#dir %20x")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "nexusplay.db")
	store, err := Open(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := os.Stat(path); err != nil {
		t.Errorf("database not created at %s: %v", path, err)
	}
	var mode string
	if err := store.database.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %q (%v), want wal: settings were not applied", mode, err)
	}
}

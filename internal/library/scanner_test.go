package library

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// makeVideo writes a one-second test clip with ffmpeg.
func makeVideo(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("ffmpeg", "-loglevel", "error", "-f", "lavfi", "-i", "testsrc=size=160x90:rate=10",
		"-t", "1", "-c:v", "libx264", "-pix_fmt", "yuv420p", path).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg: %v: %s", err, out)
	}
	old := time.Now().Add(-time.Hour)
	os.Chtimes(path, old, old)
}

func TestScan(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffmpeg/ffprobe not installed")
	}
	ctx := context.Background()
	directory := t.TempDir()
	media := filepath.Join(directory, "media")
	database, err := store.Open(ctx, filepath.Join(directory, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	makeVideo(t, filepath.Join(media, "Movies", "Alien (1979).mp4"))
	makeVideo(t, filepath.Join(media, "Cosmos", "Season 1", "Cosmos.S01E02.mp4"))
	makeVideo(t, filepath.Join(media, ".trash", "skipped.mp4"))
	os.WriteFile(filepath.Join(media, "Movies", "Alien (1979).ro.srt"), []byte("1\n00:00:00,000 --> 00:00:01,000\nSalut\n"), 0o644)
	os.WriteFile(filepath.Join(media, "Movies", "notes.txt"), []byte("ignored"), 0o644)
	// A file that is still being copied in must wait.
	fresh := filepath.Join(media, "Movies", "Copying.mp4")
	makeVideo(t, fresh)
	os.Chtimes(fresh, time.Now(), time.Now())

	scanner := NewScanner(Deps{Store: database, Prober: ffmpeg.Tools{FFprobe: "ffprobe"}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, Config{MediaDirectory: media})
	result, err := scanner.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Added != 2 || result.Subtitles != 1 || result.Waiting != 1 {
		t.Fatalf("first scan = %+v, want 2 added, 1 subtitle, 1 waiting", result)
	}
	pending, _ := database.PendingMedia(ctx)
	byPath := map[string]store.Media{}
	for _, media := range pending {
		byPath[media.Path] = media
	}
	alien := byPath["Movies/Alien (1979).mp4"]
	if alien.Title != "Alien" || alien.Year != "1979" || alien.VideoCodec != "h264" || alien.Duration < 0.5 {
		t.Errorf("movie = %+v", alien)
	}
	mediaItem := byPath["Cosmos/Season 1/Cosmos.S01E02.mp4"]
	if mediaItem.Season != 1 || mediaItem.Episode != 2 {
		t.Errorf("episode = %+v", mediaItem)
	}
	subs, _ := database.Subtitles(ctx, alien.ID)
	if len(subs) != 1 || subs[0].Language != "ro" || subs[0].Label != "Română" {
		t.Errorf("sidecar subtitles = %+v", subs)
	}

	// A second scan changes nothing.
	if result, _ := scanner.Scan(ctx); result.Added+result.Removed+result.Subtitles != 0 {
		t.Errorf("rescan = %+v, want no changes", result)
	}

	// Deleted files are removed from the library.
	os.Remove(filepath.Join(media, "Cosmos", "Season 1", "Cosmos.S01E02.mp4"))
	if result, _ := scanner.Scan(ctx); result.Removed != 1 {
		t.Errorf("after deleting a file: %+v, want 1 removed", result)
	}

	// An empty folder (an unmounted drive) must not wipe the library.
	os.RemoveAll(filepath.Join(media, "Movies"))
	if result, _ := scanner.Scan(ctx); result.Removed != 0 {
		t.Errorf("empty media folder removed %d items", result.Removed)
	}
}

// A folder that cannot be read (a flaky network share, wrong permissions)
// must not make its videos disappear from the library.
func TestScanKeepsMediaInUnreadableFolders(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffmpeg/ffprobe not installed")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores file permissions")
	}
	ctx := context.Background()
	directory := t.TempDir()
	media := filepath.Join(directory, "media")
	database, err := store.Open(ctx, filepath.Join(directory, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	makeVideo(t, filepath.Join(media, "Movies", "Alien (1979).mp4"))
	makeVideo(t, filepath.Join(media, "Share", "Cosmos.S01E01.mp4"))
	scanner := NewScanner(Deps{Store: database, Prober: ffmpeg.Tools{FFprobe: "ffprobe"}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, Config{MediaDirectory: media})
	if result, err := scanner.Scan(ctx); err != nil || result.Added != 2 {
		t.Fatalf("first scan = %+v, %v", result, err)
	}

	share := filepath.Join(media, "Share")
	if err := os.Chmod(share, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(share, 0o755) })
	result, err := scanner.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if result.Removed != 0 {
		t.Errorf("an unreadable folder removed %d items", result.Removed)
	}
	if paths, _ := database.MediaPaths(ctx); len(paths) != 2 {
		t.Errorf("library = %v, want both items kept", paths)
	}
}

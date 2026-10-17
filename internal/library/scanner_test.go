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
	dir := t.TempDir()
	media := filepath.Join(dir, "media")
	st, err := store.Open(ctx, filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	makeVideo(t, filepath.Join(media, "Movies", "Alien (1979).mp4"))
	makeVideo(t, filepath.Join(media, "Cosmos", "Season 1", "Cosmos.S01E02.mp4"))
	makeVideo(t, filepath.Join(media, ".trash", "skipped.mp4"))
	os.WriteFile(filepath.Join(media, "Movies", "Alien (1979).ro.srt"), []byte("1\n00:00:00,000 --> 00:00:01,000\nSalut\n"), 0o644)
	os.WriteFile(filepath.Join(media, "Movies", "notes.txt"), []byte("ignored"), 0o644)
	// A file that is still being copied in must wait.
	fresh := filepath.Join(media, "Movies", "Copying.mp4")
	makeVideo(t, fresh)
	os.Chtimes(fresh, time.Now(), time.Now())

	sc := NewScanner(Deps{Store: st, Prober: ffmpeg.Tools{FFprobe: "ffprobe"}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, Config{MediaDir: media})
	res, err := sc.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Added != 2 || res.Subtitles != 1 || res.Waiting != 1 {
		t.Fatalf("first scan = %+v, want 2 added, 1 subtitle, 1 waiting", res)
	}
	pending, _ := st.PendingMedia(ctx)
	byPath := map[string]store.Media{}
	for _, m := range pending {
		byPath[m.Path] = m
	}
	alien := byPath["Movies/Alien (1979).mp4"]
	if alien.Title != "Alien" || alien.Year != "1979" || alien.VideoCodec != "h264" || alien.Duration < 0.5 {
		t.Errorf("movie = %+v", alien)
	}
	ep := byPath["Cosmos/Season 1/Cosmos.S01E02.mp4"]
	if ep.Season != 1 || ep.Episode != 2 {
		t.Errorf("episode = %+v", ep)
	}
	subs, _ := st.Subtitles(ctx, alien.ID)
	if len(subs) != 1 || subs[0].Language != "ro" || subs[0].Label != "Română" {
		t.Errorf("sidecar subtitles = %+v", subs)
	}

	// A second scan changes nothing.
	if res, _ := sc.Scan(ctx); res.Added+res.Removed+res.Subtitles != 0 {
		t.Errorf("rescan = %+v, want no changes", res)
	}

	// Deleted files are removed from the library.
	os.Remove(filepath.Join(media, "Cosmos", "Season 1", "Cosmos.S01E02.mp4"))
	if res, _ := sc.Scan(ctx); res.Removed != 1 {
		t.Errorf("after deleting a file: %+v, want 1 removed", res)
	}

	// An empty folder (an unmounted drive) must not wipe the library.
	os.RemoveAll(filepath.Join(media, "Movies"))
	if res, _ := sc.Scan(ctx); res.Removed != 0 {
		t.Errorf("empty media folder removed %d items", res.Removed)
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
	dir := t.TempDir()
	media := filepath.Join(dir, "media")
	st, err := store.Open(ctx, filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	makeVideo(t, filepath.Join(media, "Movies", "Alien (1979).mp4"))
	makeVideo(t, filepath.Join(media, "Share", "Cosmos.S01E01.mp4"))
	sc := NewScanner(Deps{Store: st, Prober: ffmpeg.Tools{FFprobe: "ffprobe"}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, Config{MediaDir: media})
	if res, err := sc.Scan(ctx); err != nil || res.Added != 2 {
		t.Fatalf("first scan = %+v, %v", res, err)
	}

	share := filepath.Join(media, "Share")
	if err := os.Chmod(share, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(share, 0o755) })
	res, err := sc.Scan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 0 {
		t.Errorf("an unreadable folder removed %d items", res.Removed)
	}
	if paths, _ := st.MediaPaths(ctx); len(paths) != 2 {
		t.Errorf("library = %v, want both items kept", paths)
	}
}

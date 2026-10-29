//go:build integration

package integration

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func pixelFormatOf(t *testing.T, file string) string {
	t.Helper()
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0",
		"-show_entries", "stream=pix_fmt", "-of", "csv=p=0", file).Output()
	if err != nil {
		t.Fatal(err)
	}
	// MPEG-TS lists each stream twice (under its program and on its own).
	return strings.Fields(string(out))[0]
}

// A 10-bit H.264 MP4 plays in no browser. Without conversion it must go
// through HLS, re-encoded to 8-bit, instead of being sent (or copied) as is.
func TestTenBitPlaysThroughHLS(t *testing.T) {
	s := startServer(t, serverOpts{media: []string{tenBit}})
	s.waitForMedia(1)
	s.publishAll()
	id := s.mediaID(tenBit)
	if m, _ := s.App.Store.MediaByID(context.Background(), id); m.PixelFormat != "yuv420p10le" {
		t.Fatalf("scanner recorded pix_fmt %q, want yuv420p10le", m.PixelFormat)
	}
	viewer := s.register("viewer")
	_, page := viewer.get("/watch/" + itoa(id))
	if !strings.Contains(page, `data-hls="true"`) {
		t.Fatal("a 10-bit file must be played through HLS")
	}
	_, playlist := viewer.get("/media/" + itoa(id) + "/hls/index.m3u8")
	var segment string
	for _, line := range strings.Split(playlist, "\n") {
		if strings.HasSuffix(line, ".ts") {
			segment = line
			break
		}
	}
	_, body := viewer.get("/media/" + itoa(id) + "/hls/" + segment)
	seg := filepath.Join(t.TempDir(), "seg.ts")
	os.WriteFile(seg, []byte(body), 0o644)
	if got := pixelFormatOf(t, seg); got != "yuv420p" {
		t.Errorf("HLS segment pix_fmt = %q, want yuv420p (re-encoded, not copied)", got)
	}
}

// With conversion on, the 10-bit file is replaced in place by an 8-bit MP4.
func TestTenBitIsConverted(t *testing.T) {
	s := startServer(t, serverOpts{media: []string{tenBit}, convert: true})
	s.waitForMedia(1)
	s.waitForQueue(2 * time.Minute)
	m, err := s.App.Store.MediaByID(context.Background(), s.mediaID(tenBit))
	if err != nil {
		t.Fatal(err)
	}
	if m.PixelFormat != "yuv420p" || m.Path != tenBit {
		t.Errorf("after conversion: path=%q pix_fmt=%q, want the same path with yuv420p", m.Path, m.PixelFormat)
	}
	if got := pixelFormatOf(t, filepath.Join(s.Media, filepath.FromSlash(tenBit))); got != "yuv420p" {
		t.Errorf("file on disk is %q, want yuv420p", got)
	}
}

// Files recorded before the pixel format was stored are re-probed by the
// scanner, so an old 10-bit file is found and handled.
func TestScannerBackfillsPixelFormat(t *testing.T) {
	s := startServer(t, serverOpts{media: []string{tenBit, movieMP4}})
	s.waitForMedia(2)
	ctx := context.Background()
	for _, p := range []string{tenBit, movieMP4} {
		if err := s.App.Store.SetPixelFormat(ctx, s.mediaID(p), ""); err != nil { // as if added before migration 0003
			t.Fatal(err)
		}
	}
	if _, err := s.App.Scanner.Scan(ctx); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]string{tenBit: "yuv420p10le", movieMP4: "yuv420p"} {
		if m, _ := s.App.Store.MediaByID(ctx, s.mediaID(p)); m.PixelFormat != want {
			t.Errorf("%s: pix_fmt after rescan = %q, want %q", p, m.PixelFormat, want)
		}
	}
}

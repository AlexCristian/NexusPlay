//go:build integration

// Package integration runs NexusPlay end to end: the real app with real
// ffmpeg, a real SQLite database and, for browser and screenshot tests, a
// pinned headless Chrome. Run with:
//
//	./scripts/install-chrome.sh
//	go test -tags integration ./integration/
//
// Add -update to rewrite the reference screenshots in testdata/screenshots.
package integration

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/app"
	"github.com/AlexCristian/NexusPlay/internal/config"
	"github.com/AlexCristian/NexusPlay/internal/library"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

var update = flag.Bool("update", false, "rewrite reference screenshots instead of comparing against them")

// fixtureDir holds the generated test videos, made once per test run.
var fixtureDir string

// Fixture paths, relative to the media folder.
const (
	movieMP4 = "Movies/The Test Movie (2020).mp4"    // H.264/AAC MP4: plays directly
	movieSRT = "Movies/The Test Movie (2020).en.srt" // sidecar subtitles
	movieAVI = "Movies/Old Format Movie (1999).avi"  // MPEG-4 Part 2/MP3: needs a full transcode
	episode1 = "Test Show (2019)/Season 1/Test.Show.S01E01.Pilot.mkv"
	episode2 = "Test Show (2019)/Season 1/Test.Show.S01E02.The.Second.One.mkv"
	episode3 = "Test Show (2019)/Season 2/Test.Show.S02E01.mp4"
	// French 5.1 AC-3 and English stereo AAC, plus a built-in English subtitle track.
	multiTrack = "Movies/Multi Language (2018).mkv"
	// 10-bit H.264 in MP4: looks browser-ready by codec, but no browser decodes it.
	tenBit     = "Movies/Ten Bit (2021).mp4"
	fixtureLen = "12" // seconds
)

const subtitleSRT = "1\r\n00:00:01,000 --> 00:00:04,000\r\nHello thre!\r\n\r\n" +
	"2\r\n00:00:05,000 --> 00:00:08,000\r\nSecond line\r\nwith two rows\r\n"

func TestMain(m *testing.M) {
	flag.Parse()
	code := func() int {
		if _, err := exec.LookPath("ffmpeg"); err != nil {
			if os.Getenv("NEXUSPLAY_REQUIRE_INTEGRATION") != "" {
				fmt.Fprintln(os.Stderr, "ffmpeg is required for the integration tests")
				return 1
			}
			fmt.Fprintln(os.Stderr, "skipping integration tests: ffmpeg not installed")
			return 0
		}
		dir, err := os.MkdirTemp("", "nexusplay-fixtures-")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		defer os.RemoveAll(dir)
		fixtureDir = dir
		if err := makeFixtures(dir); err != nil {
			fmt.Fprintln(os.Stderr, "making fixtures:", err)
			return 1
		}
		return m.Run()
	}()
	os.Exit(code)
}

func makeFixtures(dir string) error {
	videos := []struct {
		path string
		args []string
	}{
		{movieMP4, []string{"-f", "lavfi", "-i", "testsrc=size=640x360:rate=25", "-f", "lavfi", "-i", "sine=frequency=440",
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart"}},
		{movieAVI, []string{"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=25", "-f", "lavfi", "-i", "sine=frequency=330",
			"-c:v", "mpeg4", "-c:a", "libmp3lame"}},
		{episode1, []string{"-f", "lavfi", "-i", "smptebars=size=640x360:rate=25", "-f", "lavfi", "-i", "sine=frequency=550",
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac"}},
		{episode2, []string{"-f", "lavfi", "-i", "smptebars=size=640x360:rate=25", "-f", "lavfi", "-i", "sine=frequency=660",
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac"}},
		{episode3, []string{"-f", "lavfi", "-i", "testsrc=size=640x360:rate=25", "-f", "lavfi", "-i", "sine=frequency=770",
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart"}},
		{tenBit, []string{"-f", "lavfi", "-i", "testsrc2=size=640x360:rate=25", "-f", "lavfi", "-i", "sine=frequency=990",
			"-c:v", "libx264", "-pix_fmt", "yuv420p10le", "-profile:v", "high10", "-c:a", "aac", "-movflags", "+faststart"}},
	}
	embedded := filepath.Join(dir, ".embedded.srt")
	if err := os.WriteFile(embedded, []byte("1\n00:00:01,000 --> 00:00:03,000\nBuilt-in line\n"), 0o644); err != nil {
		return err
	}
	defer os.Remove(embedded)
	videos = append(videos, struct {
		path string
		args []string
	}{multiTrack, []string{"-f", "lavfi", "-i", "testsrc=size=640x360:rate=25", "-f", "lavfi", "-i", "sine=frequency=440",
		"-f", "lavfi", "-i", "sine=frequency=880", "-i", embedded,
		"-map", "0:v", "-map", "1:a", "-map", "2:a", "-map", "3:s",
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		"-c:a:0", "ac3", "-ac:a:0", "6", "-c:a:1", "aac", "-ac:a:1", "2", "-c:s", "srt",
		"-metadata:s:a:0", "language=fre", "-metadata:s:a:1", "language=eng", "-metadata:s:s:0", "language=eng",
		"-disposition:a:0", "0", "-disposition:a:1", "default"}})
	for _, v := range videos {
		out := filepath.Join(dir, filepath.FromSlash(v.path))
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return err
		}
		args := append([]string{"-loglevel", "error", "-y"}, v.args...)
		args = append(args, "-t", fixtureLen, "-bitexact", out)
		if b, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %v: %s", v.path, err, b)
		}
	}
	return os.WriteFile(filepath.Join(dir, filepath.FromSlash(movieSRT)), []byte(subtitleSRT), 0o644)
}

// copyMedia copies the fixtures into a fresh media folder, dated an hour ago
// so the scanner does not treat them as still being copied.
func copyMedia(t *testing.T, dest string, paths ...string) {
	t.Helper()
	old := time.Now().Add(-time.Hour)
	for _, p := range paths {
		src := filepath.Join(fixtureDir, filepath.FromSlash(p))
		dst := filepath.Join(dest, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(src)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(dst, old, old)
	}
}

var allFixtures = []string{movieMP4, movieSRT, movieAVI, episode1, episode2, episode3}

// server is a running NexusPlay instance.
type server struct {
	t     *testing.T
	URL   string
	App   *app.App
	Media string
}

type serverOpts struct {
	media         []string // fixtures to put in the media folder
	maxTranscodes int
	ffmpeg        string // override the ffmpeg binary
	convert       bool   // run the background MP4 converter
}

func startServer(t *testing.T, o serverOpts) *server {
	t.Helper()
	dir := t.TempDir()
	media := filepath.Join(dir, "media")
	if err := os.MkdirAll(media, 0o755); err != nil {
		t.Fatal(err)
	}
	copyMedia(t, media, o.media...)
	if o.maxTranscodes == 0 {
		o.maxTranscodes = 2
	}
	if o.ffmpeg == "" {
		o.ffmpeg = "ffmpeg"
	}
	cfg := config.Config{
		MediaDir: media, DataDir: filepath.Join(dir, "data"),
		ScanInterval: time.Hour, VotesRequired: 2, MaxTranscodes: o.maxTranscodes,
		FFmpegPath: o.ffmpeg, FFprobePath: "ffprobe",
		Convert: o.convert, MaxConversions: 1,
	}
	logw := io.Discard
	if os.Getenv("NEXUSPLAY_TEST_LOG") != "" {
		logw = os.Stderr
	}
	log := slog.New(slog.NewTextHandler(logw, &slog.HandlerOptions{Level: slog.LevelDebug}))
	ctx, cancel := context.WithCancel(context.Background())
	a, err := app.New(ctx, cfg, log)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		cancel()
		a.Close()
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- a.Serve(ctx, ln) }()
	t.Cleanup(func() {
		// Serve returns once every worker (and every ffmpeg) has stopped and
		// the database is closed, so the temp folder can then be removed.
		cancel()
		if err := <-served; err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	s := &server{t: t, URL: "http://" + ln.Addr().String(), App: a, Media: media}
	return s
}

// waitForMedia waits until the scanner has found n media items.
func (s *server) waitForMedia(n int) {
	s.t.Helper()
	eventually(s.t, 30*time.Second, func() error {
		paths, err := s.App.Store.MediaPaths(context.Background())
		if err != nil {
			return err
		}
		if len(paths) != n {
			return fmt.Errorf("scanner found %d media, want %d", len(paths), n)
		}
		return nil
	})
}

// mediaID returns the ID of a fixture.
func (s *server) mediaID(path string) int64 {
	s.t.Helper()
	paths, err := s.App.Store.MediaPaths(context.Background())
	if err != nil {
		s.t.Fatal(err)
	}
	id, ok := paths[path]
	if !ok {
		s.t.Fatalf("%s is not in the library", path)
	}
	return id
}

// publishAll publishes everything the scanner found except the given paths,
// the way an administrator accepting the scanner's guesses would.
func (s *server) publishAll(except ...string) {
	s.t.Helper()
	ctx := context.Background()
	pending, err := s.App.Store.PendingMedia(ctx)
	if err != nil {
		s.t.Fatal(err)
	}
	skip := map[string]bool{}
	for _, p := range except {
		skip[p] = true
	}
	for _, m := range pending {
		if skip[m.Path] {
			continue
		}
		g := library.ParsePath(m.Path)
		u := store.MediaUpdate{Title: m.Title, Year: m.Year, Season: m.Season, Episode: m.Episode, Category: "Movies"}
		if g.IsEpisode() {
			u.Category = ""
			c, err := s.App.Store.CollectionByName(ctx, g.Show)
			if err != nil {
				id, err := s.App.Store.CreateCollection(ctx, store.Collection{Name: g.Show, Category: "TV Shows", Year: g.Year,
					Overview: "A show made of test patterns, used by the integration tests."})
				if err != nil {
					s.t.Fatal(err)
				}
				c.ID = id
			}
			u.CollectionID = c.ID
		}
		if err := s.App.Store.UpdateMedia(ctx, m.ID, u, true); err != nil {
			s.t.Fatal(err)
		}
	}
}

func eventually(t *testing.T, timeout time.Duration, check func() error) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		err := check()
		if err == nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v: %v", timeout, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// httpUser is a cookie-carrying HTTP client acting as one signed-in person.
type httpUser struct {
	s    *server
	c    *http.Client
	csrf string
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (s *server) register(nick string) *httpUser {
	s.t.Helper()
	jar, _ := cookiejar.New(nil)
	u := &httpUser{s: s, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}}
	resp, _ := u.post("/register", url.Values{"nickname": {nick}, "email": {nick + "@example.com"}, "password": {"integration-pass"}})
	if resp.StatusCode != http.StatusSeeOther {
		s.t.Fatalf("register %s: %d", nick, resp.StatusCode)
	}
	_, page := u.get("/")
	m := csrfRe.FindStringSubmatch(page)
	if m == nil {
		s.t.Fatalf("no CSRF token after registering %s", nick)
	}
	u.csrf = m[1]
	return u
}

func (u *httpUser) get(path string) (*http.Response, string) {
	u.s.t.Helper()
	return u.do("GET", path, nil, "")
}

func (u *httpUser) post(path string, form url.Values) (*http.Response, string) {
	u.s.t.Helper()
	if u.csrf != "" {
		form.Set("csrf_token", u.csrf)
	}
	return u.do("POST", path, strings.NewReader(form.Encode()), "application/x-www-form-urlencoded")
}

func (u *httpUser) do(method, path string, body io.Reader, contentType string) (*http.Response, string) {
	u.s.t.Helper()
	req, err := http.NewRequest(method, u.s.URL+path, body)
	if err != nil {
		u.s.t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if method != "GET" {
		req.Header.Set("Origin", u.s.URL)
	}
	resp, err := u.c.Do(req)
	if err != nil {
		u.s.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

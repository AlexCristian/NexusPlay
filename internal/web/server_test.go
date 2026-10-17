package web

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/AlexCristian/NexusPlay/internal/config"
	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/library"
	"github.com/AlexCristian/NexusPlay/internal/metadata"
	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/stream"
	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

type env struct {
	t     *testing.T
	srv   *httptest.Server
	store *store.Store
	cfg   config.Config
}

func newEnv(t *testing.T, mutate func(*config.Config)) *env {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Config{
		MediaDir: filepath.Join(dir, "media"), DataDir: filepath.Join(dir, "data"),
		VotesRequired: 2, MaxTranscodes: 1, FFmpegPath: "ffmpeg", FFprobePath: "ffprobe",
	}
	if mutate != nil {
		mutate(&cfg)
	}
	for _, d := range []string{cfg.MediaDir, cfg.PosterDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	st, err := store.Open(context.Background(), cfg.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	tm, err := stream.NewManager(ffmpeg.Tools{FFmpeg: "ffmpeg", FFprobe: "ffprobe"}, cfg.TranscodeDir(), 1, log)
	if err != nil {
		t.Fatal(err)
	}
	meta := metadata.New(metadata.Config{PosterDir: cfg.PosterDir()}, log)
	s := &Server{Cfg: cfg, Store: st, Meta: meta, Transcodes: tm, Log: log,
		Scanner: &library.Scanner{Store: st, MediaDir: cfg.MediaDir, Meta: meta, Log: log}}
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(func() { ts.Close(); st.Close() })
	return &env{t: t, srv: ts, store: st, cfg: cfg}
}

// client is a browser-like client with its own cookies.
type client struct {
	e    *env
	http *http.Client
	csrf string
}

func (e *env) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{e: e, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}}
}

func (c *client) do(method, path string, form url.Values, headers map[string]string) (*http.Response, string) {
	c.e.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req, _ := http.NewRequest(method, c.e.srv.URL+path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if method != http.MethodGet {
		req.Header.Set("Origin", c.e.srv.URL)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

var csrfRe = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (c *client) register(nick string) {
	c.e.t.Helper()
	resp, _ := c.do("POST", "/register", url.Values{"nickname": {nick}, "email": {nick + "@example.com"}, "password": {"long enough password"}}, nil)
	if resp.StatusCode != http.StatusSeeOther {
		c.e.t.Fatalf("register %s: status %d", nick, resp.StatusCode)
	}
	_, page := c.do("GET", "/", nil, nil)
	m := csrfRe.FindStringSubmatch(page)
	if m == nil {
		c.e.t.Fatalf("no CSRF token on the home page after registering %s", nick)
	}
	c.csrf = m[1]
}

func TestFirstAccountAdministers(t *testing.T) {
	e := newEnv(t, nil)
	admin, user := e.client(), e.client()

	_, page := admin.do("GET", "/register", nil, nil)
	if !strings.Contains(page, "becomes the administrator") {
		t.Error("first registration should say it creates the administrator")
	}
	admin.register("alex")
	user.register("sam")

	if resp, body := admin.do("GET", "/admin", nil, nil); resp.StatusCode != 200 || !strings.Contains(body, "Control panel") {
		t.Errorf("admin: GET /admin = %d", resp.StatusCode)
	}
	if resp, _ := user.do("GET", "/admin", nil, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("regular user: GET /admin = %d, want 403", resp.StatusCode)
	}
	if resp, _ := e.client().do("GET", "/admin", nil, nil); resp.StatusCode != http.StatusSeeOther {
		t.Errorf("anonymous: GET /admin = %d, want redirect to login", resp.StatusCode)
	}
}

func TestLogin(t *testing.T) {
	e := newEnv(t, nil)
	e.client().register("alex")
	c := e.client()
	resp, body := c.do("POST", "/login", url.Values{"email": {"alex@example.com"}, "password": {"wrong password"}}, nil)
	if resp.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "Wrong e-mail or password") {
		t.Errorf("bad password: %d", resp.StatusCode)
	}
	resp, _ = c.do("POST", "/login", url.Values{"email": {"nobody@example.com"}, "password": {"x"}}, nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("unknown email: %d", resp.StatusCode)
	}
	resp, _ = c.do("POST", "/login", url.Values{"email": {"ALEX@example.com"}, "password": {"long enough password"}, "next": {"//evil.example"}}, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/" {
		t.Errorf("login: %d to %q, want 303 to / (open redirect refused)", resp.StatusCode, resp.Header.Get("Location"))
	}
	resp, _ = c.do("POST", "/login", url.Values{"email": {"alex@example.com"}, "password": {"long enough password"}}, map[string]string{"HX-Request": "true"})
	if resp.Header.Get("HX-Redirect") == "" {
		t.Error("htmx login should answer with HX-Redirect")
	}
}

func TestCSRF(t *testing.T) {
	e := newEnv(t, nil)
	c := e.client()
	c.register("alex")
	coll, err := e.store.CreateCollection(context.Background(), store.Collection{Name: "Show", Category: "TV Shows"})
	if err != nil {
		t.Fatal(err)
	}
	path := "/collections/" + itoa(coll) + "/subscribe"

	if resp, _ := c.do("POST", path, url.Values{}, nil); resp.StatusCode != http.StatusForbidden {
		t.Errorf("no token: %d, want 403", resp.StatusCode)
	}
	if resp, _ := c.do("POST", path, url.Values{}, map[string]string{"X-CSRF-Token": c.csrf, "Origin": "https://evil.example"}); resp.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site: %d, want 403", resp.StatusCode)
	}
	resp, body := c.do("POST", path, url.Values{}, map[string]string{"X-CSRF-Token": c.csrf})
	if resp.StatusCode != 200 || !strings.Contains(body, "Subscribed") {
		t.Errorf("with header token: %d %q", resp.StatusCode, body)
	}
	if resp, _ := c.do("POST", path, url.Values{"csrf_token": {c.csrf}}, nil); resp.StatusCode != 200 {
		t.Errorf("with form token: %d", resp.StatusCode)
	}
}

func TestUnpublishedMediaIsHidden(t *testing.T) {
	e := newEnv(t, nil)
	admin := e.client()
	admin.register("alex")
	id, _ := e.store.InsertScannedMedia(context.Background(), store.ScannedMedia{Path: "secret.mp4", Title: "Secret", VideoCodec: "h264", AudioCodec: "aac"})
	path := "/watch/" + itoa(id)

	if resp, _ := e.client().do("GET", path, nil, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("anonymous, unpublished: %d, want 404", resp.StatusCode)
	}
	if resp, body := admin.do("GET", path, nil, nil); resp.StatusCode != 200 || !strings.Contains(body, `data-src="/media/`+itoa(id)+`/file"`) {
		t.Errorf("admin preview: %d", resp.StatusCode)
	}
	e.store.UpdateMedia(context.Background(), id, store.MediaUpdate{Title: "Secret"}, true)
	if resp, _ := e.client().do("GET", path, nil, nil); resp.StatusCode != 200 {
		t.Errorf("anonymous, published: %d, want 200", resp.StatusCode)
	}
}

func TestMediaStreamingSupportsRanges(t *testing.T) {
	e := newEnv(t, nil)
	if err := os.WriteFile(filepath.Join(e.cfg.MediaDir, "clip.mp4"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, _ := e.store.InsertScannedMedia(ctx, store.ScannedMedia{Path: "clip.mp4", Title: "Clip", VideoCodec: "h264"})
	e.store.UpdateMedia(ctx, id, store.MediaUpdate{Title: "Clip"}, true)
	resp, body := e.client().do("GET", "/media/"+itoa(id)+"/file", nil, map[string]string{"Range": "bytes=2-5"})
	if resp.StatusCode != http.StatusPartialContent || body != "2345" || resp.Header.Get("Content-Type") != "video/mp4" {
		t.Errorf("range request: %d %q %q", resp.StatusCode, body, resp.Header.Get("Content-Type"))
	}
}

func TestSubtitleFlow(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	a, b := e.client(), e.client()
	a.register("alex")
	b.register("sam")
	id, _ := e.store.InsertScannedMedia(ctx, store.ScannedMedia{Path: "m.mp4", Title: "M", VideoCodec: "h264"})
	e.store.UpdateMedia(ctx, id, store.MediaUpdate{Title: "M"}, true)
	sub, _, _ := e.store.AddSubtitle(ctx, store.NewSubtitle{MediaID: id, Language: "en", Label: "English", Cues: []subtitle.Cue{{StartMS: 1000, EndMS: 2000, Text: "Helo"}}})

	resp, body := a.do("GET", "/media/"+itoa(id)+"/subtitle-report?track="+itoa(sub)+"&t=1500", nil, nil)
	if resp.StatusCode != 200 || !strings.Contains(body, "Helo") {
		t.Fatalf("report dialog: %d", resp.StatusCode)
	}
	cue, _ := e.store.CueAt(ctx, sub, 1500)
	htmx := func(c *client) map[string]string {
		return map[string]string{"X-CSRF-Token": c.csrf, "HX-Request": "true"}
	}
	resp, body = a.do("POST", "/cues/"+itoa(cue.ID)+"/changes", url.Values{"text": {"Hello"}}, htmx(a))
	if resp.StatusCode != 200 || !strings.Contains(body, "Correction submitted") {
		t.Fatalf("propose: %d %s", resp.StatusCode, body)
	}
	changes, _ := e.store.Changes(ctx, cue.ID, 0)
	resp, body = b.do("POST", "/changes/"+itoa(changes[0].ID)+"/vote", url.Values{}, htmx(b))
	if resp.StatusCode != 200 || !strings.Contains(body, "Change applied") {
		t.Fatalf("vote: %d %s", resp.StatusCode, body)
	}
	// Without htmx (scripts not loaded yet) the form still works and returns to the video.
	resp, _ = a.do("POST", "/cues/"+itoa(cue.ID)+"/changes", url.Values{"text": {"Hello!"}, "csrf_token": {a.csrf}}, nil)
	if resp.StatusCode != http.StatusSeeOther || resp.Header.Get("Location") != "/watch/"+itoa(id) {
		t.Errorf("plain form post: %d to %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	_, vtt := e.client().do("GET", "/subtitles/"+itoa(sub), nil, nil)
	if !strings.Contains(vtt, "WEBVTT") || !strings.Contains(vtt, "Hello") {
		t.Errorf("vtt after vote:\n%s", vtt)
	}
}

func TestRequireLogin(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.RequireLogin = true })
	resp, _ := e.client().do("GET", "/", nil, nil)
	if resp.StatusCode != http.StatusSeeOther || !strings.HasPrefix(resp.Header.Get("Location"), "/login") {
		t.Errorf("anonymous home with RequireLogin: %d %q", resp.StatusCode, resp.Header.Get("Location"))
	}
	if resp, _ := e.client().do("GET", "/login", nil, nil); resp.StatusCode != 200 {
		t.Errorf("login page must stay reachable: %d", resp.StatusCode)
	}
	c := e.client()
	c.register("alex")
	if resp, _ := c.do("GET", "/", nil, nil); resp.StatusCode != 200 {
		t.Errorf("signed in: %d", resp.StatusCode)
	}
}

func TestPosterPathTraversal(t *testing.T) {
	e := newEnv(t, nil)
	for _, p := range []string{"/posters/..%2fnexusplay.db", "/posters/.hidden", "/posters/%2e%2e"} {
		if resp, _ := e.client().do("GET", p, nil, nil); resp.StatusCode == 200 {
			t.Errorf("GET %s = 200, want refusal", p)
		}
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"":                     "/",
		"/watch/3":             "/watch/3",
		"/search?q=a+b":        "/search?q=a+b",
		"/watch/3#t=10":        "/watch/3",
		"//evil.example":       "/",
		"///evil.example":      "/",
		"https://evil.example": "/",
		"javascript:alert(1)":  "/",
		`/\evil.example`:       "/",
		"/\t/evil.example":     "/", // browsers strip the tab: "//evil.example"
		"/\n/evil.example":     "/",
		"/\r\n/evil.example":   "/",
		" /watch/3":            "/",
		"watch/3":              "/",
	} {
		if got := safeNext(in); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHealthz(t *testing.T) {
	e := newEnv(t, nil)
	if resp, body := e.client().do("GET", "/healthz", nil, nil); resp.StatusCode != 200 || body != "ok\n" {
		t.Errorf("healthz: %d %q", resp.StatusCode, body)
	}
}

// An upload over the size limit is answered as too large, never as a CSRF
// failure, whether the token comes in a form field or a header.
func TestOversizeUploadIsRejectedAsTooLarge(t *testing.T) {
	e := newEnv(t, nil)
	admin := e.client()
	admin.register("alex")
	id, _ := e.store.InsertScannedMedia(context.Background(), store.ScannedMedia{Path: "m.mp4", Title: "M", VideoCodec: "h264"})

	upload := func(headerToken bool, size int) (*http.Response, string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if !headerToken {
			mw.WriteField("csrf_token", admin.csrf)
		}
		mw.WriteField("language", "en")
		fw, _ := mw.CreateFormFile("file", "big.srt")
		fw.Write(bytes.Repeat([]byte("x"), size))
		mw.Close()
		headers := map[string]string{"Content-Type": mw.FormDataContentType()}
		if headerToken {
			headers["X-CSRF-Token"] = admin.csrf
		}
		req, _ := http.NewRequest("POST", e.srv.URL+"/admin/media/"+itoa(id)+"/subtitles", &buf)
		req.Header.Set("Origin", e.srv.URL)
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := admin.http.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}

	resp, body := upload(false, maxUpload+1<<20)
	if resp.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(body, "too large") {
		t.Errorf("oversize upload with a form token: %d %q", resp.StatusCode, body)
	}
	resp, body = upload(true, maxUpload+1<<20)
	if resp.StatusCode == http.StatusForbidden || !strings.Contains(body, "too large") {
		t.Errorf("oversize upload with a header token: %d, body mentions too large: %v", resp.StatusCode, strings.Contains(body, "too large"))
	}
	// A normal upload still goes through (it is not a valid subtitle, so the
	// form comes back with that error, not a size or CSRF error).
	resp, body = upload(false, 1000)
	if resp.StatusCode != http.StatusBadRequest || !strings.Contains(body, "not a valid SRT or WebVTT") {
		t.Errorf("small upload: %d", resp.StatusCode)
	}
}

// Ordinary forms outside the admin area have a small limit.
func TestFormBodyLimit(t *testing.T) {
	e := newEnv(t, nil)
	c := e.client()
	c.register("alex")
	resp, _ := c.do("POST", "/me/progress", url.Values{"csrf_token": {c.csrf}, "pad": {strings.Repeat("x", maxFormBody)}}, nil)
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize form: %d, want 413", resp.StatusCode)
	}
}

// Members cannot act on unpublished media by guessing IDs.
func TestUnpublishedMediaRejectsProgressAndVotes(t *testing.T) {
	e := newEnv(t, nil)
	ctx := context.Background()
	e.client().register("admin")
	member := e.client()
	member.register("sam")
	id, _ := e.store.InsertScannedMedia(ctx, store.ScannedMedia{Path: "secret.mp4", Title: "Secret", VideoCodec: "h264"})
	sub, _, _ := e.store.AddSubtitle(ctx, store.NewSubtitle{MediaID: id, Language: "en", Label: "English", Cues: []subtitle.Cue{{StartMS: 0, EndMS: 1000, Text: "Helo"}}})
	cue, _ := e.store.CueAt(ctx, sub, 500)

	if resp, _ := member.do("POST", "/me/progress", url.Values{"csrf_token": {member.csrf}, "media_id": {itoa(id)}, "position": {"5"}}, nil); resp.StatusCode != http.StatusNotFound {
		t.Errorf("progress on unpublished media: %d, want 404", resp.StatusCode)
	}
	headers := map[string]string{"X-CSRF-Token": member.csrf, "HX-Request": "true"}
	if resp, _ := member.do("POST", "/cues/"+itoa(cue.ID)+"/changes", url.Values{"text": {"Hello"}}, headers); resp.StatusCode != http.StatusNotFound {
		t.Errorf("correction on unpublished media: %d, want 404", resp.StatusCode)
	}
	if changes, _ := e.store.Changes(ctx, cue.ID, 0); len(changes) != 0 {
		t.Errorf("a correction was recorded on unpublished media: %+v", changes)
	}

	e.store.UpdateMedia(ctx, id, store.MediaUpdate{Title: "Secret"}, true)
	for _, bad := range []string{"NaN", "Inf", "-1", "abc"} {
		if resp, _ := member.do("POST", "/me/progress", url.Values{"csrf_token": {member.csrf}, "media_id": {itoa(id)}, "position": {bad}}, nil); resp.StatusCode != http.StatusBadRequest {
			t.Errorf("position %q: %d, want 400", bad, resp.StatusCode)
		}
	}
	if resp, _ := member.do("POST", "/me/progress", url.Values{"csrf_token": {member.csrf}, "media_id": {itoa(id)}, "position": {"5"}}, nil); resp.StatusCode != http.StatusNoContent {
		t.Errorf("progress on published media: %d, want 204", resp.StatusCode)
	}
}

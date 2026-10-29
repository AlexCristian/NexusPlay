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
	"time"

	"github.com/AlexCristian/NexusPlay/internal/auth"
	"github.com/AlexCristian/NexusPlay/internal/config"
	"github.com/AlexCristian/NexusPlay/internal/convert"
	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/library"
	"github.com/AlexCristian/NexusPlay/internal/metadata"
	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/stream"
	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

type environment struct {
	t      *testing.T
	srv    *httptest.Server
	store  *store.Store
	config config.Config
}

func newEnvironment(t *testing.T, mutate func(*config.Config)) *environment {
	t.Helper()
	directory := t.TempDir()
	configuration := config.Config{
		MediaDirectory: filepath.Join(directory, "media"), DataDirectory: filepath.Join(directory, "data"),
		VotesRequired: 2, MaxTranscodes: 1, FFmpegPath: "ffmpeg", FFprobePath: "ffprobe",
	}
	if mutate != nil {
		mutate(&configuration)
	}
	for _, directory := range []string{configuration.MediaDirectory, configuration.PosterDirectory()} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	database, err := store.Open(context.Background(), configuration.DatabasePath())
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	manager, err := stream.NewManager(stream.FFmpegStarter(ffmpeg.Tools{FFmpeg: "ffmpeg", FFprobe: "ffprobe"}),
		stream.Config{Directory: configuration.TranscodeDirectory(), Max: 1}, log)
	if err != nil {
		t.Fatal(err)
	}
	tmdbClient := metadata.New(metadata.Config{PosterDirectory: configuration.PosterDirectory()}, log)
	accounts, err := auth.NewAccounts(database, configuration.MaxAccounts, log)
	if err != nil {
		t.Fatal(err)
	}
	testServer := &Server{Config: configuration, Store: database, Metadata: tmdbClient, Transcodes: manager, Converter: convert.Disabled{}, Log: log,
		Accounts: accounts, Sessions: auth.NewSessions(database, time.Hour, log),
		Scanner: library.NewScanner(library.Dependencies{Store: database, Metadata: tmdbClient, Log: log}, library.Config{MediaDirectory: configuration.MediaDirectory})}
	server := httptest.NewServer(testServer.Handler())
	t.Cleanup(func() { server.Close(); database.Close() })
	return &environment{t: t, srv: server, store: database, config: configuration}
}

// client is a browser-like client with its own cookies.
type client struct {
	environment *environment
	http        *http.Client
	csrf        string
}

func (environment *environment) client() *client {
	jar, _ := cookiejar.New(nil)
	return &client{environment: environment, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}}
}

func (client *client) sendForm(method, path string, form url.Values, headers map[string]string) (*http.Response, string) {
	client.environment.t.Helper()
	if form == nil {
		return client.send(method, path, nil, headers)
	}
	allHeaders := map[string]string{"Content-Type": "application/x-www-form-urlencoded"}
	for key, value := range headers {
		allHeaders[key] = value
	}
	return client.send(method, path, strings.NewReader(form.Encode()), allHeaders)
}

// send makes a request with any body, such as a multipart upload.
func (client *client) send(method, path string, body io.Reader, headers map[string]string) (*http.Response, string) {
	client.environment.t.Helper()
	request, _ := http.NewRequest(method, client.environment.srv.URL+path, body)
	if method != http.MethodGet {
		request.Header.Set("Origin", client.environment.srv.URL)
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.http.Do(request)
	if err != nil {
		client.environment.t.Fatal(err)
	}
	defer response.Body.Close()
	content, _ := io.ReadAll(response.Body)
	return response, string(content)
}

var csrfPattern = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func (client *client) register(nick string) {
	client.environment.t.Helper()
	response, _ := client.sendForm("POST", "/register", url.Values{"nickname": {nick}, "email": {nick + "@example.com"}, "password": {"long enough password"}}, nil)
	if response.StatusCode != http.StatusSeeOther {
		client.environment.t.Fatalf("register %s: status %d", nick, response.StatusCode)
	}
	_, page := client.sendForm("GET", "/", nil, nil)
	match := csrfPattern.FindStringSubmatch(page)
	if match == nil {
		client.environment.t.Fatalf("no CSRF token on the home page after registering %s", nick)
	}
	client.csrf = match[1]
}

func TestFirstAccountAdministers(t *testing.T) {
	environment := newEnvironment(t, nil)
	admin, user := environment.client(), environment.client()

	_, page := admin.sendForm("GET", "/register", nil, nil)
	if !strings.Contains(page, "becomes the administrator") {
		t.Error("first registration should say it creates the administrator")
	}
	admin.register("alex")
	user.register("sam")

	if response, body := admin.sendForm("GET", "/admin", nil, nil); response.StatusCode != 200 || !strings.Contains(body, "Control panel") {
		t.Errorf("admin: GET /admin = %d", response.StatusCode)
	}
	if response, _ := user.sendForm("GET", "/admin", nil, nil); response.StatusCode != http.StatusForbidden {
		t.Errorf("regular user: GET /admin = %d, want 403", response.StatusCode)
	}
	if response, _ := environment.client().sendForm("GET", "/admin", nil, nil); response.StatusCode != http.StatusSeeOther {
		t.Errorf("anonymous: GET /admin = %d, want redirect to login", response.StatusCode)
	}
}

func TestLogin(t *testing.T) {
	environment := newEnvironment(t, nil)
	environment.client().register("alex")
	client := environment.client()
	response, body := client.sendForm("POST", "/login", url.Values{"email": {"alex@example.com"}, "password": {"wrong password"}}, nil)
	if response.StatusCode != http.StatusUnauthorized || !strings.Contains(body, "Wrong e-mail or password") {
		t.Errorf("bad password: %d", response.StatusCode)
	}
	response, _ = client.sendForm("POST", "/login", url.Values{"email": {"nobody@example.com"}, "password": {"x"}}, nil)
	if response.StatusCode != http.StatusUnauthorized {
		t.Errorf("unknown email: %d", response.StatusCode)
	}
	response, _ = client.sendForm("POST", "/login", url.Values{"email": {"ALEX@example.com"}, "password": {"long enough password"}, "next": {"//evil.example"}}, nil)
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/" {
		t.Errorf("login: %d to %q, want 303 to / (open redirect refused)", response.StatusCode, response.Header.Get("Location"))
	}
	response, _ = client.sendForm("POST", "/login", url.Values{"email": {"alex@example.com"}, "password": {"long enough password"}}, map[string]string{"HX-Request": "true"})
	if response.Header.Get("HX-Redirect") == "" {
		t.Error("htmx login should answer with HX-Redirect")
	}
}

func TestCSRF(t *testing.T) {
	environment := newEnvironment(t, nil)
	client := environment.client()
	client.register("alex")
	coll, err := environment.store.CreateCollection(context.Background(), store.Collection{Name: "Show", Category: "TV Shows"})
	if err != nil {
		t.Fatal(err)
	}
	path := "/collections/" + itoa(coll) + "/subscribe"

	if response, _ := client.sendForm("POST", path, url.Values{}, nil); response.StatusCode != http.StatusForbidden {
		t.Errorf("no token: %d, want 403", response.StatusCode)
	}
	if response, _ := client.sendForm("POST", path, url.Values{}, map[string]string{"X-CSRF-Token": client.csrf, "Origin": "https://evil.example"}); response.StatusCode != http.StatusForbidden {
		t.Errorf("cross-site: %d, want 403", response.StatusCode)
	}
	response, body := client.sendForm("POST", path, url.Values{}, map[string]string{"X-CSRF-Token": client.csrf})
	if response.StatusCode != 200 || !strings.Contains(body, "Subscribed") {
		t.Errorf("with header token: %d %q", response.StatusCode, body)
	}
	if response, _ := client.sendForm("POST", path, url.Values{"csrf_token": {client.csrf}}, nil); response.StatusCode != 200 {
		t.Errorf("with form token: %d", response.StatusCode)
	}
}

func TestUnpublishedMediaIsHidden(t *testing.T) {
	environment := newEnvironment(t, nil)
	admin := environment.client()
	admin.register("alex")
	id, _, _ := environment.store.InsertScannedMedia(context.Background(), store.ScannedMedia{Path: "secret.mp4", Title: "Secret", VideoCodec: "h264", AudioCodec: "aac"})
	path := "/watch/" + itoa(id)

	if response, _ := environment.client().sendForm("GET", path, nil, nil); response.StatusCode != http.StatusNotFound {
		t.Errorf("anonymous, unpublished: %d, want 404", response.StatusCode)
	}
	if response, body := admin.sendForm("GET", path, nil, nil); response.StatusCode != 200 || !strings.Contains(body, `data-src="/media/`+itoa(id)+`/file"`) {
		t.Errorf("admin preview: %d", response.StatusCode)
	}
	environment.store.UpdateMedia(context.Background(), id, store.MediaUpdate{Title: "Secret"}, true)
	if response, _ := environment.client().sendForm("GET", path, nil, nil); response.StatusCode != 200 {
		t.Errorf("anonymous, published: %d, want 200", response.StatusCode)
	}
}

func TestMediaStreamingSupportsRanges(t *testing.T) {
	environment := newEnvironment(t, nil)
	if err := os.WriteFile(filepath.Join(environment.config.MediaDirectory, "clip.mp4"), []byte("0123456789"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	id, _, _ := environment.store.InsertScannedMedia(ctx, store.ScannedMedia{Path: "clip.mp4", Title: "Clip", VideoCodec: "h264"})
	environment.store.UpdateMedia(ctx, id, store.MediaUpdate{Title: "Clip"}, true)
	response, body := environment.client().sendForm("GET", "/media/"+itoa(id)+"/file", nil, map[string]string{"Range": "bytes=2-5"})
	if response.StatusCode != http.StatusPartialContent || body != "2345" || response.Header.Get("Content-Type") != "video/mp4" {
		t.Errorf("range request: %d %q %q", response.StatusCode, body, response.Header.Get("Content-Type"))
	}
}

func TestSubtitleFlow(t *testing.T) {
	environment := newEnvironment(t, nil)
	ctx := context.Background()
	firstClient, secondClient := environment.client(), environment.client()
	firstClient.register("alex")
	secondClient.register("sam")
	id, _, _ := environment.store.InsertScannedMedia(ctx, store.ScannedMedia{Path: "m.mp4", Title: "M", VideoCodec: "h264"})
	environment.store.UpdateMedia(ctx, id, store.MediaUpdate{Title: "M"}, true)
	subtitleID, _, _ := environment.store.AddSubtitle(ctx, store.NewSubtitle{MediaID: id, Language: "en", Label: "English", Cues: []subtitle.Cue{{StartMS: 1000, EndMS: 2000, Text: "Helo"}}})

	response, body := firstClient.sendForm("GET", "/media/"+itoa(id)+"/subtitle-report?track="+itoa(subtitleID)+"&t=1500", nil, nil)
	if response.StatusCode != 200 || !strings.Contains(body, "Helo") {
		t.Fatalf("report dialog: %d", response.StatusCode)
	}
	cue, _ := environment.store.CueAt(ctx, subtitleID, 1500)
	htmx := func(client *client) map[string]string {
		return map[string]string{"X-CSRF-Token": client.csrf, "HX-Request": "true"}
	}
	response, body = firstClient.sendForm("POST", "/cues/"+itoa(cue.ID)+"/changes", url.Values{"text": {"Hello"}}, htmx(firstClient))
	if response.StatusCode != 200 || !strings.Contains(body, "Correction submitted") {
		t.Fatalf("propose: %d %s", response.StatusCode, body)
	}
	changes, _ := environment.store.Changes(ctx, cue.ID, 0)
	response, body = secondClient.sendForm("POST", "/changes/"+itoa(changes[0].ID)+"/vote", url.Values{}, htmx(secondClient))
	if response.StatusCode != 200 || !strings.Contains(body, "Change applied") {
		t.Fatalf("vote: %d %s", response.StatusCode, body)
	}
	// Without htmx (scripts not loaded yet) the form still works and returns to the video.
	response, _ = firstClient.sendForm("POST", "/cues/"+itoa(cue.ID)+"/changes", url.Values{"text": {"Hello!"}, "csrf_token": {firstClient.csrf}}, nil)
	if response.StatusCode != http.StatusSeeOther || response.Header.Get("Location") != "/watch/"+itoa(id) {
		t.Errorf("plain form post: %d to %q", response.StatusCode, response.Header.Get("Location"))
	}
	_, vtt := environment.client().sendForm("GET", "/subtitles/"+itoa(subtitleID), nil, nil)
	if !strings.Contains(vtt, "WEBVTT") || !strings.Contains(vtt, "Hello") {
		t.Errorf("vtt after vote:\n%s", vtt)
	}
}

func TestRequireLogin(t *testing.T) {
	environment := newEnvironment(t, func(config *config.Config) { config.RequireLogin = true })
	response, _ := environment.client().sendForm("GET", "/", nil, nil)
	if response.StatusCode != http.StatusSeeOther || !strings.HasPrefix(response.Header.Get("Location"), "/login") {
		t.Errorf("anonymous home with RequireLogin: %d %q", response.StatusCode, response.Header.Get("Location"))
	}
	if response, _ := environment.client().sendForm("GET", "/login", nil, nil); response.StatusCode != 200 {
		t.Errorf("login page must stay reachable: %d", response.StatusCode)
	}
	client := environment.client()
	client.register("alex")
	if response, _ := client.sendForm("GET", "/", nil, nil); response.StatusCode != 200 {
		t.Errorf("signed in: %d", response.StatusCode)
	}
}

func TestPosterPathTraversal(t *testing.T) {
	environment := newEnvironment(t, nil)
	for _, path := range []string{"/posters/..%2fnexusplay.db", "/posters/.hidden", "/posters/%2e%2e"} {
		if response, _ := environment.client().sendForm("GET", path, nil, nil); response.StatusCode == 200 {
			t.Errorf("GET %s = 200, want refusal", path)
		}
	}
}

func TestSafeNext(t *testing.T) {
	for input, want := range map[string]string{
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
		if got := safeNext(input); got != want {
			t.Errorf("safeNext(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHealthz(t *testing.T) {
	environment := newEnvironment(t, nil)
	if response, body := environment.client().sendForm("GET", "/healthz", nil, nil); response.StatusCode != 200 || body != "ok\n" {
		t.Errorf("healthz: %d %q", response.StatusCode, body)
	}
}

// An upload over the size limit is answered as too large, never as a CSRF
// failure, whether the token comes in a form field or a header.
func TestOversizeUploadIsRejectedAsTooLarge(t *testing.T) {
	environment := newEnvironment(t, nil)
	admin := environment.client()
	admin.register("alex")
	id, _, _ := environment.store.InsertScannedMedia(context.Background(), store.ScannedMedia{Path: "m.mp4", Title: "M", VideoCodec: "h264"})

	upload := func(headerToken bool, size int) (*http.Response, string) {
		var buffer bytes.Buffer
		multipartWriter := multipart.NewWriter(&buffer)
		if !headerToken {
			multipartWriter.WriteField("csrf_token", admin.csrf)
		}
		multipartWriter.WriteField("language", "en")
		writer, _ := multipartWriter.CreateFormFile("file", "big.srt")
		writer.Write(bytes.Repeat([]byte("x"), size))
		multipartWriter.Close()
		headers := map[string]string{"Content-Type": multipartWriter.FormDataContentType()}
		if headerToken {
			headers["X-CSRF-Token"] = admin.csrf
		}
		request, _ := http.NewRequest("POST", environment.srv.URL+"/admin/media/"+itoa(id)+"/subtitles", &buffer)
		request.Header.Set("Origin", environment.srv.URL)
		for key, value := range headers {
			request.Header.Set(key, value)
		}
		response, err := admin.http.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, _ := io.ReadAll(response.Body)
		return response, string(body)
	}

	response, body := upload(false, maxUpload+1<<20)
	if response.StatusCode != http.StatusRequestEntityTooLarge || !strings.Contains(body, "too large") {
		t.Errorf("oversize upload with a form token: %d %q", response.StatusCode, body)
	}
	response, body = upload(true, maxUpload+1<<20)
	if response.StatusCode == http.StatusForbidden || !strings.Contains(body, "too large") {
		t.Errorf("oversize upload with a header token: %d, body mentions too large: %v", response.StatusCode, strings.Contains(body, "too large"))
	}
	// A normal upload still goes through (it is not a valid subtitle, so the
	// form comes back with that error, not a size or CSRF error).
	response, body = upload(false, 1000)
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(body, "not a valid SRT or WebVTT") {
		t.Errorf("small upload: %d", response.StatusCode)
	}
}

// Ordinary forms outside the admin area have a small limit.
func TestFormBodyLimit(t *testing.T) {
	environment := newEnvironment(t, nil)
	client := environment.client()
	client.register("alex")
	response, _ := client.sendForm("POST", "/me/progress", url.Values{"csrf_token": {client.csrf}, "pad": {strings.Repeat("x", maxFormBody)}}, nil)
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("oversize form: %d, want 413", response.StatusCode)
	}
}

// Members cannot act on unpublished media by guessing IDs.
func TestUnpublishedMediaRejectsProgressAndVotes(t *testing.T) {
	environment := newEnvironment(t, nil)
	ctx := context.Background()
	environment.client().register("admin")
	member := environment.client()
	member.register("sam")
	id, _, _ := environment.store.InsertScannedMedia(ctx, store.ScannedMedia{Path: "secret.mp4", Title: "Secret", VideoCodec: "h264"})
	subtitleID, _, _ := environment.store.AddSubtitle(ctx, store.NewSubtitle{MediaID: id, Language: "en", Label: "English", Cues: []subtitle.Cue{{StartMS: 0, EndMS: 1000, Text: "Helo"}}})
	cue, _ := environment.store.CueAt(ctx, subtitleID, 500)

	if response, _ := member.sendForm("POST", "/me/progress", url.Values{"csrf_token": {member.csrf}, "media_id": {itoa(id)}, "position": {"5"}}, nil); response.StatusCode != http.StatusNotFound {
		t.Errorf("progress on unpublished media: %d, want 404", response.StatusCode)
	}
	headers := map[string]string{"X-CSRF-Token": member.csrf, "HX-Request": "true"}
	if response, _ := member.sendForm("POST", "/cues/"+itoa(cue.ID)+"/changes", url.Values{"text": {"Hello"}}, headers); response.StatusCode != http.StatusNotFound {
		t.Errorf("correction on unpublished media: %d, want 404", response.StatusCode)
	}
	if changes, _ := environment.store.Changes(ctx, cue.ID, 0); len(changes) != 0 {
		t.Errorf("a correction was recorded on unpublished media: %+v", changes)
	}

	environment.store.UpdateMedia(ctx, id, store.MediaUpdate{Title: "Secret"}, true)
	for _, bad := range []string{"NaN", "Inf", "-1", "abc"} {
		if response, _ := member.sendForm("POST", "/me/progress", url.Values{"csrf_token": {member.csrf}, "media_id": {itoa(id)}, "position": {bad}}, nil); response.StatusCode != http.StatusBadRequest {
			t.Errorf("position %q: %d, want 400", bad, response.StatusCode)
		}
	}
	if response, _ := member.sendForm("POST", "/me/progress", url.Values{"csrf_token": {member.csrf}, "media_id": {itoa(id)}, "position": {"5"}}, nil); response.StatusCode != http.StatusNoContent {
		t.Errorf("progress on published media: %d, want 204", response.StatusCode)
	}
}

// The edit form's "Remove the current poster" box clears the poster; sent
// together with a new upload it is refused, and nothing changes.
func TestRemovePoster(t *testing.T) {
	environment := newEnvironment(t, nil)
	admin := environment.client()
	admin.register("alex")
	ctx := context.Background()
	id, _, _ := environment.store.InsertScannedMedia(ctx, store.ScannedMedia{Path: "m.mp4", Title: "M", VideoCodec: "h264"})
	if err := environment.store.UpdateMedia(ctx, id, store.MediaUpdate{Title: "M", Poster: "old.jpg"}, false); err != nil {
		t.Fatal(err)
	}
	png := []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")
	save := func(upload bool) (int, string) {
		var buffer bytes.Buffer
		multipartWriter := multipart.NewWriter(&buffer)
		multipartWriter.WriteField("csrf_token", admin.csrf)
		multipartWriter.WriteField("title", "M")
		multipartWriter.WriteField("remove_poster", "1")
		if upload {
			writer, _ := multipartWriter.CreateFormFile("poster", "new.png")
			writer.Write(png)
		}
		multipartWriter.Close()
		response, body := admin.send("POST", "/admin/media/"+itoa(id), &buffer, map[string]string{"Content-Type": multipartWriter.FormDataContentType()})
		return response.StatusCode, body
	}
	poster := func() string {
		media, err := environment.store.MediaByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return media.Poster
	}

	_, page := admin.sendForm("GET", "/admin/media/"+itoa(id), nil, nil)
	if !strings.Contains(page, `name="remove_poster"`) {
		t.Error("the edit page has no remove box although the media has a poster")
	}
	if status, body := save(true); status != http.StatusBadRequest || !strings.Contains(body, "not both") {
		t.Errorf("upload and remove: %d", status)
	}
	if got := poster(); got != "old.jpg" {
		t.Errorf("after a refused save, poster = %q", got)
	}
	if status, _ := save(false); status != http.StatusSeeOther {
		t.Errorf("remove: %d", status)
	}
	if got := poster(); got != "" {
		t.Errorf("after remove, poster = %q", got)
	}
	_, page = admin.sendForm("GET", "/admin/media/"+itoa(id), nil, nil)
	if strings.Contains(page, `name="remove_poster"`) {
		t.Error("the edit page offers to remove a poster that is not there")
	}
}

// Once NEXUSPLAY_MAX_ACCOUNTS accounts exist, the sign-up form is replaced
// by a notice and a direct POST is refused.
func TestRegistrationClosedAtLimit(t *testing.T) {
	environment := newEnvironment(t, func(config *config.Config) { config.MaxAccounts = 1 })
	environment.client().register("alex")
	client := environment.client()
	_, page := client.sendForm("GET", "/register", nil, nil)
	if !strings.Contains(page, "Registration is closed") || strings.Contains(page, `name="password"`) {
		t.Error("the register page still offers the form at the limit")
	}
	response, _ := client.sendForm("POST", "/register", url.Values{"nickname": {"sam"}, "email": {"sam@example.com"}, "password": {"long enough password"}}, nil)
	if response.StatusCode != http.StatusForbidden {
		t.Errorf("register past the limit: %d, want 403", response.StatusCode)
	}
}

// "Refresh metadata" without an API key explains itself on the edit page.
func TestRefreshMetadataWithoutKey(t *testing.T) {
	environment := newEnvironment(t, nil)
	admin := environment.client()
	admin.register("alex")
	id, _, _ := environment.store.InsertScannedMedia(context.Background(), store.ScannedMedia{Path: "m.mp4", Title: "M", VideoCodec: "h264"})
	response, body := admin.sendForm("POST", "/admin/media/"+itoa(id)+"/refresh-metadata", url.Values{"csrf_token": {admin.csrf}}, nil)
	if response.StatusCode != http.StatusBadRequest || !strings.Contains(body, "Metadata lookups are off") {
		t.Errorf("refresh without a key: %d", response.StatusCode)
	}
	if response, _ := admin.sendForm("POST", "/admin/media/9999/refresh-metadata", url.Values{"csrf_token": {admin.csrf}}, nil); response.StatusCode != http.StatusNotFound {
		t.Errorf("unknown media: %d", response.StatusCode)
	}
}

package web

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/stream"
)

func TestRespondErrorMapping(t *testing.T) {
	var logs bytes.Buffer
	s := responder{log: slog.New(slog.NewTextHandler(&logs, nil))}
	for _, tt := range []struct {
		name   string
		err    error
		status int
		logged bool
	}{
		{"not found", fmt.Errorf("media 7: %w", store.ErrNotFound), 404, false},
		{"duplicate", store.ErrDuplicate, 409, false},
		{"busy", stream.ErrBusy, 503, false},
		{"bad input", badRequest("Season must be a number."), 400, false},
		{"too large", &http.MaxBytesError{Limit: 20 << 20}, 413, false},
		{"database down", errors.New("disk I/O error"), 500, true},
	} {
		logs.Reset()
		rec := httptest.NewRecorder()
		s.respondError(rec, httptest.NewRequest("GET", "/x", nil).WithContext(context.Background()), tt.err)
		if rec.Code != tt.status {
			t.Errorf("%s: status %d, want %d", tt.name, rec.Code, tt.status)
		}
		if logged := strings.Contains(logs.String(), "request failed"); logged != tt.logged {
			t.Errorf("%s: logged = %v, want %v", tt.name, logged, tt.logged)
		}
	}
	rec := httptest.NewRecorder()
	s.respondError(rec, httptest.NewRequest("GET", "/x", nil), stream.ErrBusy)
	if rec.Header().Get("Retry-After") != "30" {
		t.Error("busy responses must say when to retry")
	}
	// A visitor who left gets nothing, and nothing is logged.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	logs.Reset()
	rec = httptest.NewRecorder()
	s.respondError(rec, httptest.NewRequest("GET", "/x", nil).WithContext(ctx), errors.New("write: broken pipe"))
	if rec.Body.Len() != 0 || logs.Len() != 0 {
		t.Errorf("cancelled request: body %q, log %q", rec.Body.String(), logs.String())
	}
}

func TestRespondFragmentOrPage(t *testing.T) {
	s := responder{log: slog.Default()}
	fragment := templ.Raw("<p>fragment</p>")
	page := func(p Page, c templ.Component) templ.Component {
		return templ.Join(templ.Raw("<html>"+p.Title+"</html>"), c)
	}
	for _, htmx := range []bool{true, false} {
		r := httptest.NewRequest("GET", "/collections/1", nil)
		if htmx {
			r.Header.Set("HX-Request", "true")
		}
		rec := httptest.NewRecorder()
		s.respond(rec, r, http.StatusOK, "Cosmos", fragment, page)
		gotPage := strings.Contains(rec.Body.String(), "<html>Cosmos</html>")
		if gotPage == htmx || !strings.Contains(rec.Body.String(), "fragment") {
			t.Errorf("htmx=%v: body %q", htmx, rec.Body.String())
		}
	}
}

func TestGroupBySeason(t *testing.T) {
	eps := []store.Media{{ID: 1, Season: 1}, {ID: 2, Season: 1}, {ID: 3, Season: 2}, {ID: 4, Season: 4}}
	seasons := groupBySeason(eps)
	var got []string
	for _, s := range seasons {
		got = append(got, fmt.Sprintf("%d:%d", s.Number, len(s.Episodes)))
	}
	if strings.Join(got, ",") != "1:2,2:1,4:1" {
		t.Errorf("seasons = %v", got)
	}
	if groupBySeason(nil) != nil {
		t.Error("no episodes should give no seasons")
	}
}

// A database failure is a server error, logged, never "Not found".
func TestDatabaseErrorIsNotNotFound(t *testing.T) {
	e := newEnv(t, nil)
	e.store.Close()
	resp, _ := e.client().do("GET", "/collections/1", nil, nil)
	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status %d, want 500", resp.StatusCode)
	}
}

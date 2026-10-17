// Package web serves NexusPlay's pages, API and media streams.
package web

import (
	"context"
	"crypto/subtle"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/AlexCristian/NexusPlay/internal/auth"
	"github.com/AlexCristian/NexusPlay/internal/config"
	"github.com/AlexCristian/NexusPlay/internal/convert"
	"github.com/AlexCristian/NexusPlay/internal/library"
	"github.com/AlexCristian/NexusPlay/internal/metadata"
	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/stream"
)

//go:embed static
var staticFiles embed.FS

const sessionCookie = "nexusplay_session"

const sessionLifetime = 30 * 24 * time.Hour

// Server holds the dependencies of every handler.
type Server struct {
	Cfg        config.Config
	Store      *store.Store
	Scanner    *library.Scanner
	Meta       *metadata.Client
	Transcodes *stream.Manager
	Converter  *convert.Converter // nil when conversion is turned off
	Log        *slog.Logger
}

type ctxKey int

const viewerKey ctxKey = 0

// Viewer is who is making a request. User is nil for anonymous visitors.
type Viewer struct {
	User *store.User
	CSRF string
}

// ID returns the user's ID, or 0 for anonymous visitors.
func (v Viewer) ID() int64 {
	if v.User == nil {
		return 0
	}
	return v.User.ID
}

// IsAdmin reports whether the viewer administers this install.
func (v Viewer) IsAdmin() bool { return v.User != nil && v.User.IsAdmin }

func viewer(r *http.Request) Viewer {
	v, _ := r.Context().Value(viewerKey).(Viewer)
	return v
}

// Handler returns the complete HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	static, _ := fs.Sub(staticFiles, "static") // cannot fail: "static" is a valid, embedded path
	mux.Handle("GET /static/", cacheForever(http.StripPrefix("/static/", http.FileServerFS(static))))
	mux.HandleFunc("GET /healthz", s.healthz)

	// Pages anyone may see unless NEXUSPLAY_REQUIRE_LOGIN is set.
	mux.Handle("GET /{$}", s.browse(s.home))
	mux.Handle("GET /search", s.browse(s.search))
	mux.Handle("GET /collections/{id}", s.browse(s.collection))
	mux.Handle("GET /watch/{id}", s.browse(s.watch))
	mux.Handle("GET /media/{id}/file", s.browse(s.mediaFile))
	mux.Handle("GET /media/{id}/hls/{file}", s.browse(s.hls))
	mux.Handle("GET /subtitles/{id}", s.browse(s.subtitleVTT))
	mux.Handle("GET /posters/{name}", s.browse(s.poster))

	// Accounts.
	mux.HandleFunc("GET /login", s.loginPage)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("GET /register", s.registerPage)
	mux.HandleFunc("POST /register", s.register)
	mux.HandleFunc("POST /logout", s.logout)

	// Signed-in features.
	mux.Handle("GET /me/resume", s.member(s.resumeList))
	mux.Handle("GET /me/notifications", s.member(s.notificationList))
	mux.Handle("POST /me/progress", s.member(s.saveProgress))
	mux.Handle("POST /collections/{id}/subscribe", s.member(s.toggleSubscription))
	mux.Handle("GET /media/{id}/subtitle-report", s.member(s.subtitleReport))
	mux.Handle("POST /cues/{id}/changes", s.member(s.proposeChange))
	mux.Handle("POST /changes/{id}/vote", s.member(s.voteChange))

	// Administration.
	mux.Handle("GET /admin", s.admin(s.controlPanel))
	mux.Handle("POST /admin/scan", s.admin(s.scanNow))
	mux.Handle("GET /admin/conversions", s.admin(s.conversionsPanel))
	mux.Handle("POST /admin/media/{id}/retry-conversion", s.admin(s.retryConversion))
	mux.Handle("GET /admin/media/{id}", s.admin(s.editMediaPage))
	mux.Handle("POST /admin/media/{id}", s.admin(s.saveMedia))
	mux.Handle("POST /admin/media/{id}/subtitles", s.admin(s.uploadSubtitle))
	mux.Handle("POST /admin/subtitles/{id}", s.admin(s.saveSubtitle))
	mux.Handle("POST /admin/subtitles/{id}/delete", s.admin(s.deleteSubtitle))
	mux.Handle("POST /admin/collections", s.admin(s.createCollection))
	mux.Handle("GET /admin/collections/{id}", s.admin(s.editCollectionPage))
	mux.Handle("POST /admin/collections/{id}", s.admin(s.saveCollection))
	mux.Handle("POST /admin/collections/{id}/delete", s.admin(s.deleteCollection))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { s.notFound(w, r) })

	var h http.Handler = mux
	h = s.csrf(h)
	h = s.session(h)
	h = securityHeaders(h)
	h = s.recoverer(h)
	h = s.logRequests(h)
	return h
}

// browse guards pages that are public unless the install requires login.
func (s *Server) browse(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Cfg.RequireLogin && viewer(r).User == nil {
			s.needLogin(w, r)
			return
		}
		h(w, r)
	})
}

// member guards features that need an account.
func (s *Server) member(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if viewer(r).User == nil {
			s.needLogin(w, r)
			return
		}
		h(w, r)
	})
}

// admin guards the control panel.
func (s *Server) admin(h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := viewer(r)
		if v.User == nil {
			s.needLogin(w, r)
			return
		}
		if !v.IsAdmin() {
			s.errorPage(w, r, http.StatusForbidden, "Access denied", "Only the administrator can do that.")
			return
		}
		h(w, r)
	})
}

func (s *Server) needLogin(w http.ResponseWriter, r *http.Request) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", "/login?next="+url.QueryEscape(currentURL(r)))
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	if r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/media/") && !strings.HasPrefix(r.URL.Path, "/subtitles/") {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(currentURL(r)), http.StatusSeeOther)
		return
	}
	http.Error(w, "login required", http.StatusUnauthorized)
}

func currentURL(r *http.Request) string {
	if isHTMX(r) {
		if u, err := url.Parse(r.Header.Get("HX-Current-URL")); err == nil && u.Path != "" {
			return u.RequestURI()
		}
		return "/"
	}
	return r.URL.RequestURI()
}

// session loads the logged-in user, if any.
func (s *Server) session(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v Viewer
		if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
			sess, u, err := s.Store.SessionUser(r.Context(), auth.HashToken(c.Value))
			if err == nil {
				v = Viewer{User: &u, CSRF: sess.CSRFToken}
			} else if !errors.Is(err, store.ErrNotFound) {
				s.Log.Error("loading session", "err", err)
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), viewerKey, v)))
	})
}

// Request body limits. Only the admin forms accept uploads (posters and
// subtitle files); everything else is a small form or a progress beacon.
const (
	maxUpload   = 20 << 20
	maxFormBody = 1 << 20
)

// bodyLimit returns the largest request body a route accepts.
func bodyLimit(r *http.Request) int64 {
	if strings.HasPrefix(r.URL.Path, "/admin/") {
		return maxUpload
	}
	return maxFormBody
}

// csrf protects state-changing requests in two layers:
//
//   - Origin check (every unsafe request): Origin, else Referer, must match the
//     Host (or X-Forwarded-Host behind a reverse proxy). When neither header is
//     present the request is accepted only if Sec-Fetch-Site is absent (a
//     non-browser client, which has no ambient cookies to abuse) or
//     "same-origin".
//   - Token (signed-in requests): the session's CSRF token in X-CSRF-Token
//     (htmx sends it from hx-headers on <body>, and player.js reads it from
//     there) or in the csrf_token form field (plain forms).
//
// /login and /register skip the token, because a visitor may not have a
// session yet or may hold a stale one; the Origin check still stops login CSRF.
//
// The body size limit is applied here, before anything reads the body, so an
// oversize upload is answered with 413 rather than looking like a missing
// token.
func (s *Server) csrf(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		limit := bodyLimit(r)
		r.Body = http.MaxBytesReader(w, r.Body, limit)
		if !sameOrigin(r) {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		v := viewer(r)
		if v.User == nil || r.URL.Path == "/login" || r.URL.Path == "/register" {
			next.ServeHTTP(w, r)
			return
		}
		token := r.Header.Get("X-CSRF-Token")
		if token == "" {
			if err := parseForm(r, limit); err != nil {
				writeFormError(w, err)
				return
			}
			token = r.FormValue("csrf_token")
		}
		if subtle.ConstantTimeCompare([]byte(token), []byte(v.CSRF)) != 1 {
			http.Error(w, "invalid or missing CSRF token; reload the page and try again", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// parseForm parses a URL-encoded or multipart body. Parsing twice is
// harmless: later calls return the already parsed form.
func parseForm(r *http.Request, limit int64) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return r.ParseMultipartForm(limit)
	}
	return r.ParseForm()
}

// writeFormError answers a body that could not be parsed: 413 when it was
// over the size limit, 400 otherwise.
func writeFormError(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		http.Error(w, fmt.Sprintf("The upload is too large. The limit is %d MB.", tooBig.Limit>>20), http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "The form could not be read.", http.StatusBadRequest)
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		// Non-browser clients send neither; browsers always send Origin on POST.
		return r.Header.Get("Sec-Fetch-Site") == "" || r.Header.Get("Sec-Fetch-Site") == "same-origin"
	}
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	return strings.EqualFold(u.Host, host)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; media-src 'self' blob:; "+
			"worker-src 'self' blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

func cacheForever(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=86400")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				s.Log.Error("panic", "err", rec, "path", r.URL.Path, "stack", string(debug.Stack()))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if strings.HasPrefix(r.URL.Path, "/static/") || strings.Contains(r.URL.Path, "/hls/") {
			return
		}
		status := sw.status
		if status == 0 {
			status = http.StatusOK
		}
		s.Log.Debug("request", "method", r.Method, "path", r.URL.Path, "status", status, "dur", time.Since(start))
	})
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.Store.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok\n"))
}

// render writes a component, or the whole page around it for a normal request.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "HX-Request")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		s.Log.Error("render", "err", err, "path", r.URL.Path)
	}
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Error("request failed", "path", r.URL.Path, "err", err)
	s.errorPage(w, r, http.StatusInternalServerError, "Something went wrong", "The server hit an error. Please try again.")
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.errorPage(w, r, http.StatusNotFound, "Not found", "That page or item does not exist.")
}

func (s *Server) errorPage(w http.ResponseWriter, r *http.Request, status int, title, msg string) {
	if isHTMX(r) {
		s.render(w, r, status, alert("error", msg))
		return
	}
	s.render(w, r, status, messagePage(s.page(r, title), title, msg))
}

func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Boosted") != "true"
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

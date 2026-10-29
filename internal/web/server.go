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
	"strings"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/auth"
	"github.com/AlexCristian/NexusPlay/internal/config"
	"github.com/AlexCristian/NexusPlay/internal/library"
	"github.com/AlexCristian/NexusPlay/internal/metadata"
	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/stream"
)

//go:embed static
var staticFiles embed.FS

const sessionCookie = "nexusplay_session"

// Server is the web front end. Its fields are the dependencies; Handler
// hands each feature group (library, playback, subtitles, accounts, admin)
// only the ones it uses.
type Server struct {
	Config     config.Config
	Store      *store.Store
	Scanner    *library.Scanner
	Metadata   *metadata.Client
	Transcodes *stream.Manager
	Accounts   *auth.Accounts
	Sessions   *auth.Sessions
	Converter  Conversions
	Log        *slog.Logger

	responder // set by Handler; used by the middleware
}

type ctxKey int

const viewerKey ctxKey = 0

// viewer is who made the request; the zero Viewer for a visitor who is not
// logged in. The session middleware sets it.
func viewer(r *http.Request) Viewer {
	viewer, _ := r.Context().Value(viewerKey).(Viewer)
	return viewer
}

// Handler returns the complete HTTP handler.
func (server *Server) Handler() http.Handler {
	server.responder = responder{log: server.Log}
	lib := &libraryHandlers{responder: server.responder, store: server.Store}
	play := &playbackHandlers{responder: server.responder, store: server.Store, transcodes: server.Transcodes,
		mediaDirectory: server.Config.MediaDirectory, posterDirectory: server.Config.PosterDirectory()}
	subtitleRoutes := &subtitleHandlers{responder: server.responder, store: server.Store, votesRequired: server.Config.VotesRequired}
	acct := &accountHandlers{responder: server.responder, accounts: server.Accounts, sessions: server.Sessions,
		secureCookies: server.Config.SecureCookies}
	adm := &adminHandlers{responder: server.responder, store: server.Store, metadata: server.Metadata, scanner: server.Scanner, refresher: server.Scanner,
		conversions: server.Converter, posterDirectory: server.Config.PosterDirectory()}

	mux := http.NewServeMux()

	static, _ := fs.Sub(staticFiles, "static") // cannot fail: "static" is a valid, embedded path
	mux.Handle("GET /static/", cacheStatic(http.StripPrefix("/static/", http.FileServerFS(static))))
	mux.HandleFunc("GET /healthz", server.healthz)

	// Pages anyone may see unless NEXUSPLAY_REQUIRE_LOGIN is set.
	mux.Handle("GET /{$}", server.browse(server.handle(lib.home)))
	mux.Handle("GET /search", server.browse(server.handle(lib.search)))
	mux.Handle("GET /collections/{id}", server.browse(server.handle(lib.collection)))
	mux.Handle("GET /watch/{id}", server.browse(server.handle(play.watch)))
	mux.Handle("GET /media/{id}/file", server.browse(server.handle(play.mediaFile)))
	mux.Handle("GET /media/{id}/hls/index.m3u8", server.browse(server.handle(play.hlsPlaylist)))
	mux.Handle("GET /media/{id}/hls/{segment}", server.browse(server.handle(play.hlsSegment)))
	mux.Handle("GET /subtitles/{id}", server.browse(server.handle(subtitleRoutes.subtitleVTT)))
	mux.Handle("GET /posters/{name}", server.browse(server.handle(play.poster)))

	// Accounts.
	mux.HandleFunc("GET /login", server.handle(acct.loginPage))
	mux.HandleFunc("POST /login", server.handle(acct.login))
	mux.HandleFunc("GET /register", server.handle(acct.registerPage))
	mux.HandleFunc("POST /register", server.handle(acct.register))
	mux.HandleFunc("POST /logout", server.handle(acct.logout))

	// Signed-in features.
	mux.Handle("GET /me/resume", server.member(server.handle(lib.resumeList)))
	mux.Handle("GET /me/notifications", server.member(server.handle(lib.notificationList)))
	mux.Handle("POST /me/progress", server.member(server.handle(play.saveProgress)))
	mux.Handle("POST /collections/{id}/subscribe", server.member(server.handle(lib.toggleSubscription)))
	mux.Handle("GET /media/{id}/subtitle-report", server.member(server.handle(subtitleRoutes.subtitleReport)))
	mux.Handle("POST /cues/{id}/changes", server.member(server.handle(subtitleRoutes.proposeChange)))
	mux.Handle("POST /changes/{id}/vote", server.member(server.handle(subtitleRoutes.voteChange)))

	// Administration.
	mux.Handle("GET /admin", server.admin(server.handle(adm.controlPanel)))
	mux.Handle("POST /admin/scan", server.admin(server.handle(adm.scanNow)))
	mux.Handle("GET /admin/conversions", server.admin(server.handle(adm.conversionsPanel)))
	mux.Handle("POST /admin/media/{id}/retry-conversion", server.admin(server.handle(adm.retryConversion)))
	mux.Handle("GET /admin/media/{id}", server.admin(server.handle(adm.editMediaPage)))
	mux.Handle("POST /admin/media/{id}", server.admin(server.handle(adm.saveMedia)))
	mux.Handle("POST /admin/media/{id}/refresh-metadata", server.admin(server.handle(adm.refreshMetadata)))
	mux.Handle("POST /admin/media/{id}/subtitles", server.admin(server.handle(adm.uploadSubtitle)))
	mux.Handle("POST /admin/subtitles/{id}", server.admin(server.handle(adm.saveSubtitle)))
	mux.Handle("POST /admin/subtitles/{id}/delete", server.admin(server.handle(adm.deleteSubtitle)))
	mux.Handle("POST /admin/collections", server.admin(server.handle(adm.createCollection)))
	mux.Handle("GET /admin/collections/{id}", server.admin(server.handle(adm.editCollectionPage)))
	mux.Handle("POST /admin/collections/{id}", server.admin(server.handle(adm.saveCollection)))
	mux.Handle("POST /admin/collections/{id}/delete", server.admin(server.handle(adm.deleteCollection)))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { server.notFound(w, r) })

	// Outermost first: each request is logged, recovered from panics, given
	// security headers, then its session, then checked for CSRF.
	return chain(mux, server.logRequests, server.recoverer, securityHeaders, server.session, server.csrf)
}

// chain wraps h in middleware, the first outermost.
func chain(handler http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}

// browse guards pages that are public unless the install requires login.
func (server *Server) browse(handler http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if server.Config.RequireLogin && viewer(r).User == nil {
			server.needLogin(w, r)
			return
		}
		handler(w, r)
	})
}

// member guards features that need an account.
func (server *Server) member(handler http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if viewer(r).User == nil {
			server.needLogin(w, r)
			return
		}
		handler(w, r)
	})
}

// admin guards the control panel.
func (server *Server) admin(handler http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		currentViewer := viewer(r)
		if currentViewer.User == nil {
			server.needLogin(w, r)
			return
		}
		if !currentViewer.IsAdmin() {
			server.errorPage(w, r, http.StatusForbidden, "Access denied", "Only the administrator can do that.")
			return
		}
		handler(w, r)
	})
}

// needLogin answers a request that needs an account. htmx gets 401 with
// HX-Redirect to the login page; a page load is redirected there and comes
// back afterwards; media, subtitle and other requests get a plain 401,
// since a redirect to an HTML page would only confuse the player.
func (server *Server) needLogin(w http.ResponseWriter, r *http.Request) {
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

// currentURL is the page the user is on: for htmx, the page that made the
// request (HX-Current-URL), not the fragment's URL.
func currentURL(r *http.Request) string {
	if isHTMX(r) {
		if parsedURL, err := url.Parse(r.Header.Get("HX-Current-URL")); err == nil && parsedURL.Path != "" {
			return parsedURL.RequestURI()
		}
		return "/"
	}
	return r.URL.RequestURI()
}

// session loads the logged-in user, if any, into the request context.
func (server *Server) session(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var viewer Viewer
		if cookie, err := r.Cookie(sessionCookie); err == nil {
			viewer, err = server.Sessions.Lookup(r.Context(), cookie.Value)
			if err != nil && !errors.Is(err, auth.ErrNoSession) {
				server.Log.Error("loading session", "err", err)
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), viewerKey, viewer)))
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
func (server *Server) csrf(next http.Handler) http.Handler {
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
		currentViewer := viewer(r)
		if currentViewer.User == nil || r.URL.Path == "/login" || r.URL.Path == "/register" {
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
		if subtle.ConstantTimeCompare([]byte(token), []byte(currentViewer.CSRF)) != 1 {
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

// sameOrigin is the Origin check described on csrf.
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	if origin == "" {
		// Non-browser clients send neither; browsers always send Origin on POST.
		return r.Header.Get("Sec-Fetch-Site") == "" || r.Header.Get("Sec-Fetch-Site") == "same-origin"
	}
	parsedURL, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := r.Host
	if fwd := r.Header.Get("X-Forwarded-Host"); fwd != "" {
		host = fwd
	}
	return strings.EqualFold(parsedURL.Host, host)
}

// securityHeaders sets headers for every response. The Content-Security-
// Policy allows only this site's own scripts and styles; blob: media and
// workers are for hls.js, which feeds the video element through
// MediaSource.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := w.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("X-Frame-Options", "DENY")
		header.Set("Referrer-Policy", "same-origin")
		header.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; media-src 'self' blob:; "+
			"worker-src 'self' blob:; style-src 'self' 'unsafe-inline'; script-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		next.ServeHTTP(w, r)
	})
}

// recoverer turns a panic in a handler into a logged 500 instead of a
// dropped connection. http.ErrAbortHandler is re-raised: it is how a
// handler deliberately aborts a response.
func (server *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				server.Log.Error("panic", "err", rec, "path", r.URL.Path, "stack", string(debug.Stack()))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// statusWriter records the status code for the request log.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusWriter) WriteHeader(code int) {
	if writer.status == 0 {
		writer.status = code
	}
	writer.ResponseWriter.WriteHeader(code)
}

func (writer *statusWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

// logRequests logs each request at debug level (NEXUSPLAY_DEBUG), leaving
// out static files and HLS segments, which would drown the rest.
func (server *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		recorder := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(recorder, r)
		if strings.HasPrefix(r.URL.Path, "/static/") || strings.Contains(r.URL.Path, "/hls/") {
			return
		}
		status := recorder.status
		if status == 0 {
			status = http.StatusOK
		}
		server.Log.Debug("request", "method", r.Method, "path", r.URL.Path, "status", status, "dur", time.Since(start))
	})
}

// healthz handles GET /healthz for the container health check: 200 while
// the database answers, else 503.
func (server *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := server.Store.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok\n"))
}

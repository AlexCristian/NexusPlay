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

// [staticFiles] holds the stylesheets, scripts, fonts and images in static/,
// built into the program.
//
//go:embed static
var staticFiles embed.FS

// [healthCheckTimeout] is how long /healthz waits for the database.
const healthCheckTimeout = 2 * time.Second

// [sessionCookie] is the name of the cookie that carries the session token.
const sessionCookie = "nexusplay_session"

// [Server] is the web front end. Its fields are the dependencies; set them,
// then call [Server.Handler], which hands each feature group (library,
// playback, subtitles, accounts, admin) only the ones it uses.
type Server struct {
	// [Config] holds the settings: folders, limits and cookie security.
	Config config.Config
	// [Store] is the database.
	Store *store.Store
	// [Scanner] is the library scanner, for "Scan now" and "Refresh metadata".
	Scanner *library.Scanner
	// [Metadata] looks titles up on TMDB for the control panel.
	Metadata *metadata.Client
	// [Transcodes] runs the HLS transcodes for playback.
	Transcodes *stream.Manager
	// [Accounts] checks credentials and creates accounts.
	Accounts *auth.Accounts
	// [Sessions] starts, finds and ends sign-in sessions.
	Sessions *auth.Sessions
	// [Converter] is the MP4 converter, or [convert.Disabled].
	Converter Conversions
	// [Log] records errors, and at debug level every request except static
	// files and HLS.
	Log *slog.Logger

	// [responder] writes the middleware's error responses; [Server.Handler] sets
	// it.
	responder
}

// [contextKey] is the type of this package's keys for values stored in a
// request's context, so they cannot clash with other packages' keys.
type contextKey int

// [viewerKey] is the context key under which [Server.session] stores the
// [Viewer].
const viewerKey contextKey = 0

// [viewer] tells who made a request. [Server.session] sets it for every
// request.
//
// Parameters:
//   - [r]: the request.
//
// Returns:
//   - [Viewer]: the signed-in viewer, or the zero [Viewer] for a visitor
//     who is not signed in.
func viewer(r *http.Request) Viewer {
	viewer, _ := r.Context().Value(viewerKey).(Viewer)
	return viewer
}

// [Handler] builds the complete HTTP handler: the route table, with each
// route's access rule ([Server.browse], [Server.member] or [Server.admin]),
// wrapped in the middleware. Call it once, after setting the fields.
//
// Returns:
//   - [http.Handler]: the handler to serve.
func (server *Server) Handler() http.Handler {
	server.responder = responder{log: server.Log}
	libraryRoutes := &libraryHandlers{responder: server.responder, store: server.Store}
	playbackRoutes := &playbackHandlers{responder: server.responder, store: server.Store, transcodes: server.Transcodes,
		mediaDirectory: server.Config.MediaDirectory, posterDirectory: server.Config.PosterDirectory()}
	subtitleRoutes := &subtitleHandlers{responder: server.responder, store: server.Store, votesRequired: server.Config.VotesRequired}
	accountRoutes := &accountHandlers{responder: server.responder, accounts: server.Accounts, sessions: server.Sessions,
		secureCookies: server.Config.SecureCookies}
	adminRoutes := &adminHandlers{responder: server.responder, store: server.Store, metadata: server.Metadata, scanner: server.Scanner, refresher: server.Scanner,
		conversions: server.Converter, posterDirectory: server.Config.PosterDirectory()}

	mux := http.NewServeMux()

	static, _ := fs.Sub(staticFiles, "static") // cannot fail: "static" is a valid, embedded path
	mux.Handle("GET /static/", cacheStatic(http.StripPrefix("/static/", http.FileServerFS(static))))
	mux.HandleFunc("GET /healthz", server.healthz)

	// Pages anyone may see unless NEXUSPLAY_REQUIRE_LOGIN is set.
	mux.Handle("GET /{$}", server.browse(server.handle(libraryRoutes.home)))
	mux.Handle("GET /search", server.browse(server.handle(libraryRoutes.search)))
	mux.Handle("GET /collections/{id}", server.browse(server.handle(libraryRoutes.collection)))
	mux.Handle("GET /watch/{id}", server.browse(server.handle(playbackRoutes.watch)))
	mux.Handle("GET /media/{id}/file", server.browse(server.handle(playbackRoutes.mediaFile)))
	mux.Handle("GET /media/{id}/hls/index.m3u8", server.browse(server.handle(playbackRoutes.hlsPlaylist)))
	mux.Handle("GET /media/{id}/hls/{segment}", server.browse(server.handle(playbackRoutes.hlsSegment)))
	mux.Handle("GET /subtitles/{id}", server.browse(server.handle(subtitleRoutes.subtitleVTT)))
	mux.Handle("GET /posters/{name}", server.browse(server.handle(playbackRoutes.poster)))

	// Signing in and out.
	mux.HandleFunc("GET /login", server.handle(accountRoutes.loginPage))
	mux.HandleFunc("POST /login", server.handle(accountRoutes.login))
	mux.HandleFunc("GET /register", server.handle(accountRoutes.registerPage))
	mux.HandleFunc("POST /register", server.handle(accountRoutes.register))
	mux.HandleFunc("POST /logout", server.handle(accountRoutes.logout))

	// Signed-in features.
	mux.Handle("GET /me/resume", server.member(server.handle(libraryRoutes.resumeList)))
	mux.Handle("GET /me/notifications", server.member(server.handle(libraryRoutes.notificationList)))
	mux.Handle("POST /me/progress", server.member(server.handle(playbackRoutes.saveProgress)))
	mux.Handle("POST /collections/{id}/subscribe", server.member(server.handle(libraryRoutes.toggleSubscription)))
	mux.Handle("GET /media/{id}/subtitle-report", server.member(server.handle(subtitleRoutes.subtitleReport)))
	mux.Handle("POST /cues/{id}/changes", server.member(server.handle(subtitleRoutes.proposeChange)))
	mux.Handle("POST /changes/{id}/vote", server.member(server.handle(subtitleRoutes.voteChange)))

	// Administration.
	mux.Handle("GET /admin", server.admin(server.handle(adminRoutes.controlPanel)))
	mux.Handle("POST /admin/scan", server.admin(server.handle(adminRoutes.scanNow)))
	mux.Handle("GET /admin/conversions", server.admin(server.handle(adminRoutes.conversionsPanel)))
	mux.Handle("POST /admin/media/{id}/retry-conversion", server.admin(server.handle(adminRoutes.retryConversion)))
	mux.Handle("GET /admin/media/{id}", server.admin(server.handle(adminRoutes.editMediaPage)))
	mux.Handle("POST /admin/media/{id}", server.admin(server.handle(adminRoutes.saveMedia)))
	mux.Handle("POST /admin/media/{id}/refresh-metadata", server.admin(server.handle(adminRoutes.refreshMetadata)))
	mux.Handle("POST /admin/media/{id}/subtitles", server.admin(server.handle(adminRoutes.uploadSubtitle)))
	mux.Handle("POST /admin/subtitles/{id}", server.admin(server.handle(adminRoutes.saveSubtitle)))
	mux.Handle("POST /admin/subtitles/{id}/delete", server.admin(server.handle(adminRoutes.deleteSubtitle)))
	mux.Handle("POST /admin/collections", server.admin(server.handle(adminRoutes.createCollection)))
	mux.Handle("GET /admin/collections/{id}", server.admin(server.handle(adminRoutes.editCollectionPage)))
	mux.Handle("POST /admin/collections/{id}", server.admin(server.handle(adminRoutes.saveCollection)))
	mux.Handle("POST /admin/collections/{id}/delete", server.admin(server.handle(adminRoutes.deleteCollection)))

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { server.notFound(w, r) })

	// Outermost first: each request is logged, recovered from panics, given
	// security headers, then its session, then checked for CSRF.
	return chain(mux, server.logRequests, server.recoverer, securityHeaders, server.session, server.csrf)
}

// [chain] wraps a handler in middleware.
//
// Parameters:
//   - [handler]: the innermost handler.
//   - [middleware]: the wrappers, outermost first.
//
// Returns:
//   - [http.Handler]: [handler] wrapped in every one of [middleware].
func chain(handler http.Handler, middleware ...func(http.Handler) http.Handler) http.Handler {
	for i := len(middleware) - 1; i >= 0; i-- {
		handler = middleware[i](handler)
	}
	return handler
}

// [browse] guards pages that are public unless the install requires
// sign-in ([config.Config.RequireLogin]).
//
// Parameters:
//   - [handler]: the page's handler.
//
// Returns:
//   - [http.Handler]: a handler that runs [handler], or answers as
//     [Server.needLogin] describes when sign-in is required and missing.
func (server *Server) browse(handler http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if server.Config.RequireLogin && viewer(r).User == nil {
			server.needLogin(w, r)
			return
		}
		handler(w, r)
	})
}

// [member] guards features that need an account.
//
// Parameters:
//   - [handler]: the feature's handler.
//
// Returns:
//   - [http.Handler]: a handler that runs [handler] for a signed-in viewer,
//     and otherwise answers as [Server.needLogin] describes.
func (server *Server) member(handler http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if viewer(r).User == nil {
			server.needLogin(w, r)
			return
		}
		handler(w, r)
	})
}

// [admin] guards the control panel.
//
// Parameters:
//   - [handler]: the control panel's handler.
//
// Returns:
//   - [http.Handler]: a handler that runs [handler] for the administrator,
//     answers as [Server.needLogin] describes for a visitor who is not
//     signed in, and with 403 for anyone else.
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

// [needLogin] answers a request that needs an account. htmx gets 401 with
// HX-Redirect to the sign-in page; a page load is redirected there and comes
// back afterwards; media (/media/) and subtitle (/subtitles/) requests and
// anything that is not a GET get a plain 401, since a redirect to an HTML
// page would only confuse the player. Poster requests are redirected like
// pages.
//
// Parameters:
//   - [w]: where the answer is written.
//   - [r]: the request.
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

// [currentURL] gives the address of the page the user is on: for htmx, the
// page that made the request (HX-Current-URL), not the fragment's address.
//
// Parameters:
//   - [r]: the request.
//
// Returns:
//   - [string]: the page's path and query, or "/" for an htmx request
//     without a usable HX-Current-URL.
func currentURL(r *http.Request) string {
	if isHTMX(r) {
		if parsedURL, err := url.Parse(r.Header.Get("HX-Current-URL")); err == nil && parsedURL.Path != "" {
			return parsedURL.RequestURI()
		}
		return "/"
	}
	return r.URL.RequestURI()
}

// [session] is the middleware that finds the signed-in viewer from the
// session cookie and stores them in the request's context, for [viewer]. A
// missing, unknown or expired session means an anonymous viewer; a database
// error is logged and also treated as anonymous.
//
// Parameters:
//   - [next]: the rest of the chain.
//
// Returns:
//   - [http.Handler]: [next] with the viewer added to each request.
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
	// [maxUpload] is the largest request body the control panel accepts, in
	// bytes (20 MiB), for poster and subtitle uploads.
	maxUpload = 20 << 20
	// [maxFormBody] is the largest request body every other route accepts, in
	// bytes (1 MiB): small forms and progress beacons.
	maxFormBody = 1 << 20
)

// [bodyLimit] gives the largest request body a route accepts.
//
// Parameters:
//   - [r]: the request.
//
// Returns:
//   - [int64]: [maxUpload] under "/admin/", [maxFormBody] elsewhere.
func bodyLimit(r *http.Request) int64 {
	if strings.HasPrefix(r.URL.Path, "/admin/") {
		return maxUpload
	}
	return maxFormBody
}

// [csrf] is the middleware that protects state-changing requests (anything
// but GET, HEAD and OPTIONS) in two layers:
//
//   - Origin check, for every such request: Origin, else Referer, must match
//     the Host (or X-Forwarded-Host behind a reverse proxy). When neither is
//     present the request is accepted only if Sec-Fetch-Site is absent (a
//     client that is not a browser, which has no cookies to abuse) or
//     "same-origin". Failing gets 403.
//   - Token, for signed-in requests: the session's CSRF token in the
//     X-CSRF-Token header (htmx sends it from hx-headers on <body>) or in
//     the "csrf_token" form field (plain forms, and player.js, which reads
//     the token from hx-headers because sendBeacon cannot set headers). A missing or wrong token gets 403.
//
// /login and /register skip the token, because a visitor may not have a
// session yet or may hold a stale one; the Origin check still stops login
// CSRF.
//
// The body size limit ([bodyLimit]) is applied here, before anything reads
// the body, so an oversize upload is answered with 413 rather than looking
// like a missing token.
//
// Parameters:
//   - [next]: the rest of the chain.
//
// Returns:
//   - [http.Handler]: [next] behind the checks.
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

// [parseForm] parses a request's URL-encoded or multipart form body. Parsing
// twice is harmless: later calls keep the form already parsed.
//
// Parameters:
//   - [r]: the request.
//   - [limit]: for a multipart body, how many bytes of it may be kept in
//     memory; the rest goes to temporary files.
//
// Returns:
//   - [error]: [nil] on success, or the parse error, which wraps an
//     [*http.MaxBytesError] when the body was over its limit.
func parseForm(r *http.Request, limit int64) error {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		return r.ParseMultipartForm(limit)
	}
	return r.ParseForm()
}

// [writeFormError] answers a request whose body could not be parsed, as
// plain text.
//
// Parameters:
//   - [w]: where the answer is written.
//   - [err]: the parse error.
func writeFormError(w http.ResponseWriter, err error) {
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		http.Error(w, fmt.Sprintf("The upload is too large. The limit is %d MB.", tooBig.Limit>>20), http.StatusRequestEntityTooLarge)
		return
	}
	http.Error(w, "The form could not be read.", http.StatusBadRequest)
}

// [sameOrigin] is the Origin check described at [Server.csrf].
//
// Parameters:
//   - [r]: the request.
//
// Returns:
//   - [bool]: [true] if the request comes from this site, or from a client
//     that is not a browser; [false] otherwise, including for an Origin or
//     Referer that cannot be parsed.
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
	if forwardedHost := r.Header.Get("X-Forwarded-Host"); forwardedHost != "" {
		host = forwardedHost
	}
	return strings.EqualFold(parsedURL.Host, host)
}

// [securityHeaders] is the middleware that sets security headers on every
// response: no MIME sniffing, no framing, the referrer only for this site,
// and a Content-Security-Policy that allows only this site's own scripts
// and styles (inline styles too). blob: media and workers are for hls.js,
// which feeds the video element through MediaSource.
//
// Parameters:
//   - [next]: the rest of the chain.
//
// Returns:
//   - [http.Handler]: [next] with the headers set.
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

// [recoverer] is the middleware that turns a panic in a handler into a
// logged 500 instead of a dropped connection. [http.ErrAbortHandler] is
// raised again, since it is how a handler deliberately aborts a response.
//
// Parameters:
//   - [next]: the rest of the chain.
//
// Returns:
//   - [http.Handler]: [next] with panics caught.
func (server *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				if recovered == http.ErrAbortHandler {
					panic(recovered)
				}
				server.Log.Error("panic", "err", recovered, "path", r.URL.Path, "stack", string(debug.Stack()))
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// [statusWriter] records a response's status code for the request log.
type statusWriter struct {
	// [ResponseWriter] is the response being written.
	http.ResponseWriter
	// [status] is the first status code written, or 0 if none was written
	// explicitly.
	status int
}

// [WriteHeader] records the first status code and writes it.
//
// Parameters:
//   - [code]: the status code.
func (writer *statusWriter) WriteHeader(code int) {
	if writer.status == 0 {
		writer.status = code
	}
	writer.ResponseWriter.WriteHeader(code)
}

// [Unwrap] gives the wrapped response, so [http.ResponseController] can
// reach its optional features.
//
// Returns:
//   - [http.ResponseWriter]: the response being written.
func (writer *statusWriter) Unwrap() http.ResponseWriter { return writer.ResponseWriter }

// [logRequests] is the middleware that logs each request at debug level
// (NEXUSPLAY_DEBUG) with its status and duration, leaving out static files
// and every HLS request (playlists and segments), which would drown the
// rest.
//
// Parameters:
//   - [next]: the rest of the chain.
//
// Returns:
//   - [http.Handler]: [next] with logging.
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

// [healthz] handles GET /healthz for the container health check: "ok" with
// 200 while the database answers within 2 seconds, otherwise 503.
//
// Parameters:
//   - [w]: where the answer is written.
//   - [r]: the request.
func (server *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthCheckTimeout)
	defer cancel()
	if err := server.Store.Ping(ctx); err != nil {
		http.Error(w, "database unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("ok\n"))
}

package web

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/a-h/templ"

	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/stream"
)

// handlerFunc is an HTTP handler that returns an error instead of writing
// an error response. handle turns the error into the right response, so
// every handler maps errors the same way.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

// handle adapts a handlerFunc for the router.
func (s *Server) handle(fn handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := fn(w, r); err != nil {
			s.respondError(w, r, err)
		}
	}
}

// statusError asks for a specific error page, for the few cases the
// mapping below does not cover.
type statusError struct {
	status int
	title  string
	msg    string
}

func (e *statusError) Error() string { return e.title + ": " + e.msg }

// badRequest is input the user can fix; msg is shown to them.
func badRequest(msg string) error {
	return &statusError{status: http.StatusBadRequest, title: "Bad request", msg: msg}
}

// respondError is the one place errors become responses:
//
//	client went away          nothing (and nothing logged)
//	*statusError              its status, title and message
//	*http.MaxBytesError       413, the upload is too large
//	store.ErrNotFound         404
//	store.ErrDuplicate        409
//	stream.ErrBusy            503 with Retry-After: 30
//	anything else             500, logged with the request
func (s *Server) respondError(w http.ResponseWriter, r *http.Request, err error) {
	var se *statusError
	var tooBig *http.MaxBytesError
	switch {
	case r.Context().Err() != nil:
	case errors.As(err, &se):
		s.errorPage(w, r, se.status, se.title, se.msg)
	case errors.As(err, &tooBig):
		s.errorPage(w, r, http.StatusRequestEntityTooLarge, "Too large",
			"The upload is too large. The limit is "+strconv.FormatInt(tooBig.Limit>>20, 10)+" MB.")
	case errors.Is(err, store.ErrNotFound):
		s.notFound(w, r)
	case errors.Is(err, store.ErrDuplicate):
		s.errorPage(w, r, http.StatusConflict, "Already exists", "Something with that name already exists.")
	case errors.Is(err, stream.ErrBusy):
		w.Header().Set("Retry-After", "30")
		http.Error(w, "The server is busy converting other videos. Try again in a minute.", http.StatusServiceUnavailable)
	default:
		s.serverError(w, r, err)
	}
}

// respond writes fragment alone for an htmx request (which swaps it into the
// current page) and wraps it in page otherwise, so every dialog also works
// as a standalone URL.
func (s *Server) respond(w http.ResponseWriter, r *http.Request, status int, title string,
	fragment templ.Component, page func(Page, templ.Component) templ.Component) error {
	if isHTMX(r) {
		s.render(w, r, status, fragment)
	} else {
		s.render(w, r, status, page(s.page(r, title), fragment))
	}
	return nil
}

// render writes a component as an HTML response.
func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "HX-Request")
	w.WriteHeader(status)
	if err := c.Render(r.Context(), w); err != nil {
		s.Log.Error("render", "err", err, "path", r.URL.Path)
	}
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	s.Log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	s.errorPage(w, r, http.StatusInternalServerError, "Something went wrong", "The server hit an error. Please try again.")
}

func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	s.errorPage(w, r, http.StatusNotFound, "Not found", "That page or item does not exist.")
}

// errorPage shows an error as an alert in the dialog or page that asked
// (htmx), or as a full page.
func (s *Server) errorPage(w http.ResponseWriter, r *http.Request, status int, title, msg string) {
	if isHTMX(r) {
		s.render(w, r, status, alert("error", msg))
		return
	}
	s.render(w, r, status, messagePage(s.page(r, title), title, msg))
}

// isHTMX reports whether htmx sent the request to swap part of a page.
// Boosted requests replace the whole page, so they count as normal ones.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Boosted") != "true"
}

// pathID reads the {id} in the URL. A malformed ID is store.ErrNotFound: to
// the visitor it is a page that does not exist.
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, store.ErrNotFound
	}
	return id, nil
}

// pathIDOK is the old form of pathID, used by handlers not yet converted
// to return errors. It goes away with them.
func pathIDOK(r *http.Request) (int64, bool) {
	id, err := pathID(r)
	return id, err == nil
}

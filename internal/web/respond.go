package web

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/a-h/templ"

	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/stream"
)

// [handlerFunc] is an HTTP handler that returns an error instead of writing
// an error response. [responder.handle] turns the error into the right
// response, so every handler maps errors the same way.
type handlerFunc func(w http.ResponseWriter, r *http.Request) error

// [responder] writes responses: pages, fragments and errors. Every handler
// group embeds one, so they all answer the same way.
type responder struct {
	// [log] records render failures and unexpected errors.
	log *slog.Logger
}

// [handle] adapts a [handlerFunc] for the router.
//
// Parameters:
//   - [handler]: the handler.
//
// Returns:
//   - [http.HandlerFunc]: a handler that runs [handler] and, if it returns
//     an error, answers with [responder.respondError].
func (responder responder) handle(handler handlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := handler(w, r); err != nil {
			responder.respondError(w, r, err)
		}
	}
}

// [statusError] asks for a specific error page, for the few cases the
// mapping in [responder.respondError] does not cover.
type statusError struct {
	// [status] is the HTTP status code, such as 400.
	status int
	// [title] is the error page's title, such as "Bad request".
	title string
	// [message] is the sentence shown to the user.
	message string
}

// [Error] describes the error for logs.
//
// Returns:
//   - [string]: the title and message, separated by ": ".
func (statusError *statusError) Error() string { return statusError.title + ": " + statusError.message }

// [badRequest] reports input the user can fix, answered with 400.
//
// Parameters:
//   - [message]: the sentence shown to the user.
//
// Returns:
//   - [error]: a [*statusError] with status 400 and the title "Bad
//     request".
func badRequest(message string) error {
	return &statusError{status: http.StatusBadRequest, title: "Bad request", message: message}
}

// [respondError] is the one place errors become responses:
//
//	request cancelled         nothing (and nothing logged)
//	*statusError              its status, title and message
//	*http.MaxBytesError       413, the upload is too large
//	store.ErrNotFound         404
//	store.ErrDuplicate        409
//	stream.ErrBusy            503 with Retry-After: 30
//	anything else             500, logged with the request
//
// Parameters:
//   - [w]: where to write the response.
//   - [r]: the request that failed.
//   - [err]: the handler's error.
func (responder responder) respondError(w http.ResponseWriter, r *http.Request, err error) {
	var statusErr *statusError
	var tooBig *http.MaxBytesError
	switch {
	case r.Context().Err() != nil:
	case errors.As(err, &statusErr):
		responder.errorPage(w, r, statusErr.status, statusErr.title, statusErr.message)
	case errors.As(err, &tooBig):
		responder.errorPage(w, r, http.StatusRequestEntityTooLarge, "Too large",
			"The upload is too large. The limit is "+strconv.FormatInt(tooBig.Limit>>20, 10)+" MB.")
	case errors.Is(err, store.ErrNotFound):
		responder.notFound(w, r)
	case errors.Is(err, store.ErrDuplicate):
		responder.errorPage(w, r, http.StatusConflict, "Already exists", "Something with that name already exists.")
	case errors.Is(err, stream.ErrBusy):
		w.Header().Set("Retry-After", "30")
		http.Error(w, "The server is busy converting other videos. Try again in a minute.", http.StatusServiceUnavailable)
	default:
		responder.serverError(w, r, err)
	}
}

// [respond] writes a fragment alone for an htmx request, which swaps it into
// the current page, and wraps it in a full page otherwise, so every dialog
// also works as a standalone URL.
//
// Parameters:
//   - [w]: where to write the response.
//   - [r]: the request.
//   - [status]: the HTTP status code.
//   - [title]: the full page's title.
//   - [fragment]: the content.
//   - [page]: wraps [fragment] in a full page.
//
// Returns:
//   - [error]: always [nil], so handlers can return it directly.
func (responder responder) respond(w http.ResponseWriter, r *http.Request, status int, title string,
	fragment templ.Component, page func(Page, templ.Component) templ.Component) error {
	if isHTMX(r) {
		responder.render(w, r, status, fragment)
	} else {
		responder.render(w, r, status, page(newPage(r, title), fragment))
	}
	return nil
}

// [render] writes a component as an HTML response. A failure while rendering
// is logged; by then the status has been sent.
//
// Parameters:
//   - [w]: where to write the response.
//   - [r]: the request, whose context the component renders with.
//   - [status]: the HTTP status code.
//   - [component]: what to render.
func (responder responder) render(w http.ResponseWriter, r *http.Request, status int, component templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Vary", "HX-Request")
	w.WriteHeader(status)
	if err := component.Render(r.Context(), w); err != nil {
		responder.log.Error("render", "err", err, "path", r.URL.Path)
	}
}

// [serverError] logs an unexpected error with the request and answers with a
// generic 500 page.
//
// Parameters:
//   - [w]: where to write the response.
//   - [r]: the request that failed.
//   - [err]: the error, which is logged but not shown.
func (responder responder) serverError(w http.ResponseWriter, r *http.Request, err error) {
	responder.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	responder.errorPage(w, r, http.StatusInternalServerError, "Something went wrong", "The server hit an error. Please try again.")
}

// [notFound] answers with a 404 page.
//
// Parameters:
//   - [w]: where to write the response.
//   - [r]: the request.
func (responder responder) notFound(w http.ResponseWriter, r *http.Request) {
	responder.errorPage(w, r, http.StatusNotFound, "Not found", "That page or item does not exist.")
}

// [errorPage] shows an error as an alert in the dialog or page that asked
// (for htmx requests), or as a full page.
//
// Parameters:
//   - [w]: where to write the response.
//   - [r]: the request.
//   - [status]: the HTTP status code.
//   - [title]: the full page's title.
//   - [message]: the sentence shown to the user.
func (responder responder) errorPage(w http.ResponseWriter, r *http.Request, status int, title, message string) {
	if isHTMX(r) {
		responder.render(w, r, status, alert("error", message))
		return
	}
	responder.render(w, r, status, messagePage(newPage(r, title), title, message))
}

// [isHTMX] reports whether htmx sent the request to swap part of a page.
// Boosted requests replace the whole page, so they count as normal ones.
//
// Parameters:
//   - [r]: the request.
//
// Returns:
//   - [bool]: [true] for an htmx request that is not boosted, [false]
//     otherwise.
func isHTMX(r *http.Request) bool {
	return r.Header.Get("HX-Request") == "true" && r.Header.Get("HX-Boosted") != "true"
}

// [pathID] reads the numeric {id} wildcard in the request's path.
//
// Parameters:
//   - [r]: the request, whose route has an {id} wildcard.
//
// Returns:
//   - [int64]: the ID, at least 1, or 0 if [error] is not [nil].
//   - [error]: [nil] on success, or [store.ErrNotFound] if {id} is missing,
//     not a number, or 0 or less: to the visitor it is a page that does not
//     exist, so it gets a 404, not a 400.
func pathID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, store.ErrNotFound
	}
	return id, nil
}

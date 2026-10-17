package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/AlexCristian/NexusPlay/internal/auth"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// accountHandlers serve logging in, registering and logging out.
type accountHandlers struct {
	responder
	accounts      accountService
	sessions      sessionService
	secureCookies bool
}

// accountService checks credentials and creates accounts (auth.Accounts).
type accountService interface {
	FirstUser(ctx context.Context) (bool, error)
	Register(ctx context.Context, in auth.Registration) (store.User, error)
	Login(ctx context.Context, email, password string) (store.User, error)
}

// sessionService starts and ends logins (auth.Sessions).
type sessionService interface {
	Start(ctx context.Context, userID int64) (token string, expires time.Time, err error)
	End(ctx context.Context, token string) error
}

// safeNext returns next if it is a path on this site, and "/" otherwise. It
// prevents open redirects after login: next comes from the query string, so
// anything that a browser could read as another site is refused. That covers
// schemes and hosts ("https://evil", "//evil"), backslashes ("/\evil", which
// browsers treat like "/"), and control characters ("/\t/evil": browsers strip
// tabs and newlines from URLs, turning it into "//evil"). The result is
// rebuilt from the parsed path and query, so nothing else slips through.
func safeNext(next string) string {
	if next == "" {
		return "/"
	}
	for _, r := range next {
		if r < 0x20 || r == 0x7f || r == '\\' {
			return "/"
		}
	}
	u, err := url.Parse(next)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" {
		return "/"
	}
	if !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") {
		return "/"
	}
	out := u.EscapedPath()
	if u.RawQuery != "" {
		out += "?" + u.RawQuery
	}
	return out
}

// loginPage handles GET /login, as a page or (htmx) a dialog.
func (h *accountHandlers) loginPage(w http.ResponseWriter, r *http.Request) error {
	next := r.URL.Query().Get("next")
	if next == "" && isHTMX(r) {
		next = currentURL(r) // opened as a dialog: come back to the same page
	}
	return h.respond(w, r, http.StatusOK, "Log in", loginForm("", "", safeNext(next)), authPage)
}

// registerPage handles GET /register, as a page or (htmx) a dialog.
func (h *accountHandlers) registerPage(w http.ResponseWriter, r *http.Request) error {
	first, err := h.accounts.FirstUser(r.Context())
	if err != nil {
		return err
	}
	return h.respond(w, r, http.StatusOK, "Register", registerForm("", "", "", first), authPage)
}

// rejectAuthForm shows a login or registration form again with its error.
// A full page carries the failure status; an htmx dialog needs 200, because
// htmx only swaps in successful responses.
func (h *accountHandlers) rejectAuthForm(w http.ResponseWriter, r *http.Request, status int, title string, form templ.Component) error {
	if isHTMX(r) {
		status = http.StatusOK
	}
	return h.respond(w, r, status, title, form, authPage)
}

// login handles POST /login from the login form or dialog.
func (h *accountHandlers) login(w http.ResponseWriter, r *http.Request) error {
	email := strings.TrimSpace(r.FormValue("email"))
	next := safeNext(r.FormValue("next"))
	u, err := h.accounts.Login(r.Context(), email, r.FormValue("password"))
	if errors.Is(err, auth.ErrInvalidCredentials) {
		return h.rejectAuthForm(w, r, http.StatusUnauthorized, "Log in", loginForm("Wrong e-mail or password.", email, next))
	}
	if err != nil {
		return err
	}
	if err := h.startSession(w, r, u.ID); err != nil {
		return err
	}
	h.redirect(w, r, next)
	return nil
}

// register handles POST /register. The first account becomes the administrator.
func (h *accountHandlers) register(w http.ResponseWriter, r *http.Request) error {
	in := auth.Registration{Email: r.FormValue("email"), Nickname: r.FormValue("nickname"), Password: r.FormValue("password")}
	u, err := h.accounts.Register(r.Context(), in)
	var invalid *auth.ValidationError
	var msg string
	switch {
	case errors.As(err, &invalid):
		msg = invalid.Msg
	case errors.Is(err, store.ErrDuplicate):
		msg = "That e-mail or nickname is already registered."
	case err != nil:
		return err
	default:
		if err := h.startSession(w, r, u.ID); err != nil {
			return err
		}
		h.redirect(w, r, "/")
		return nil
	}
	first, err := h.accounts.FirstUser(r.Context())
	if err != nil {
		return err
	}
	form := registerForm(msg, strings.TrimSpace(in.Email), strings.TrimSpace(in.Nickname), first)
	return h.rejectAuthForm(w, r, http.StatusBadRequest, "Register", form)
}

// logout handles POST /logout.
func (h *accountHandlers) logout(w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(sessionCookie); err == nil {
		// The cookie is cleared below regardless: the user asked to leave. A
		// session row left behind expires on its own.
		if err := h.sessions.End(r.Context(), c.Value); err != nil {
			h.log.Warn("could not delete a session on logout", "err", err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: h.secureCookies, SameSite: http.SameSiteLaxMode})
	h.redirect(w, r, "/")
	return nil
}

// startSession logs a user in: a new session and its cookie.
func (h *accountHandlers) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	token, expires, err := h.sessions.Start(r.Context(), userID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: h.secureCookies, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// redirect sends the browser elsewhere, whether the request came from htmx or a plain form.
func (h *accountHandlers) redirect(w http.ResponseWriter, r *http.Request, to string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

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
	RegistrationStatus(ctx context.Context) (auth.RegistrationStatus, error)
	Register(ctx context.Context, registration auth.Registration) (store.User, error)
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
	for _, character := range next {
		if character < 0x20 || character == 0x7f || character == '\\' {
			return "/"
		}
	}
	parsedURL, err := url.Parse(next)
	if err != nil || parsedURL.Scheme != "" || parsedURL.Host != "" || parsedURL.User != nil || parsedURL.Opaque != "" {
		return "/"
	}
	if !strings.HasPrefix(parsedURL.Path, "/") || strings.HasPrefix(parsedURL.Path, "//") {
		return "/"
	}
	out := parsedURL.EscapedPath()
	if parsedURL.RawQuery != "" {
		out += "?" + parsedURL.RawQuery
	}
	return out
}

// loginPage handles GET /login, as a page or (htmx) a dialog.
func (handlers *accountHandlers) loginPage(w http.ResponseWriter, r *http.Request) error {
	next := r.URL.Query().Get("next")
	if next == "" && isHTMX(r) {
		next = currentURL(r) // opened as a dialog: come back to the same page
	}
	return handlers.respond(w, r, http.StatusOK, "Log in", loginForm("", "", safeNext(next)), authPage)
}

// registerPage handles GET /register, as a page or (htmx) a dialog.
func (handlers *accountHandlers) registerPage(w http.ResponseWriter, r *http.Request) error {
	status, err := handlers.accounts.RegistrationStatus(r.Context())
	if err != nil {
		return err
	}
	return handlers.respond(w, r, http.StatusOK, "Register", registerForm("", "", "", status), authPage)
}

// rejectAuthForm shows a login or registration form again with its error.
// A full page carries the failure status; an htmx dialog needs 200, because
// htmx only swaps in successful responses.
func (handlers *accountHandlers) rejectAuthForm(w http.ResponseWriter, r *http.Request, status int, title string, form templ.Component) error {
	if isHTMX(r) {
		status = http.StatusOK
	}
	return handlers.respond(w, r, status, title, form, authPage)
}

// login handles POST /login from the login form or dialog.
func (handlers *accountHandlers) login(w http.ResponseWriter, r *http.Request) error {
	email := strings.TrimSpace(r.FormValue("email"))
	next := safeNext(r.FormValue("next"))
	user, err := handlers.accounts.Login(r.Context(), email, r.FormValue("password"))
	if errors.Is(err, auth.ErrInvalidCredentials) {
		return handlers.rejectAuthForm(w, r, http.StatusUnauthorized, "Log in", loginForm("Wrong e-mail or password.", email, next))
	}
	if err != nil {
		return err
	}
	if err := handlers.startSession(w, r, user.ID); err != nil {
		return err
	}
	handlers.redirect(w, r, next)
	return nil
}

// register handles POST /register. The first account becomes the administrator.
func (handlers *accountHandlers) register(w http.ResponseWriter, r *http.Request) error {
	registration := auth.Registration{Email: r.FormValue("email"), Nickname: r.FormValue("nickname"), Password: r.FormValue("password")}
	user, err := handlers.accounts.Register(r.Context(), registration)
	var invalid *auth.ValidationError
	var message string
	status := http.StatusBadRequest
	switch {
	case errors.As(err, &invalid):
		message = invalid.Message
	case errors.Is(err, store.ErrDuplicate):
		message = "That e-mail or nickname is already registered."
	case errors.Is(err, auth.ErrRegistrationClosed):
		status = http.StatusForbidden // the form below says why
	case err != nil:
		return err
	default:
		if err := handlers.startSession(w, r, user.ID); err != nil {
			return err
		}
		handlers.redirect(w, r, "/")
		return nil
	}
	registrationStatus, err := handlers.accounts.RegistrationStatus(r.Context())
	if err != nil {
		return err
	}
	form := registerForm(message, strings.TrimSpace(registration.Email), strings.TrimSpace(registration.Nickname), registrationStatus)
	return handlers.rejectAuthForm(w, r, status, "Register", form)
}

// logout handles POST /logout.
func (handlers *accountHandlers) logout(w http.ResponseWriter, r *http.Request) error {
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		// The cookie is cleared below regardless: the user asked to leave. A
		// session row left behind expires on its own.
		if err := handlers.sessions.End(r.Context(), cookie.Value); err != nil {
			handlers.log.Warn("could not delete a session on logout", "err", err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: handlers.secureCookies, SameSite: http.SameSiteLaxMode})
	handlers.redirect(w, r, "/")
	return nil
}

// startSession logs a user in: a new session and its cookie.
func (handlers *accountHandlers) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	token, expires, err := handlers.sessions.Start(r.Context(), userID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: handlers.secureCookies, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// redirect sends the browser elsewhere, whether the request came from htmx or a plain form.
func (handlers *accountHandlers) redirect(w http.ResponseWriter, r *http.Request, target string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

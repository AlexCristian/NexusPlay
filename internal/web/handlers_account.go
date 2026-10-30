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

// [accountHandlers] serve signing in, registering and signing out.
type accountHandlers struct {
	// [responder] writes the responses.
	responder
	// [accounts] checks credentials and creates accounts.
	accounts accountService
	// [sessions] starts and ends sign-in sessions.
	sessions sessionService
	// [secureCookies] marks the session cookie Secure, so browsers send it only
	// over HTTPS.
	secureCookies bool
}

// [accountService] checks credentials and creates accounts; [auth.Accounts]
// implements it.
type accountService interface {
	// [RegistrationStatus] tells whether registering is open.
	//
	// Parameters:
	//   - [ctx]: cancels the query.
	//
	// Returns:
	//   - [auth.RegistrationStatus]: the status.
	//   - [error]: [nil] on success, or the database error.
	RegistrationStatus(ctx context.Context) (auth.RegistrationStatus, error)
	// [Register] validates a sign-up and creates the account.
	//
	// Parameters:
	//   - [ctx]: cancels the database work.
	//   - [registration]: what the form submitted.
	//
	// Returns:
	//   - [store.User]: the new account.
	//   - [error]: as [auth.Accounts.Register] describes.
	Register(ctx context.Context, registration auth.Registration) (store.User, error)
	// [Login] checks an e-mail and password.
	//
	// Parameters:
	//   - [ctx]: cancels the database work.
	//   - [email]: the e-mail typed in.
	//   - [password]: the password typed in.
	//
	// Returns:
	//   - [store.User]: the account.
	//   - [error]: as [auth.Accounts.Login] describes.
	Login(ctx context.Context, email, password string) (store.User, error)
}

// [sessionService] starts and ends sign-in sessions; [auth.Sessions]
// implements it.
type sessionService interface {
	// [Start] creates a session for an account.
	//
	// Parameters:
	//   - [ctx]: cancels the insert.
	//   - [userID]: the account to sign in.
	//
	// Returns:
	//   - [string] ([token]): the token for the session cookie.
	//   - [time.Time] ([expires]): when the session expires.
	//   - [error] ([err]): [nil] on success, or the error.
	Start(ctx context.Context, userID int64) (token string, expires time.Time, err error)
	// [End] deletes a session.
	//
	// Parameters:
	//   - [ctx]: cancels the delete.
	//   - [token]: the session cookie's token.
	//
	// Returns:
	//   - [error]: [nil] on success, or the database error.
	End(ctx context.Context, token string) error
}

// [safeNext] makes the "next" address to go to after signing in safe to
// redirect to. It prevents open redirects: "next" comes from the query
// string, so anything a browser could read as another site is refused. That
// covers schemes and hosts ("https://evil", "//evil"), backslashes
// ("/\evil", which browsers treat like "/"), and control characters
// ("/\t/evil": browsers strip tabs and newlines from URLs, turning it into
// "//evil"). The result is rebuilt from the parsed path and query, so
// nothing else slips through.
//
// Parameters:
//   - [next]: the requested address, or "".
//
// Returns:
//   - [string]: [next]'s path and query if it is a path on this site, and
//     "/" otherwise.
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
	safe := parsedURL.EscapedPath()
	if parsedURL.RawQuery != "" {
		safe += "?" + parsedURL.RawQuery
	}
	return safe
}

// [loginPage] handles GET /login: the sign-in form, as a page or, for htmx,
// a dialog. Opened as a dialog without a "next" parameter, it comes back to
// the current page after signing in.
//
// Parameters:
//   - [w]: where the form is written.
//   - [r]: the request; its "next" parameter is where to go afterwards.
//
// Returns:
//   - [error]: always [nil].
func (handlers *accountHandlers) loginPage(w http.ResponseWriter, r *http.Request) error {
	next := r.URL.Query().Get("next")
	if next == "" && isHTMX(r) {
		next = currentURL(r) // opened as a dialog: come back to the same page
	}
	return handlers.respond(w, r, http.StatusOK, "Log in", loginForm("", "", safeNext(next)), authPage)
}

// [registerPage] handles GET /register: the sign-up form, as a page or, for
// htmx, a dialog.
//
// Parameters:
//   - [w]: where the form is written.
//   - [r]: the request.
//
// Returns:
//   - [error]: [nil] once written, or the database error.
func (handlers *accountHandlers) registerPage(w http.ResponseWriter, r *http.Request) error {
	status, err := handlers.accounts.RegistrationStatus(r.Context())
	if err != nil {
		return err
	}
	return handlers.respond(w, r, http.StatusOK, "Register", registerForm("", "", "", status), authPage)
}

// [rejectAuthForm] shows a sign-in or sign-up form again with its error. A
// full page carries the failure status; an htmx dialog gets 200, because
// htmx only swaps in successful responses.
//
// Parameters:
//   - [w]: where the form is written.
//   - [r]: the request.
//   - [status]: the failure status for a full page, such as 401.
//   - [title]: the full page's title.
//   - [form]: the form, with its error message.
//
// Returns:
//   - [error]: always [nil].
func (handlers *accountHandlers) rejectAuthForm(w http.ResponseWriter, r *http.Request, status int, title string, form templ.Component) error {
	if isHTMX(r) {
		status = http.StatusOK
	}
	return handlers.respond(w, r, status, title, form, authPage)
}

// [login] handles POST /login from the sign-in form or dialog: it checks
// the e-mail and password, starts a session and sends the browser to
// "next". Wrong credentials show the form again with a 401.
//
// Parameters:
//   - [w]: where the redirect or form is written.
//   - [r]: the request with the form fields "email", "password" and "next".
//
// Returns:
//   - [error]: [nil] once answered, or a database or session error.
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

// [register] handles POST /register: it creates the account, signs it in
// and sends the browser to the home page. The first account becomes the
// administrator. Invalid input or a taken e-mail or nickname shows the form
// again with a 400, and closed registration with a 403.
//
// Parameters:
//   - [w]: where the redirect or form is written.
//   - [r]: the request with the form fields "email", "nickname" and
//     "password".
//
// Returns:
//   - [error]: [nil] once answered, or a database or session error.
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

// [logout] handles POST /logout: it deletes the session, clears the session
// cookie and sends the browser to the home page. The cookie is cleared even
// if the session cannot be deleted (that is logged), since the user asked
// to leave; a session row left behind expires on its own.
//
// Parameters:
//   - [w]: where the redirect is written.
//   - [r]: the request, with the session cookie if there is one.
//
// Returns:
//   - [error]: always [nil].
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

// [startSession] signs a user in: it creates a session and sets its cookie,
// HTTP-only and SameSite=Lax, expiring with the session.
//
// Parameters:
//   - [w]: where the cookie is set.
//   - [r]: the request.
//   - [userID]: the account to sign in.
//
// Returns:
//   - [error]: [nil] on success, or the session error.
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

// [redirect] sends the browser elsewhere: with an HX-Redirect header and 204
// for htmx, or a 303 See Other for a plain form.
//
// Parameters:
//   - [w]: where the redirect is written.
//   - [r]: the request.
//   - [target]: the address to go to.
func (handlers *accountHandlers) redirect(w http.ResponseWriter, r *http.Request, target string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", target)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

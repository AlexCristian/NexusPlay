package web

import (
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/auth"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

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

func (s *Server) loginPage(w http.ResponseWriter, r *http.Request) {
	next := r.URL.Query().Get("next")
	if next == "" && isHTMX(r) {
		next = currentURL(r) // opened as a dialog: come back to the same page
	}
	next = safeNext(next)
	if isHTMX(r) {
		s.render(w, r, http.StatusOK, loginForm("", "", next))
		return
	}
	s.render(w, r, http.StatusOK, authPage(s.page(r, "Log in"), loginForm("", "", next)))
}

func (s *Server) registerPage(w http.ResponseWriter, r *http.Request) {
	first, err := s.firstUser(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	form := registerForm("", "", "", first)
	if isHTMX(r) {
		s.render(w, r, http.StatusOK, form)
		return
	}
	s.render(w, r, http.StatusOK, authPage(s.page(r, "Register"), form))
}

func (s *Server) firstUser(r *http.Request) (bool, error) {
	return s.Accounts.FirstUser(r.Context())
}

// login handles POST /login from the login form or dialog.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	next := safeNext(r.FormValue("next"))
	u, err := s.Accounts.Login(r.Context(), email, r.FormValue("password"))
	if errors.Is(err, auth.ErrInvalidCredentials) {
		form := loginForm("Wrong e-mail or password.", email, next)
		if isHTMX(r) {
			s.render(w, r, http.StatusOK, form)
		} else {
			s.render(w, r, http.StatusUnauthorized, authPage(s.page(r, "Log in"), form))
		}
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if err := s.startSession(w, r, u.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.redirect(w, r, next)
}

// register handles POST /register. The first account becomes the administrator.
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	in := auth.Registration{Email: r.FormValue("email"), Nickname: r.FormValue("nickname"), Password: r.FormValue("password")}
	u, err := s.Accounts.Register(r.Context(), in)
	var invalid *auth.ValidationError
	msg := ""
	switch {
	case errors.As(err, &invalid):
		msg = invalid.Msg
	case errors.Is(err, store.ErrDuplicate):
		msg = "That e-mail or nickname is already registered."
	case err != nil:
		s.serverError(w, r, err)
		return
	}
	if msg != "" {
		first, ferr := s.firstUser(r)
		if ferr != nil {
			s.serverError(w, r, ferr)
			return
		}
		form := registerForm(msg, strings.TrimSpace(in.Email), strings.TrimSpace(in.Nickname), first)
		if isHTMX(r) {
			s.render(w, r, http.StatusOK, form)
		} else {
			s.render(w, r, http.StatusBadRequest, authPage(s.page(r, "Register"), form))
		}
		return
	}
	if err := s.startSession(w, r, u.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.redirect(w, r, "/")
}

// logout handles POST /logout.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		// The cookie is cleared below regardless: the user asked to leave. A
		// session row left behind expires on its own.
		if err := s.Sessions.End(r.Context(), c.Value); err != nil {
			s.Log.Warn("could not delete a session on logout", "err", err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.Cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
	s.redirect(w, r, "/")
}

// startSession logs a user in: a new session and its cookie.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	token, expires, err := s.Sessions.Start(r.Context(), userID)
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: expires,
		HttpOnly: true, Secure: s.Cfg.SecureCookies, SameSite: http.SameSiteLaxMode,
	})
	return nil
}

// redirect sends the browser elsewhere, whether the request came from htmx or a plain form.
func (s *Server) redirect(w http.ResponseWriter, r *http.Request, to string) {
	if isHTMX(r) {
		w.Header().Set("HX-Redirect", to)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

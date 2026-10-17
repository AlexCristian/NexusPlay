package web

import (
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/AlexCristian/NexusPlay/internal/auth"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// A precomputed hash lets failed logins for unknown emails take as long as
// real ones, so response times do not reveal which emails are registered.
var dummyHash = sync.OnceValue(func() string {
	h, err := auth.HashPassword("nexusplay-timing-equalizer")
	if err != nil {
		// Only a broken system random source fails here. Without this hash,
		// logins for unknown emails would return faster than real ones and
		// reveal which emails are registered, so refuse to continue.
		panic("auth: cannot create the timing-equalizer hash: " + err.Error())
	}
	return h
})

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
	has, err := s.Store.HasUsers(r.Context())
	return !has, err
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	password := r.FormValue("password")
	next := safeNext(r.FormValue("next"))
	fail := func() {
		form := loginForm("Wrong e-mail or password.", email, next)
		if isHTMX(r) {
			s.render(w, r, http.StatusOK, form)
		} else {
			s.render(w, r, http.StatusUnauthorized, authPage(s.page(r, "Log in"), form))
		}
	}
	u, err := s.Store.UserByEmail(r.Context(), email)
	if errors.Is(err, store.ErrNotFound) {
		auth.VerifyPassword(password, dummyHash())
		fail()
		return
	} else if err != nil {
		s.serverError(w, r, err)
		return
	}
	needsRehash, err := auth.VerifyPassword(password, u.PasswordHash)
	if err != nil {
		fail()
		return
	}
	if needsRehash {
		// Upgrading an old hash is best effort: the password was correct, so
		// the login succeeds either way, and the next login tries again.
		h, err := auth.HashPassword(password)
		if err == nil {
			err = s.Store.UpdatePasswordHash(r.Context(), u.ID, h)
		}
		if err != nil {
			s.Log.Warn("could not upgrade a password hash", "user", u.ID, "err", err)
		}
	}
	if err := s.startSession(w, r, u.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.redirect(w, r, next)
}

func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	email := strings.TrimSpace(r.FormValue("email"))
	nickname := strings.TrimSpace(r.FormValue("nickname"))
	password := r.FormValue("password")
	first, err := s.firstUser(r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	fail := func(msg string) {
		form := registerForm(msg, email, nickname, first)
		if isHTMX(r) {
			s.render(w, r, http.StatusOK, form)
		} else {
			s.render(w, r, http.StatusBadRequest, authPage(s.page(r, "Register"), form))
		}
	}
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email || len(email) > 254 {
		fail("Enter a valid e-mail address.")
		return
	}
	if n := utf8.RuneCountInString(nickname); n < 2 || n > 40 {
		fail("Your nickname must be between 2 and 40 characters.")
		return
	}
	if utf8.RuneCountInString(password) < 8 || len(password) > 1024 {
		fail("Your password must be at least 8 characters long.")
		return
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	u, err := s.Store.CreateUser(r.Context(), email, nickname, hash)
	if errors.Is(err, store.ErrDuplicate) {
		fail("That e-mail or nickname is already registered.")
		return
	} else if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.Log.Info("user registered", "user", u.ID, "admin", u.IsAdmin)
	if err := s.startSession(w, r, u.ID); err != nil {
		s.serverError(w, r, err)
		return
	}
	s.redirect(w, r, "/")
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		// The cookie is cleared below regardless: the user asked to leave. A
		// session row left behind expires on its own.
		if err := s.Store.DeleteSession(r.Context(), auth.HashToken(c.Value)); err != nil {
			s.Log.Warn("could not delete a session on logout", "err", err)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: s.Cfg.SecureCookies, SameSite: http.SameSiteLaxMode})
	s.redirect(w, r, "/")
}

func (s *Server) startSession(w http.ResponseWriter, r *http.Request, userID int64) error {
	token, err := auth.NewToken()
	if err != nil {
		return err
	}
	csrf, err := auth.NewToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(sessionLifetime)
	if err := s.Store.CreateSession(r.Context(), auth.HashToken(token), userID, csrf, expires); err != nil {
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

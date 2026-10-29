// Package web serves NexusPlay's pages, htmx fragments and media streams.
//
// Server holds the route table and the middleware (logging, panic recovery,
// security headers, sessions, CSRF). The handlers are in five groups, each
// given only what it uses: library, playback, subtitles, accounts and admin
// (handlers_*.go). Handlers return an error, and respondError turns it into
// the response (respond.go), so every handler reports "not found" or "try
// later" the same way. Templates are templ components (*.templ); the data
// they show is in views.go.
//
// The package holds no business rules of its own beyond who may see what
// (access.go): accounts are auth's, data is store's, playback policy is
// media's.
package web

package web

// Page carries what every full page needs.
type Page struct {
	Title  string
	Viewer Viewer
	Query  string // the search box's text
}

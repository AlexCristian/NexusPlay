package web

// [Page] carries what every full page needs, from [newPage].
type Page struct {
	// [Title] is the page's title, shown in the browser tab.
	Title string
	// [Viewer] is who is making the request.
	Viewer Viewer
	// [Query] is the search box's text, or "".
	Query string
}

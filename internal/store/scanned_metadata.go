package store

// [ScannedMetadata] is what an online lookup found for a file. A field that
// is "" means "nothing found" and leaves the current value alone.
type ScannedMetadata struct {
	// [Title] is the title found, or "".
	Title string
	// [Year] is the release or first-air year found, or "".
	Year string
	// [Overview] is the summary found, or "".
	Overview string
	// [Poster] is the downloaded poster's file name in the poster folder, or
	// "".
	Poster string
}

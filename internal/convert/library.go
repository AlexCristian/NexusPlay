package convert

// [Library] is the scanner as the converter sees it: the lock it shares with
// scans, and a way to ask for a rescan, which imports extracted subtitles.
// [library.Scanner] implements it.
type Library interface {
	// [Exclusive] runs an operation while no scan is changing the database or
	// reading the media folder.
	//
	// Parameters:
	//   - [operation]: the work to do under the lock; it must not start a scan.
	//
	// Returns:
	//   - [error]: the error [operation] returned, or an error if the lock could
	//     not be taken in time.
	Exclusive(operation func() error) error
	// [Trigger] asks for a scan as soon as possible, without waiting for it.
	Trigger()
}

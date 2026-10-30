package convert

// [Library] is the scanner as the converter sees it: the lock it shares with
// scans, and a way to ask for a rescan, which imports extracted subtitles.
// [library.Scanner] implements it.
type Library interface {
	// [Exclusive] runs an operation while no scan is adding or removing media
	// rows. Scans may still walk the folder, probe files, look up metadata and
	// import subtitles meanwhile; a scan re-checks the disk under this lock
	// before changing rows. It waits as long as it takes.
	//
	// Parameters:
	//   - [operation]: the work to do under the lock; it must not start a scan.
	//
	// Returns:
	//   - [error]: the error [operation] returned.
	Exclusive(operation func() error) error
	// [Trigger] asks for a scan as soon as possible, without waiting for it.
	Trigger()
}

package convert

// Library is the scanner as the converter sees it: the lock it shares with
// scans, and a way to ask for a rescan (which imports extracted subtitles).
type Library interface {
	Exclusive(operation func() error) error
	Trigger()
}

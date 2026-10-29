package convert

// Config tunes the converter.
type Config struct {
	MediaDirectory string
	Max            int // conversions at once (at least 1)
	// FreeSpace reports the free bytes in a folder's file system. Nil uses
	// the operating system; tests replace it.
	FreeSpace func(directory string) (uint64, error)
}

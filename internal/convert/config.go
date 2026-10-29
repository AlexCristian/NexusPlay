package convert

// [Config] tunes the [Converter].
type Config struct {
	// [MediaDirectory] is the media folder; paths in the database are relative
	// to it.
	MediaDirectory string
	// [Max] is how many conversions may run at once. Less than 1 means 1.
	Max int
	// [FreeSpace] reports the free bytes in a folder's file system, checked
	// before each conversion. [nil] asks the operating system (Linux only;
	// elsewhere the check is skipped). Tests replace it.
	FreeSpace func(directory string) (uint64, error)
}

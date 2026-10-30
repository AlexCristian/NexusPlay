package convert

import "errors"

// Errors that mean "not now" rather than "this file cannot be converted".
var (
	// [ErrNoSpace] means the disk has too little free space for a conversion.
	// It means "not now": the file is postponed, not marked failed.
	ErrNoSpace = errors.New("not enough free disk space")
	// [ErrTargetExists] means a file already sits at the MP4's name, for example
	// one the user added while the conversion ran. It is never overwritten.
	ErrTargetExists = errors.New("a file with the converted file's name already exists")
	// [ErrSourceChanged] means the media row no longer points at the file that
	// was converted, because a rescan replaced or removed it. The converted file
	// is discarded.
	ErrSourceChanged = errors.New("the file changed in the library while it was being converted")
)

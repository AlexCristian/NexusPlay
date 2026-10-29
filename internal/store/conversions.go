package store

// Conversion states stored in the media table's convert_status column.
const (
	// [ConvertQueued] means the file is waiting for conversion, or needs none.
	ConvertQueued = ""
	// [ConvertFailed] means the converter gave up on the file; an
	// administrator can retry it from the control panel.
	ConvertFailed = "failed"
)

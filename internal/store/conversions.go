package store

// Conversion states stored in media.convert_status.
const (
	ConvertQueued = ""       // waiting, or nothing to do
	ConvertFailed = "failed" // gave up; an administrator can retry
)

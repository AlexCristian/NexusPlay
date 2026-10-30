package stream

import "errors"

// Errors the [Manager] returns.
var (
	// [ErrBusy] means the limit of running transcodes is reached. Callers should
	// ask the client to retry later.
	ErrBusy = errors.New("too many transcodes running")
	// [ErrNoSegment] means a requested segment does not exist and did not appear
	// in time.
	ErrNoSegment = errors.New("segment not available")
)

package stream

import (
	"time"
)

// [Config] tunes a [Manager]. Zero durations use the defaults.
type Config struct {
	// [Directory] is the scratch folder for HLS output. [NewManager] deletes it
	// and everything in it, then creates it empty.
	Directory string
	// [Max] is how many transcodes may run at once, at least 1.
	Max int
	// [IdleTimeout] stops a running transcode nobody has fetched from for this
	// long, because the viewer left. 0 means 3 minutes.
	IdleTimeout time.Duration
	// [KeepDone] keeps a finished transcode for this long after it was last
	// used, so watching again does not start over. 0 means 6 hours.
	KeepDone time.Duration
}

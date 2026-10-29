package library

import (
	"time"
)

// [Config] tunes the [Scanner]. Zero durations use the defaults.
type Config struct {
	// [MediaDirectory] is the media folder to scan.
	MediaDirectory string
	// [Interval] is the time between full rescans. 0 or less means 5 minutes.
	Interval time.Duration
	// [Settle] is how long a new file must go unmodified before it is added, so
	// a file still being copied in is not read half-written. 0 or less means 30
	// seconds.
	Settle time.Duration
}

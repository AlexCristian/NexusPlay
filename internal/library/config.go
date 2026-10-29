package library

import (
	"time"
)

// Config tunes the scanner. Zero durations use the defaults.
type Config struct {
	MediaDirectory string
	Interval       time.Duration // full rescan period (default 5 min)
	Settle         time.Duration // how long a new file must go unmodified before import (default 30 s)
}

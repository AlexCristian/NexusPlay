package stream

import (
	"time"
)

// Config tunes a Manager. Zero durations use the defaults.
type Config struct {
	Directory   string        // scratch folder for HLS output; emptied by NewManager
	Max         int           // transcodes allowed to run at once
	IdleTimeout time.Duration // stop a running transcode nobody fetched from for this long (default 3 min)
	KeepDone    time.Duration // keep a finished transcode for this long after its last use (default 6 h)
}

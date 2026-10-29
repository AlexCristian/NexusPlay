package posters

import (
	"time"
)

// Config sets where posters live and how careful the sweep is.
type Config struct {
	Directory string
	MinAge    time.Duration // a file younger than this is kept (default 1 h)
	Interval  time.Duration // time between sweeps (default 1 h)
}

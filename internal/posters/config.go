package posters

import (
	"time"
)

// [Config] sets where posters live and how often and how carefully the
// [Sweeper] deletes them.
type Config struct {
	// [Directory] is the poster folder, the one [config.Config.PosterDirectory]
	// names.
	Directory string
	// [MinAge] is how old an unused file must be before it is deleted, so a
	// poster written just before the row that uses it is saved survives. 0 or
	// less means one hour.
	MinAge time.Duration
	// [Interval] is the time between sweeps. 0 or less means one hour.
	Interval time.Duration
}

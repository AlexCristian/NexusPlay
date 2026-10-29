package convert

// Status is a snapshot for the control panel.
type Status struct {
	Off         bool   // conversion is turned off in the configuration
	Unavailable string // why conversion cannot run (a read-only folder), if it cannot
	Active      []JobStatus
	Queued      int
	Postponed   int // waiting for disk space
}

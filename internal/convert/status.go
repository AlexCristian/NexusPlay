package convert

// [Status] is a snapshot of the converter for the control panel.
type Status struct {
	// [Off] is [true] when conversion is turned off in the configuration
	// (NEXUSPLAY_CONVERT).
	Off bool
	// [Unavailable] explains to the administrator why conversion cannot run,
	// such as a read-only media folder, or "" if it can.
	Unavailable string
	// [Active] lists the conversions running now, in no particular order.
	Active []JobStatus
	// [Queued] counts the files waiting to be converted, not counting running or
	// postponed ones.
	Queued int
	// [Postponed] counts the files skipped for now because the disk was too
	// full; they are tried again [noSpaceRetry] after the failed check.
	Postponed int
}

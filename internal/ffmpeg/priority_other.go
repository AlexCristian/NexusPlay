//go:build !linux

package ffmpeg

// [lowerPriority] does nothing outside Linux, where the process runs at
// normal priority.
//
// Parameters:
//   - [processID]: the process's ID; ignored.
func lowerPriority(processID int) {}

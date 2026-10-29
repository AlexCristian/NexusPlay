//go:build !linux

package convert

import "errors"

// [freeBytes] cannot check free space outside Linux, so the check before
// each conversion is skipped.
//
// Parameters:
//   - [directory]: ignored.
//
// Returns:
//   - [uint64]: always 0.
//   - [error]: always an error saying the check is not supported.
func freeBytes(directory string) (uint64, error) {
	return 0, errors.New("free space check not supported on this system")
}

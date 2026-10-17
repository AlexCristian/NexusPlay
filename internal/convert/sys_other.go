//go:build !linux

package convert

import "errors"

func freeBytes(string) (uint64, error) {
	return 0, errors.New("free space check not supported on this system")
}

//go:build !linux

package convert

func freeBytes(string) (uint64, bool) { return 0, false }

//go:build !linux

package convert

func lowerPriority(int) {}

func freeBytes(string) (uint64, bool) { return 0, false }

// Package [collections] stands in for the real package, the one place a set
// may be built out of a map.
package collections

// [Set] is a set.
type Set[T comparable] struct {
	// [members] holds the set's values as the keys of a map.
	members map[T]struct{}
}

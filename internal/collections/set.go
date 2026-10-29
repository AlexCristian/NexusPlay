package collections

import (
	"iter"
	"maps"
)

// [Set] is an unordered collection of distinct values, like Java's HashSet.
// Create one with [NewSet]; the zero value is an empty set that [Set.Add]
// cannot add to. A [Set] is not safe for concurrent use.
type Set[T comparable] struct {
	// [members] holds the set's values as the keys of a map. The values of
	// the map carry nothing.
	members map[T]struct{}
}

// [NewSet] creates a set holding the given values.
//
// Parameters:
//   - [values]: the values to start with. Duplicates are stored once.
//
// Returns:
//   - [Set[T]]: a new set holding every one of [values], ready for
//     [Set.Add].
func NewSet[T comparable](values ...T) Set[T] {
	set := Set[T]{members: make(map[T]struct{}, len(values))}
	for _, value := range values {
		set.members[value] = struct{}{}
	}
	return set
}

// [Add] puts a value into the set. Adding a value the set already holds
// changes nothing.
//
// Parameters:
//   - [value]: the value to add.
func (set Set[T]) Add(value T) {
	set.members[value] = struct{}{}
}

// [Contains] reports whether the set holds a value.
//
// Parameters:
//   - [value]: the value to look for.
//
// Returns:
//   - [bool]: [true] if [value] is in the set, [false] otherwise.
func (set Set[T]) Contains(value T) bool {
	_, contains := set.members[value]
	return contains
}

// [Len] counts the values in the set.
//
// Returns:
//   - [int]: the number of distinct values in the set.
func (set Set[T]) Len() int {
	return len(set.members)
}

// [All] lists the set's values, for use in a "for … range" loop.
//
// Returns:
//   - [iter.Seq[T]]: every value in the set, once each, in no particular
//     order.
func (set Set[T]) All() iter.Seq[T] {
	return maps.Keys(set.members)
}

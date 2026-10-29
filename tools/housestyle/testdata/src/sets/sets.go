// Package [sets] holds map types for the set check.
package sets

// [marker] is an empty struct type.
type marker struct{}

// [flag] is a named boolean type, whose values mean something.
type flag bool

// [seen] is a set written as a map to booleans.
var seen map[string]bool // want "map\\[string\\]bool is a set written as a map"

// [members] is a set written as a map to empty structs.
var members = map[int]struct{}{} // want "map\\[int\\]struct\\{\\} is a set written as a map"

// [marked] is a set written as a map to a named empty struct.
var marked map[int]marker // want "map\\[int\\]marker is a set written as a map"

// [counts] is a real map.
var counts map[string]int

// [flags] maps to a named boolean type, which is not reported.
var flags map[string]flag

// [collect] builds a set inside a function.
//
// Returns:
//   - [int]: how many values were added.
func collect() int {
	local := make(map[string]bool) // want "map\\[string\\]bool is a set written as a map"
	local["one"] = true
	return len(local)
}

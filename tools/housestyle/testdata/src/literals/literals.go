// Package [literals] holds package-level values for the data literal check.
package literals

import "strings"

// [words] is a word list written in code.
var words = []string{"the", "a", "an"} // want "fixed list of 3 string literals"

// [lookup] is a lookup table written in code.
var lookup = map[string]string{"a": "b", "c": "d"} // want "fixed list of 4 string literals"

// [pointerList] is a list behind a pointer.
var pointerList = &[]string{"one", "two", "three"} // want "fixed list of 3 string literals"

// [table] is a table of structs.
var table = []struct { // want "fixed list of 4 string literals"
	// [name] is the entry's name.
	name string
	// [value] is the entry's value.
	value string
}{{"a", "b"}, {"c", "d"}}

// [pointerTable] is a table of pointers to structs, with the "&" left out.
var pointerTable = []*setting{{"a", "b"}, {name: "c"}} // want "fixed list of 3 string literals"

// [replacer] is a lookup table passed to a function.
var replacer = strings.NewReplacer("&", "&amp;", "<", "&lt;") // want "fixed list of 4 string literals"

// The grouped list is reported too.
var (
	// [grouped] is a word list in a group.
	grouped = [3]string{"x", "y", "z"} // want "fixed list of 3 string literals"
)

// [setting] is one value with several string fields.
type setting struct {
	// [name] is the setting's name.
	name string
	// [value] is the setting's value.
	value string
}

// [defaults] holds string settings of one struct value, not a list.
var defaults = setting{name: "mode", value: "fast"}

// [settings] holds string settings behind a pointer, with a third string in
// a function call.
var settings = &setting{name: "mode", value: strings.ToUpper("fast")}

// [pair] has only two strings.
var pair = []string{"one", "two"}

// [joined] is three strings joined into one, not a list.
const joined = "a" + "b" + "c"

// [handler] is code, not data.
var handler = func() []string {
	return []string{"one", "two", "three"}
}

// [local] declares a list inside a function, which is not checked.
//
// Returns:
//   - [[]string]: three words.
func local() []string {
	list := []string{"one", "two", "three"}
	return list
}

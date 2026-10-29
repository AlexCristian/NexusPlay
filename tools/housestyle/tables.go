package housestyle

import (
	_ "embed"
	"strings"
)

// [abbreviationsFile] is the contents of abbreviations.txt, built into the
// analyzer by the compiler. The build fails if the file is missing.
//
//go:embed abbreviations.txt
var abbreviationsFile string

// [shortNamesFile] is the contents of short_names.txt, built into the
// analyzer by the compiler. The build fails if the file is missing.
//
//go:embed short_names.txt
var shortNamesFile string

// [abbreviations] maps each abbreviation that must not appear in a name to
// the full word to use instead, such as "eps" to "episodes".
var abbreviations = readTable(abbreviationsFile)

// [shortNames] maps each name shorter than three letters that is allowed to
// the only type it may have, or to "*" when any type is allowed.
var shortNames = readTable(shortNamesFile)

// [readTable] reads a two-column table with one entry per line.
//
// Parameters:
//   - [table]: the table's text. Each line holds a key, a space and a value.
//     Blank lines and lines starting with "#" are skipped, and spaces around
//     each key and value are trimmed.
//
// Returns:
//   - [map[string]string]: every key in [table], lowercased, mapped to its
//     value. A line with no value maps its key to "".
func readTable(table string) map[string]string {
	entries := map[string]string{}
	for _, line := range strings.Split(table, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, _ := strings.Cut(line, " ")
		entries[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return entries
}

package datafile

import (
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/collections"
)

// [Lines] lists the entries of a data file, one per line.
//
// Parameters:
//   - [text]: the file's contents. Blank lines and lines starting with "#"
//     are skipped, and spaces around each line are trimmed.
//
// Returns:
//   - [[]string]: the remaining lines in file order, or an empty slice if
//     there are none.
func Lines(text string) []string {
	entries := []string{}
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entries = append(entries, line)
	}
	return entries
}

// [Set] reads a data file that lists one value per line, such as a list of
// codec names, into a set for membership checks.
//
// Parameters:
//   - [text]: the file's contents, in the format [Lines] reads.
//
// Returns:
//   - [collections.Set[string]]: every entry of [text], exactly as written.
//     Lookups are case-sensitive.
func Set(text string) collections.Set[string] {
	return collections.NewSet(Lines(text)...)
}

// [Table] reads a data file of key-value pairs, one pair per line, into a
// lookup table. Each line holds a key, then spaces or tabs, then the value;
// the value may itself contain spaces, as in "scan Scan started.".
//
// Parameters:
//   - [name]: the file's name, such as "video_types.txt", used in the panic
//     message if a line is malformed.
//   - [text]: the file's contents, in the format [Lines] reads.
//
// Returns:
//   - [map[string]string]: every key in [text] mapped to its value. A key
//     that appears twice keeps its last value.
//
// [Table] panics if a line has a key but no value. Data files are built into
// the program, so a malformed one is a programming error that the package's
// tests catch, not a runtime condition.
func Table(name, text string) map[string]string {
	table := map[string]string{}
	for _, line := range Lines(text) {
		key, value, hasValue := strings.Cut(strings.ReplaceAll(line, "\t", " "), " ")
		value = strings.TrimSpace(value)
		if !hasValue || value == "" {
			panic("datafile: " + name + ": line " + `"` + line + `"` + " has no value")
		}
		table[key] = value
	}
	return table
}

package housestyle

import (
	_ "embed"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/collections"
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

// [codeWordsFile] is the contents of code_words.txt, built into the analyzer
// by the compiler. The build fails if the file is missing.
//
//go:embed code_words.txt
var codeWordsFile string

// [abbreviations] maps each abbreviation that must not appear in a name to
// the full word to use instead, such as "eps" to "episodes".
var abbreviations = readTable(abbreviationsFile)

// [shortNames] maps each name shorter than three letters that is allowed to
// the only type it may have, or to "*" when any type is allowed.
var shortNames = readTable(shortNamesFile)

// [codeWords] holds the lowercase words that are always code and never
// English, such as "nil", so a comment must write them in brackets wherever
// they appear.
var codeWords = readWordList(codeWordsFile)

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
	for _, line := range tableLines(table) {
		key, value, _ := strings.Cut(line, " ")
		entries[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return entries
}

// [readWordList] turns the text of a word list file, such as
// code_words.txt, into a [collections.Set] of its words, so callers can
// check whether a word is on the list with [collections.Set.Contains].
//
// Parameters:
//   - [list]: the list's text, one word per line. Blank lines and lines
//     starting with "#" are skipped, and spaces around each word are
//     trimmed.
//
// Returns:
//   - [collections.Set[string]]: every word in [list], as written.
func readWordList(list string) collections.Set[string] {
	return collections.NewSet(tableLines(list)...)
}

// [tableLines] gets the lines of a table or word list file that hold
// entries.
//
// Parameters:
//   - [text]: the file's text.
//
// Returns:
//   - [[]string]: every line of [text] with spaces around it trimmed, in
//     order, leaving out blank lines and lines starting with "#".
func tableLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	return lines
}

package housestyle

import (
	_ "embed"

	"github.com/AlexCristian/NexusPlay/internal/datafile"
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
var abbreviations = datafile.Table("abbreviations.txt", abbreviationsFile)

// [shortNames] maps each name shorter than three letters that is allowed to
// the only type it may have, or to "*" when any type is allowed.
var shortNames = datafile.Table("short_names.txt", shortNamesFile)

// [codeWords] holds the lowercase words that are always code and never
// English, such as "nil", so a comment must write them in brackets wherever
// they appear.
var codeWords = datafile.Set(codeWordsFile)

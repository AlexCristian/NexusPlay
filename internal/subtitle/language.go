package subtitle

import (
	_ "embed"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/datafile"
)

// [languageNamesFile] is the contents of language_names.txt, built into the
// program by the compiler. The build fails if the file is missing.
//
//go:embed language_names.txt
var languageNamesFile string

// [languageNames] maps lowercase language codes to their display names, from
// language_names.txt.
var languageNames = datafile.Table("language_names.txt", languageNamesFile)

// LanguageName returns a display name for a language code, falling back to
// the code itself.
func LanguageName(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if name, found := languageNames[code]; found {
		return name
	}
	if base, _, hasSubtag := strings.Cut(code, "-"); hasSubtag {
		if name, found := languageNames[base]; found {
			return name
		}
	}
	if code == "" || code == "und" {
		return "Subtitles"
	}
	return strings.ToUpper(code)
}

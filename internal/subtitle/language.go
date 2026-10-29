package subtitle

import "strings"

var languageNames = map[string]string{
	"ar": "العربية", "bg": "Български", "cs": "Čeština", "da": "Dansk", "de": "Deutsch",
	"el": "Ελληνικά", "en": "English", "es": "Español", "fi": "Suomi", "fr": "Français",
	"he": "עברית", "hr": "Hrvatski", "hu": "Magyar", "it": "Italiano", "ja": "日本語",
	"ko": "한국어", "nl": "Nederlands", "no": "Norsk", "pl": "Polski", "pt": "Português",
	"ro": "Română", "ru": "Русский", "sk": "Slovenčina", "sr": "Srpski", "sv": "Svenska",
	"tr": "Türkçe", "uk": "Українська", "zh": "中文",
	// Three-letter codes common in sidecar file names.
	"eng": "English", "rum": "Română", "ron": "Română", "fre": "Français", "fra": "Français",
	"ger": "Deutsch", "deu": "Deutsch", "spa": "Español", "ita": "Italiano", "por": "Português",
}

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

package housestyle

import (
	"go/ast"
	"go/types"
	"strings"
	"unicode"

	"golang.org/x/tools/go/analysis"
)

// [checkNames] reports every name declared in a file that is too short or
// uses an abbreviation. Package names, import names, type parameters and "_"
// are not checked.
//
// Parameters:
//   - [pass]: the package being analyzed, and where problems are reported.
//   - [file]: the file whose declarations are checked.
func checkNames(pass *analysis.Pass, file *ast.File) {
	ast.Inspect(file, func(node ast.Node) bool {
		identifier, isIdentifier := node.(*ast.Ident)
		if !isIdentifier || identifier.Name == "_" {
			return true
		}
		object := pass.TypesInfo.Defs[identifier]
		if object == nil {
			return true
		}
		if _, isImport := object.(*types.PkgName); isImport {
			return true
		}
		if _, isTypeParameter := object.Type().(*types.TypeParam); isTypeParameter {
			return true
		}
		if problem := nameProblem(identifier.Name, object.Type()); problem != "" {
			pass.Reportf(identifier.Pos(), "%s", problem)
		}
		return true
	})
}

// [nameProblem] decides whether a declared name breaks the naming rules.
//
// Parameters:
//   - [name]: the name as declared, such as "eps" or "tempFile".
//   - [declaredType]: the type of what [name] declares, used to check names
//     from short_names.txt that are only allowed for one type.
//
// Returns:
//   - [string]: a message saying what is wrong and what to use instead, or
//     "" if [name] follows the rules.
func nameProblem(name string, declaredType types.Type) string {
	if allowedType, isShortName := shortNames[strings.ToLower(name)]; isShortName {
		if allowedType == "*" || types.TypeString(declaredType, nil) == allowedType {
			return ""
		}
		return "name " + quote(name) + " is only allowed for a value of type " + allowedType + ": spell it out as a full word"
	}
	if len(name) < 3 {
		return "name " + quote(name) + " is too short: spell it out as a full word"
	}
	for _, word := range splitWords(name) {
		if replacement, isAbbreviation := abbreviations[word]; isAbbreviation {
			return "name " + quote(name) + " uses the abbreviation " + quote(word) + ": write " + quote(replacement) + " instead"
		}
	}
	return ""
}

// [splitWords] splits a name into its lowercase words, at underscores and
// at changes of case, keeping acronyms together.
//
// Parameters:
//   - [name]: a Go name, such as "tempFile", "HTTPClient" or "show_id".
//
// Returns:
//   - [[]string]: the words of [name] in lower case, such as "temp" and
//     "file", "http" and "client", or "show" and "id".
func splitWords(name string) []string {
	var words []string
	characters := []rune(name)
	start := 0
	for index := 1; index <= len(characters); index++ {
		atEnd := index == len(characters)
		if !atEnd && !startsWord(characters, index) {
			continue
		}
		word := strings.Trim(string(characters[start:index]), "_")
		if word != "" {
			words = append(words, strings.ToLower(word))
		}
		start = index
	}
	return words
}

// [startsWord] reports whether a new word of a name starts at a given
// character.
//
// Parameters:
//   - [characters]: the name's characters.
//   - [index]: the position in [characters] to check. It must be at least 1
//     and less than the length of [characters].
//
// Returns:
//   - [bool]: [true] after an underscore, at an uppercase letter that
//     follows a lowercase letter or digit ("temp|File"), and at the last
//     uppercase letter of an acronym followed by a lowercase letter
//     ("HTTP|Client"); [false] otherwise.
func startsWord(characters []rune, index int) bool {
	current, previous := characters[index], characters[index-1]
	if previous == '_' || current == '_' {
		return true
	}
	if !unicode.IsUpper(current) {
		return false
	}
	if unicode.IsLower(previous) || unicode.IsDigit(previous) {
		return true
	}
	hasNext := index+1 < len(characters)
	return unicode.IsUpper(previous) && hasNext && unicode.IsLower(characters[index+1])
}

// [quote] wraps text in double quotes for a message.
//
// Parameters:
//   - [text]: the text to quote.
//
// Returns:
//   - [string]: [text] between double quotes.
func quote(text string) string {
	return `"` + text + `"`
}

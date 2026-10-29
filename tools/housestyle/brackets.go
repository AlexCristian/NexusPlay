package housestyle

import (
	"bytes"
	"go/ast"
	"go/token"
	"go/types"
	"regexp"
	"strings"
	"unicode"

	"github.com/AlexCristian/NexusPlay/internal/collections"
	"golang.org/x/tools/go/analysis"
)

// [commentWord] matches a word in a comment, including a qualified name such
// as "metadata.Episode". A dot that ends a sentence is not part of the word.
var commentWord = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*`)

// [bracketCandidates] collects the names declared in a package that
// comments must write in brackets: package-level names, methods and struct
// fields that are exported or camelCase. Lowercase one-word names such as
// "similarity" are left out, because they cannot be told apart from
// ordinary English words; so are all-caps acronyms such as "ID".
//
// Parameters:
//   - [typesPackage]: the package whose declarations are collected.
//
// Returns:
//   - [collections.Set[string]]: every name to check.
func bracketCandidates(typesPackage *types.Package) collections.Set[string] {
	candidates := collections.NewSet[string]()
	add := func(name string) {
		if looksLikeCode(name) {
			candidates.Add(name)
		}
	}
	scope := typesPackage.Scope()
	for _, name := range scope.Names() {
		add(name)
		typeName, isTypeName := scope.Lookup(name).(*types.TypeName)
		if !isTypeName {
			continue
		}
		named, isNamed := typeName.Type().(*types.Named)
		if !isNamed {
			continue
		}
		for index := range named.NumMethods() {
			add(named.Method(index).Name())
		}
		if structure, isStruct := named.Underlying().(*types.Struct); isStruct {
			for index := range structure.NumFields() {
				add(structure.Field(index).Name())
			}
		}
	}
	return candidates
}

// [looksLikeCode] reports whether a name cannot be an ordinary English word,
// so a comment that mentions it must mean the code.
//
// Parameters:
//   - [name]: a declared name.
//
// Returns:
//   - [bool]: [true] if [name] has an uppercase letter and a lowercase
//     letter, such as "Client" or "posterDir"; [false] for lowercase words
//     such as "similarity" and all-caps acronyms such as "ID".
func looksLikeCode(name string) bool {
	hasUpper := strings.IndexFunc(name, unicode.IsUpper) >= 0
	hasLower := strings.IndexFunc(name, unicode.IsLower) >= 0
	return hasUpper && hasLower
}

// [checkBrackets] reports names in a file's comments that should be written
// in brackets: the package's own names from [bracketCandidates], and any
// name qualified with a package the file imports, such as
// "metadata.Episode". Text already in brackets or quotes, code blocks and
// directives such as "//go:embed" are skipped.
//
// Parameters:
//   - [pass]: where problems are reported.
//   - [file]: the file whose comments are checked.
//   - [candidates]: the package's own names to look for, from
//     [bracketCandidates].
func checkBrackets(pass *analysis.Pass, file *ast.File, candidates collections.Set[string]) {
	imports := collections.NewSet[string]()
	for _, importSpecification := range file.Imports {
		if importSpecification.Name != nil {
			imports.Add(importSpecification.Name.Name)
			continue
		}
		path := strings.Trim(importSpecification.Path.Value, `"`)
		imports.Add(path[strings.LastIndex(path, "/")+1:])
	}
	for _, group := range file.Comments {
		text, lines := maskedCommentText(group)
		for _, location := range commentWord.FindAllStringIndex(text, -1) {
			word := text[location[0]:location[1]]
			qualifier, _, isQualified := strings.Cut(word, ".")
			if candidates.Contains(word) || (isQualified && imports.Contains(qualifier)) {
				pass.Reportf(commentPosition(lines, location[0]), "write %q in a comment as [%s]", word, word)
			}
		}
	}
}

// [maskedCommentText] joins a comment group's lines into one text, with
// everything the bracket check must skip replaced by spaces: the "//"
// markers, directives, code blocks, and text inside brackets or double
// quotes. Brackets and quotes may span lines. Masking works byte by byte, so
// offsets in the text are byte offsets in the comment.
//
// Parameters:
//   - [group]: the comment group to read. Only "//" comments are read; a
//     "/* */" comment is masked whole.
//
// Returns:
//   - [string]: the masked text, one line per comment, joined with line
//     breaks.
//   - [[]commentLine]: where each line of the text starts, in the text and
//     in the file, for [commentPosition].
func maskedCommentText(group *ast.CommentGroup) (string, []commentLine) {
	var text []byte
	var lines []commentLine
	for _, comment := range group.List {
		if len(text) > 0 {
			text = append(text, '\n')
		}
		lines = append(lines, commentLine{offset: len(text), position: comment.Slash})
		line := []byte(comment.Text)
		body := line[min(2, len(line)):]
		isDirective := len(body) > 0 && body[0] != ' ' && body[0] != '\t'
		isCodeBlock := len(body) > 0 && body[0] == '\t'
		if !strings.HasPrefix(comment.Text, "//") || isDirective || isCodeBlock {
			line = bytes.Repeat([]byte(" "), len(line))
		} else {
			line[0], line[1] = ' ', ' '
		}
		text = append(text, line...)
	}
	depth, inQuote := 0, false
	for index, character := range text {
		switch {
		case character == '"' && depth == 0:
			inQuote = !inQuote
			text[index] = ' '
		case character == '[' && !inQuote:
			depth++
			text[index] = ' '
		case character == ']' && !inQuote && depth > 0:
			depth--
			text[index] = ' '
		case (depth > 0 || inQuote) && character != '\n':
			text[index] = ' '
		}
	}
	return string(text), lines
}

// [commentLine] records where one line of a masked comment text starts.
type commentLine struct {
	// [offset] is the byte offset in the text where the line starts.
	offset int
	// [position] is the file position of the line's "//".
	position token.Pos
}

// [commentPosition] turns an offset in the text from [maskedCommentText]
// into a position in the file.
//
// Parameters:
//   - [lines]: where each line of the text starts, from
//     [maskedCommentText].
//   - [offset]: a byte offset in the text.
//
// Returns:
//   - [token.Pos]: the file position of the byte at [offset].
func commentPosition(lines []commentLine, offset int) token.Pos {
	current := lines[0]
	for _, line := range lines {
		if line.offset > offset {
			break
		}
		current = line
	}
	return current.position + token.Pos(offset-current.offset)
}

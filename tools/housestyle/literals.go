package housestyle

import (
	"go/ast"
	"go/token"

	"golang.org/x/tools/go/analysis"
)

// [maximumStringLiterals] is how many string literals a package-level
// constant or variable may hold before it counts as a fixed data list that
// belongs in an embedded file.
const maximumStringLiterals = 2

// [checkDataLiterals] reports package-level constants and variables whose
// value holds more than [maximumStringLiterals] string literals, such as a
// word list written as a map literal. Such lists belong in a text file
// loaded with "//go:embed".
//
// Parameters:
//   - [pass]: where problems are reported.
//   - [file]: the file whose declarations are checked.
func checkDataLiterals(pass *analysis.Pass, file *ast.File) {
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || (general.Tok != token.VAR && general.Tok != token.CONST) {
			continue
		}
		for _, specification := range general.Specs {
			for _, value := range specification.(*ast.ValueSpec).Values {
				if count := countStringLiterals(value); count > maximumStringLiterals {
					pass.Reportf(value.Pos(), "fixed list of %d string literals: move it to a text file and load it with //go:embed", count)
				}
			}
		}
	}
}

// [countStringLiterals] counts the string literals inside a composite
// literal, including nested ones, keys and values.
//
// Parameters:
//   - [expression]: the value of a constant or variable.
//
// Returns:
//   - [int]: the number of string literals in [expression] if it is a
//     composite literal such as a map or slice, and 0 for anything else, such
//     as a single string or a function call.
func countStringLiterals(expression ast.Expr) int {
	composite, isComposite := expression.(*ast.CompositeLit)
	if !isComposite {
		return 0
	}
	count := 0
	ast.Inspect(composite, func(node ast.Node) bool {
		if literal, isLiteral := node.(*ast.BasicLit); isLiteral && literal.Kind == token.STRING {
			count++
		}
		return true
	})
	return count
}

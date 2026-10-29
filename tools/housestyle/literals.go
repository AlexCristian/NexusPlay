package housestyle

import (
	"go/ast"
	"go/token"
	"go/types"

	"golang.org/x/tools/go/analysis"
)

// [maximumStringLiterals] is how many string literals a package-level
// constant or variable may hold in lists before it counts as a fixed data
// list that belongs in an embedded file.
const maximumStringLiterals = 2

// [checkDataLiterals] reports package-level constants and variables whose
// value holds more than [maximumStringLiterals] string literals as list
// items, such as a word list written as a slice or map literal, or a table
// passed to a function such as [strings.NewReplacer]. Such lists belong in a
// text file loaded with "//go:embed". String fields of a single struct
// value, such as an [analysis.Analyzer], are settings rather than a list and
// are not counted.
//
// Parameters:
//   - [pass]: the package being analyzed, and where problems are reported.
//   - [file]: the file whose declarations are checked.
func checkDataLiterals(pass *analysis.Pass, file *ast.File) {
	for _, declaration := range file.Decls {
		general, isGeneral := declaration.(*ast.GenDecl)
		if !isGeneral || (general.Tok != token.VAR && general.Tok != token.CONST) {
			continue
		}
		for _, specification := range general.Specs {
			for _, value := range specification.(*ast.ValueSpec).Values {
				if count := countListStrings(pass, value, false); count > maximumStringLiterals {
					pass.Reportf(value.Pos(), "fixed list of %d string literals: move it to a text file and load it with //go:embed", count)
				}
			}
		}
	}
}

// [countListStrings] counts the string literals in an expression that are
// items of a list: elements or keys of a slice, array or map literal, at any
// depth (including fields of struct values inside such a literal), and
// arguments of function calls. Function literals are code, not data, and
// are not looked into.
//
// Parameters:
//   - [pass]: the package being analyzed, for the types of composite
//     literals.
//   - [expression]: the expression to look into, such as the value of a
//     constant or variable.
//   - [inList]: whether [expression] is already inside a list, so a string
//     literal here counts.
//
// Returns:
//   - [int]: the number of string literals in [expression] that are list
//     items.
func countListStrings(pass *analysis.Pass, expression ast.Expr, inList bool) int {
	switch expression := expression.(type) {
	case *ast.BasicLit:
		if inList && expression.Kind == token.STRING {
			return 1
		}
		return 0
	case *ast.FuncLit:
		return 0
	case *ast.CompositeLit:
		elementsInList := inList || isListType(pass.TypesInfo.TypeOf(expression))
		count := 0
		for _, element := range expression.Elts {
			count += countListStrings(pass, element, elementsInList)
		}
		return count
	case *ast.CallExpr:
		count := 0
		for _, argument := range expression.Args {
			count += countListStrings(pass, argument, true)
		}
		return count
	}
	count := 0
	ast.Inspect(expression, func(node ast.Node) bool {
		if node == expression {
			return true
		}
		if child, isExpression := node.(ast.Expr); isExpression {
			count += countListStrings(pass, child, inList)
		}
		return false
	})
	return count
}

// [isListType] reports whether a composite literal of a type holds a list
// of values rather than the fields of one value.
//
// Parameters:
//   - [literalType]: the type of a composite literal, or [nil] if it is not
//     known.
//
// Returns:
//   - [bool]: [true] for a slice, array or map type, or a pointer to one;
//     [false] for a struct type, any other type, or [nil].
func isListType(literalType types.Type) bool {
	if literalType == nil {
		return false
	}
	if pointer, isPointer := literalType.Underlying().(*types.Pointer); isPointer {
		literalType = pointer.Elem()
	}
	switch literalType.Underlying().(type) {
	case *types.Slice, *types.Array, *types.Map:
		return true
	}
	return false
}

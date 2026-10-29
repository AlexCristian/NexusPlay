package housestyle

import (
	"go/ast"
	"go/types"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/collections"
	"golang.org/x/tools/go/analysis"
)

// [checkReturnedErrors] reports functions and methods that return a
// package-level error variable, such as [store.ErrNotFound], directly from
// a "return" statement, but whose doc comment never mentions it in brackets.
// Callers rely on the doc comment to learn which errors they can match with
// [errors.Is]. Errors that are wrapped, such as with [fmt.Errorf], or passed
// on from a local variable are not checked, and neither are "return"
// statements inside function literals. Functions without a doc comment are
// left to [checkDocComments].
//
// Parameters:
//   - [pass]: the package being analyzed, and where problems are reported.
//   - [file]: the file whose functions are checked.
func checkReturnedErrors(pass *analysis.Pass, file *ast.File) {
	for _, declaration := range file.Decls {
		function, isFunction := declaration.(*ast.FuncDecl)
		if !isFunction || function.Body == nil || function.Doc == nil {
			continue
		}
		documentation := function.Doc.Text()
		reported := collections.NewSet[string]()
		ast.Inspect(function.Body, func(node ast.Node) bool {
			if _, isFunctionLiteral := node.(*ast.FuncLit); isFunctionLiteral {
				return false
			}
			statement, isReturn := node.(*ast.ReturnStmt)
			if !isReturn {
				return true
			}
			for _, result := range statement.Results {
				names := errorVariableNames(pass, result)
				if len(names) == 0 || reported.Contains(names[0]) || mentionsAny(documentation, names) {
					continue
				}
				reported.Add(names[0])
				pass.Reportf(result.Pos(), "returns [%s] but its doc comment does not mention it", names[0])
			}
			return true
		})
	}
}

// [errorVariableNames] decides whether a returned expression is a
// package-level error variable, and how a doc comment may refer to it.
//
// Parameters:
//   - [pass]: the package being analyzed, for what names refer to.
//   - [expression]: one result of a "return" statement.
//
// Returns:
//   - [[]string]: the names a doc comment may use for the variable, the
//     first being the one to use in messages: "ErrNoMatch" and
//     "metadata.ErrNoMatch" for a variable of the package itself, or just
//     "store.ErrNotFound" for one of another package, qualified the way the
//     file imports it. [nil] if [expression] is anything else, such as a
//     local variable, a call or [nil].
func errorVariableNames(pass *analysis.Pass, expression ast.Expr) []string {
	expression = ast.Unparen(expression)
	var identifier *ast.Ident
	qualifier := ""
	switch expression := expression.(type) {
	case *ast.Ident:
		identifier = expression
	case *ast.SelectorExpr:
		packageIdentifier, isIdentifier := expression.X.(*ast.Ident)
		if !isIdentifier {
			return nil
		}
		if _, isPackage := pass.TypesInfo.Uses[packageIdentifier].(*types.PkgName); !isPackage {
			return nil
		}
		identifier, qualifier = expression.Sel, packageIdentifier.Name
	default:
		return nil
	}
	variable, isVariable := pass.TypesInfo.Uses[identifier].(*types.Var)
	if !isVariable || variable.Pkg() == nil || variable.Parent() != variable.Pkg().Scope() {
		return nil
	}
	if !types.Implements(variable.Type(), errorInterface) {
		return nil
	}
	if qualifier != "" {
		return []string{qualifier + "." + identifier.Name}
	}
	return []string{identifier.Name, variable.Pkg().Name() + "." + identifier.Name}
}

// [errorInterface] is the predeclared [error] interface, which every
// returned error variable must implement to be checked.
var errorInterface = types.Universe.Lookup("error").Type().Underlying().(*types.Interface)

// [mentionsAny] reports whether a doc comment mentions any of the given
// names in brackets.
//
// Parameters:
//   - [documentation]: the doc comment's text.
//   - [names]: the names to look for, without brackets.
//
// Returns:
//   - [bool]: [true] if [documentation] contains at least one of [names]
//     between "[" and "]"; [false] otherwise.
func mentionsAny(documentation string, names []string) bool {
	for _, name := range names {
		if strings.Contains(documentation, "["+name+"]") {
			return true
		}
	}
	return false
}

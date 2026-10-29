package housestyle

import (
	"go/ast"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// [collectionsPath] is the import path of the package that defines
// [collections.Set], the one place allowed to build a set out of a map.
const collectionsPath = "github.com/AlexCristian/NexusPlay/internal/collections"

// [checkSetMaps] reports map types used as sets: a map whose values are
// [bool] or "struct{}". Such code should use [collections.Set] instead, so
// readers see a set rather than a map whose values mean nothing.
//
// Parameters:
//   - [pass]: the package being analyzed, and where problems are reported.
//   - [file]: the file whose type expressions are checked.
func checkSetMaps(pass *analysis.Pass, file *ast.File) {
	if pass.Pkg.Path() == collectionsPath || strings.HasPrefix(pass.Pkg.Path(), collectionsPath+"_") {
		return
	}
	ast.Inspect(file, func(node ast.Node) bool {
		mapType, isMapType := node.(*ast.MapType)
		if !isMapType {
			return true
		}
		valueType := pass.TypesInfo.TypeOf(mapType.Value)
		if valueType == nil {
			return true
		}
		structure, isStruct := valueType.Underlying().(*types.Struct)
		isEmptyStruct := isStruct && structure.NumFields() == 0
		if types.Identical(valueType, types.Typ[types.Bool]) || isEmptyStruct {
			pass.Reportf(mapType.Pos(), "%s is a set written as a map: use [collections.Set] instead", expressionText(pass.Fset, mapType))
		}
		return true
	})
}

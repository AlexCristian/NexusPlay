package housestyle

import (
	"go/ast"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// [checkFileLayout] reports exported types that are not in a file named
// after them, and methods of exported types that are not in their type's
// file. [metadata.Episode] belongs in episode.go, and [ConversionView] in
// conversion_view.go.
//
// Parameters:
//   - [pass]: where problems are reported.
//   - [file]: the file whose declarations are checked.
//   - [fileName]: the path of [file].
func checkFileLayout(pass *analysis.Pass, file *ast.File, fileName string) {
	baseName := filepath.Base(fileName)
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.GenDecl:
			for _, specification := range declaration.Specs {
				typeSpecification, isType := specification.(*ast.TypeSpec)
				if !isType || !typeSpecification.Name.IsExported() {
					continue
				}
				if want := typeFileName(typeSpecification.Name.Name); baseName != want {
					pass.Reportf(typeSpecification.Name.Pos(), "exported type [%s] should be in its own file, %s", typeSpecification.Name.Name, want)
				}
			}
		case *ast.FuncDecl:
			typeName := receiverTypeName(declaration)
			if !ast.IsExported(typeName) {
				continue
			}
			if want := typeFileName(typeName); baseName != want {
				pass.Reportf(declaration.Name.Pos(), "method [%s.%s] should be in %s with [%s]", typeName, declaration.Name.Name, want, typeName)
			}
		}
	}
}

// [typeFileName] gets the file name an exported type belongs in.
//
// Parameters:
//   - [typeName]: the type's name, such as "Episode" or "HTTPClient".
//
// Returns:
//   - [string]: the name's words joined with underscores, plus ".go", such
//     as "episode.go" or "http_client.go".
func typeFileName(typeName string) string {
	return strings.Join(splitWords(typeName), "_") + ".go"
}

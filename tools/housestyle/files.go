package housestyle

import (
	"go/ast"
	"go/types"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// [checkFileLayout] reports exported types that are not in a file named
// after them, and methods of exported types that are neither in their
// type's file nor in a file whose name starts with it and an underscore.
// [metadata.Episode] belongs in episode.go, and [ConversionView] in
// conversion_view.go; methods of a [store.Store] may also be split into
// store_media.go and store_users.go, but not session_store.go.
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
				if valueSpecification, isValue := specification.(*ast.ValueSpec); isValue {
					checkErrorFile(pass, valueSpecification, baseName)
					continue
				}
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
			want := typeFileName(typeName)
			stem := strings.TrimSuffix(want, ".go")
			if baseName != want && !strings.HasPrefix(baseName, stem+"_") {
				pass.Reportf(declaration.Name.Pos(), "method [%s.%s] should be in %s or a %s_*.go file, with [%s]", typeName, declaration.Name.Name, want, stem, typeName)
			}
		}
	}
}

// [checkErrorFile] reports exported sentinel errors, package-level variables
// of type [error] whose name starts with "Err", that are not in errors.go.
//
// Parameters:
//   - [pass]: where problems are reported.
//   - [specification]: a package-level "var" or "const" entry.
//   - [baseName]: the name of the file it is declared in.
func checkErrorFile(pass *analysis.Pass, specification *ast.ValueSpec, baseName string) {
	if baseName == "errors.go" {
		return
	}
	for _, name := range specification.Names {
		object := pass.TypesInfo.Defs[name]
		if object == nil || !name.IsExported() || !strings.HasPrefix(name.Name, "Err") {
			continue
		}
		if object.Parent() == pass.Pkg.Scope() && types.Identical(object.Type(), types.Universe.Lookup("error").Type()) {
			pass.Reportf(name.Pos(), "sentinel error [%s] should be in errors.go", name.Name)
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

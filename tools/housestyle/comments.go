package housestyle

import (
	"bytes"
	"go/ast"
	"go/printer"
	"go/token"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// [listItem] matches one entry of a "Parameters:" or "Returns:" list, such
// as "  - [episodes]: every episode…" or "  - [int64] (start): …". The first
// group is the bracketed name or type; the second is the optional label.
var listItem = regexp.MustCompile(`^\s*- \[(.+?)\]( \([^)]*\))?:`)

// [docSection] is one "Parameters:" or "Returns:" list of a doc comment.
type docSection struct {
	// [found] reports whether the doc comment has the section's heading.
	found bool
	// [entries] holds the bracketed name or type that starts each entry, in
	// order.
	entries []string
}

// [checkDocComments] reports declarations in a file whose doc comment is
// missing, does not start with the declared name in brackets, or has wrong
// "Parameters:" or "Returns:" lists.
//
// Parameters:
//   - [pass]: the package being analyzed, and where problems are reported.
//   - [file]: the file whose declarations are checked.
func checkDocComments(pass *analysis.Pass, file *ast.File) {
	if file.Doc != nil {
		checkPackageDoc(pass, file)
	}
	for _, declaration := range file.Decls {
		switch declaration := declaration.(type) {
		case *ast.FuncDecl:
			checkFunctionDoc(pass, declaration)
		case *ast.GenDecl:
			checkGeneralDeclarationDoc(pass, declaration)
		}
	}
}

// [checkPackageDoc] reports a package comment that does not start with the
// package's name in brackets: "Package [metadata] …", or for a command
// (package main), "Command [housestyle] …" with the command's folder name.
//
// Parameters:
//   - [pass]: where problems are reported.
//   - [file]: a file with a package comment.
func checkPackageDoc(pass *analysis.Pass, file *ast.File) {
	want := "Package [" + file.Name.Name + "]"
	if file.Name.Name == "main" {
		folder := filepath.Base(filepath.Dir(pass.Fset.File(file.Pos()).Name()))
		want = "Command [" + folder + "]"
	}
	if !strings.HasPrefix(file.Doc.Text(), want) {
		pass.Reportf(file.Doc.Pos(), "package comment should start with %q", want)
	}
}

// [checkFunctionDoc] reports problems with a function's or method's doc
// comment: a missing comment, a missing bracketed name at its start, and
// "Parameters:" or "Returns:" lists that do not match the signature.
//
// Parameters:
//   - [pass]: the package being analyzed, and where problems are reported.
//   - [function]: the function or method declaration to check.
func checkFunctionDoc(pass *analysis.Pass, function *ast.FuncDecl) {
	name := function.Name.Name
	if !checkLeadingName(pass, function.Name.Pos(), function.Doc, name, receiverTypeName(function)+"."+name) {
		return
	}
	parameters, returns := docSections(function.Doc.Text())

	var parameterNames []string
	for _, field := range function.Type.Params.List {
		for _, fieldName := range field.Names {
			if fieldName.Name != "_" {
				parameterNames = append(parameterNames, fieldName.Name)
			}
		}
	}
	checkSection(pass, function.Name.Pos(), "Parameters", "parameter", parameters, parameterNames)

	var returnTypes []string
	if function.Type.Results != nil {
		for _, field := range function.Type.Results.List {
			typeText := expressionText(pass.Fset, field.Type)
			for range max(1, len(field.Names)) {
				returnTypes = append(returnTypes, typeText)
			}
		}
	}
	checkSection(pass, function.Name.Pos(), "Returns", "return value", returns, returnTypes)
}

// [checkSection] compares one "Parameters:" or "Returns:" list with what the
// signature says it should hold, and reports every difference.
//
// Parameters:
//   - [pass]: where problems are reported.
//   - [position]: where in the file to report them, the function's name.
//   - [heading]: the section's heading without its colon, "Parameters" or
//     "Returns".
//   - [kind]: what each entry describes, "parameter" or "return value", for
//     messages.
//   - [section]: the section as written in the doc comment.
//   - [want]: the entries the signature calls for, in order: parameter names
//     for "Parameters", type expressions for "Returns".
func checkSection(pass *analysis.Pass, position token.Pos, heading, kind string, section docSection, want []string) {
	switch {
	case len(want) == 0 && section.found:
		pass.Reportf(position, "doc comment has a %q list, but there is no %s", heading+":", kind)
		return
	case len(want) == 0:
		return
	case !section.found:
		pass.Reportf(position, "doc comment is missing the %q list: %s", heading+":", bracketAll(want))
		return
	}
	for index, wanted := range want {
		if index >= len(section.entries) {
			pass.Reportf(position, "%q list is missing %s %d, [%s]", heading+":", kind, index+1, wanted)
			continue
		}
		if section.entries[index] != wanted {
			pass.Reportf(position, "%q entry %d is [%s] but should be [%s]", heading+":", index+1, section.entries[index], wanted)
		}
	}
	for _, extra := range section.entries[min(len(want), len(section.entries)):] {
		pass.Reportf(position, "%q list has an extra entry [%s]", heading+":", extra)
	}
}

// [checkGeneralDeclarationDoc] reports types, struct fields, interface
// methods, constants and variables whose doc comment is missing or does not
// start with their name in brackets. Imports are not checked.
//
// Parameters:
//   - [pass]: the package being analyzed, and where problems are reported.
//   - [declaration]: a "type", "const" or "var" declaration, alone or as a
//     group in parentheses.
func checkGeneralDeclarationDoc(pass *analysis.Pass, declaration *ast.GenDecl) {
	for _, specification := range declaration.Specs {
		switch specification := specification.(type) {
		case *ast.TypeSpec:
			documentation := specificationDoc(declaration, specification.Doc)
			checkLeadingName(pass, specification.Name.Pos(), documentation, specification.Name.Name)
			switch typeExpression := specification.Type.(type) {
			case *ast.StructType:
				checkFieldDocs(pass, typeExpression.Fields)
			case *ast.InterfaceType:
				checkFieldDocs(pass, typeExpression.Methods)
			}
		case *ast.ValueSpec:
			documentation := specificationDoc(declaration, specification.Doc)
			var names []string
			for _, name := range specification.Names {
				names = append(names, name.Name)
			}
			checkLeadingName(pass, specification.Names[0].Pos(), documentation, names...)
		}
	}
}

// [checkFieldDocs] reports struct fields and interface methods whose doc
// comment is missing or does not start with their name in brackets. An
// embedded field's name is its type's name.
//
// Parameters:
//   - [pass]: where problems are reported.
//   - [fields]: the fields of a struct type or the methods of an interface
//     type.
func checkFieldDocs(pass *analysis.Pass, fields *ast.FieldList) {
	for _, field := range fields.List {
		var names []string
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
		if len(names) == 0 {
			names = append(names, embeddedTypeName(field.Type))
		}
		checkLeadingName(pass, field.Pos(), field.Doc, names...)
	}
}

// [checkLeadingName] reports a doc comment that is missing or does not start
// with one of the given names in brackets.
//
// Parameters:
//   - [pass]: where problems are reported.
//   - [position]: where in the file to report them, the declared name.
//   - [documentation]: the doc comment, or [nil] if there is none.
//   - [names]: the names the comment may start with, such as "Movie" and
//     "Client.Movie" for a method. The first one is used in messages.
//
// Returns:
//   - [bool]: [true] if the comment exists, even if it starts wrongly, so
//     callers can go on to check the rest of it; [false] if it is missing.
func checkLeadingName(pass *analysis.Pass, position token.Pos, documentation *ast.CommentGroup, names ...string) bool {
	if documentation == nil {
		pass.Reportf(position, "[%s] has no doc comment", names[0])
		return false
	}
	text := documentation.Text()
	for _, name := range names {
		if strings.HasPrefix(text, "["+name+"]") {
			return true
		}
	}
	pass.Reportf(position, "doc comment should start with [%s]", names[0])
	return true
}

// [specificationDoc] finds the doc comment of one entry of a "type",
// "const" or "var" declaration.
//
// Parameters:
//   - [declaration]: the whole declaration the entry belongs to.
//   - [own]: the entry's own doc comment, or [nil] if it has none.
//
// Returns:
//   - [*ast.CommentGroup]: [own] if there is one; otherwise the
//     declaration's doc comment if the declaration has only this entry and
//     no parentheses; otherwise [nil].
func specificationDoc(declaration *ast.GenDecl, own *ast.CommentGroup) *ast.CommentGroup {
	if own != nil {
		return own
	}
	if len(declaration.Specs) == 1 && !declaration.Lparen.IsValid() {
		return declaration.Doc
	}
	return nil
}

// [docSections] finds the "Parameters:" and "Returns:" lists in a doc
// comment. A list starts at its heading line and ends at the next blank line
// or heading.
//
// Parameters:
//   - [text]: the doc comment's text, as [ast.CommentGroup.Text] returns it.
//
// Returns:
//   - [docSection] (parameters): the "Parameters:" list.
//   - [docSection] (returns): the "Returns:" list.
func docSections(text string) (docSection, docSection) {
	var parameters, returns docSection
	var current *docSection
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "Parameters:":
			parameters.found, current = true, &parameters
		case trimmed == "Returns:":
			returns.found, current = true, &returns
		case trimmed == "":
			current = nil
		case current != nil:
			if match := listItem.FindStringSubmatch(line); match != nil {
				current.entries = append(current.entries, match[1])
			}
		}
	}
	return parameters, returns
}

// [receiverTypeName] gets the name of the type a method belongs to.
//
// Parameters:
//   - [function]: a function or method declaration.
//
// Returns:
//   - [string]: the receiver's type name without "*" or type parameters,
//     such as "Client" for "func (client *Client) Movie", or "" for a plain
//     function.
func receiverTypeName(function *ast.FuncDecl) string {
	if function.Recv == nil || len(function.Recv.List) == 0 {
		return ""
	}
	return embeddedTypeName(function.Recv.List[0].Type)
}

// [embeddedTypeName] gets the bare name of a type, as used for embedded
// fields and receivers.
//
// Parameters:
//   - [expression]: a type expression such as "Info", "*Client",
//     "metadata.Info" or "List[T]".
//
// Returns:
//   - [string]: the type's name without package, "*" or type parameters,
//     such as "Info", "Client" or "List", or "" for any other expression.
func embeddedTypeName(expression ast.Expr) string {
	switch expression := expression.(type) {
	case *ast.Ident:
		return expression.Name
	case *ast.StarExpr:
		return embeddedTypeName(expression.X)
	case *ast.SelectorExpr:
		return expression.Sel.Name
	case *ast.IndexExpr:
		return embeddedTypeName(expression.X)
	case *ast.IndexListExpr:
		return embeddedTypeName(expression.X)
	}
	return ""
}

// [expressionText] prints an expression the way it appears in the source.
//
// Parameters:
//   - [fileSet]: the file set [expression] was parsed with.
//   - [expression]: the expression to print, such as a result type.
//
// Returns:
//   - [string]: the expression's text in gofmt style, such as
//     "metadata.Episode", "[]Episode" or "map[string]bool".
func expressionText(fileSet *token.FileSet, expression ast.Expr) string {
	var buffer bytes.Buffer
	if err := printer.Fprint(&buffer, fileSet, expression); err != nil {
		return "?" + strconv.Quote(err.Error())
	}
	return buffer.String()
}

// [bracketAll] writes names as a bracketed, comma-separated list for a
// message.
//
// Parameters:
//   - [names]: the names to list.
//
// Returns:
//   - [string]: each of [names] in brackets, separated by commas, such as
//     "[episodes], [number]".
func bracketAll(names []string) string {
	bracketed := make([]string, len(names))
	for index, name := range names {
		bracketed[index] = "[" + name + "]"
	}
	return strings.Join(bracketed, ", ")
}

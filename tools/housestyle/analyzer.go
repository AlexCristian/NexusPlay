package housestyle

import (
	"go/ast"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// [Analyzer] reports code that breaks the NexusPlay house style. Run it with
// the housestyle command, or pass it to any [analysis] driver.
var Analyzer = &analysis.Analyzer{
	Name: "housestyle",
	Doc:  "reports code that breaks the NexusPlay house style in CLAUDE.md",
	Run:  run,
}

// [run] checks every hand-written file of one package. Generated files are
// skipped. Test files get only the naming check: doc comments, returned
// errors, brackets, file layout, data literals and sets are not checked in
// them.
//
// Parameters:
//   - [pass]: the package being analyzed, and where problems are reported.
//
// Returns:
//   - [any]: always [nil]; [Analyzer] produces no facts or results for other
//     analyzers.
//   - [error]: always [nil]; every problem is reported through [pass]
//     instead.
func run(pass *analysis.Pass) (any, error) {
	candidates := bracketCandidates(pass)
	for _, file := range pass.Files {
		if ast.IsGenerated(file) {
			continue
		}
		checkNames(pass, file)
		fileName := pass.Fset.File(file.Pos()).Name()
		if strings.HasSuffix(fileName, "_test.go") {
			continue
		}
		checkDocComments(pass, file)
		checkReturnedErrors(pass, file)
		checkBrackets(pass, file, candidates)
		checkFileLayout(pass, file, fileName)
		checkDataLiterals(pass, file)
		checkSetMaps(pass, file)
	}
	return nil, nil
}

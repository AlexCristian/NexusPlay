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

// [run] checks every hand-written, non-test file of one package.
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
	candidates := bracketCandidates(pass.Pkg)
	for _, file := range pass.Files {
		fileName := pass.Fset.File(file.Pos()).Name()
		if strings.HasSuffix(fileName, "_test.go") || ast.IsGenerated(file) {
			continue
		}
		checkNames(pass, file)
		checkDocComments(pass, file)
		checkBrackets(pass, file, candidates)
		checkFileLayout(pass, file, fileName)
		checkDataLiterals(pass, file)
		checkSetMaps(pass, file)
	}
	return nil, nil
}

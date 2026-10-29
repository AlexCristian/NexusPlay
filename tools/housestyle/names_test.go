package housestyle

import (
	"go/types"
	"slices"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestCheckNames(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "names")
}

// TestNameProblemFullWords checks that full words which start with a banned
// abbreviation are not flagged, because names are compared word by word.
func TestNameProblemFullWords(t *testing.T) {
	for _, name := range []string{"subtitle", "extension", "conversion", "process", "command", "memory", "address", "seconds", "template", "environment", "subtitleTrack", "ExtensionList"} {
		if problem := nameProblem(name, types.Typ[types.Int]); problem != "" {
			t.Errorf("nameProblem(%q) = %q, want no problem", name, problem)
		}
	}
	for _, name := range []string{"subTrack", "fileExt", "convJob", "procID", "memLimit", "listenAddr", "timeoutSec", "pageTmpl", "envName", "cmdArgs"} {
		if problem := nameProblem(name, types.Typ[types.Int]); problem == "" {
			t.Errorf("nameProblem(%q) found no problem, want an abbreviation", name)
		}
	}
}

func TestSplitWords(t *testing.T) {
	cases := []struct {
		name string
		want []string
	}{
		{"tempFile", []string{"temp", "file"}},
		{"HTTPClient", []string{"http", "client"}},
		{"show_id", []string{"show", "id"}},
		{"URLs", []string{"urls"}},
		{"showIDsByName", []string{"show", "ids", "by", "name"}},
		{"HTTPServer", []string{"http", "server"}},
		{"season2Episodes", []string{"season2", "episodes"}},
		{"ID", []string{"id"}},
		{"x", []string{"x"}},
	}
	for _, testCase := range cases {
		if got := splitWords(testCase.name); !slices.Equal(got, testCase.want) {
			t.Errorf("splitWords(%q) = %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

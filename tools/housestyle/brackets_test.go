package housestyle

import (
	"bytes"
	"io/fs"
	"os"
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestCheckBrackets(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "brackets")
}

// TestCheckBracketsColumn checks that a word after a multi-byte character
// is reported at its own column, not shifted by the character's extra
// bytes.
func TestCheckBracketsColumn(t *testing.T) {
	results := analysistest.Run(t, analysistest.TestData(), Analyzer, "brackets")
	fileName := "src/brackets/brackets.go"
	source, err := fs.ReadFile(os.DirFS(analysistest.TestData()), fileName)
	if err != nil {
		t.Fatal(err)
	}
	marker := []byte("character… localCount")
	offset := bytes.Index(source, marker)
	if offset < 0 {
		t.Fatalf("%s has no %q", fileName, marker)
	}
	wantOffset := offset + len(marker) - len("localCount")
	found := false
	for _, result := range results {
		for _, diagnostic := range result.Diagnostics {
			position := result.Pass.Fset.Position(diagnostic.Pos)
			if bytes.Contains([]byte(diagnostic.Message), []byte(`"localCount"`)) {
				found = true
				if position.Offset != wantOffset {
					t.Errorf("localCount reported at offset %d, want %d", position.Offset, wantOffset)
				}
			}
		}
	}
	if !found {
		t.Error("localCount was not reported")
	}
}

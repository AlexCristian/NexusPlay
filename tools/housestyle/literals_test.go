package housestyle

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestCheckDataLiterals(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "literals")
}

package housestyle

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestCheckReturnedErrors(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "returnederrors")
}

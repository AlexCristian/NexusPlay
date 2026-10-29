package housestyle

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestCheckDocComments(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "comments", "packagecomment", "command", "badcommand")
}

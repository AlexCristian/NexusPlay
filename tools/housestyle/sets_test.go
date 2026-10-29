package housestyle

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestCheckSetMaps(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "sets", "github.com/AlexCristian/NexusPlay/internal/collections")
}

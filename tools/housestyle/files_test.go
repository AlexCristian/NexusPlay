package housestyle

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// TestCheckFileLayout also covers what every check skips: the generated
// file and the test file in testdata/src/files break every rule, and
// nothing may be reported for them.
func TestCheckFileLayout(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), Analyzer, "files")
}

func TestTypeFileName(t *testing.T) {
	cases := []struct {
		typeName string
		want     string
	}{
		{"Episode", "episode.go"},
		{"HTTPClient", "http_client.go"},
		{"ConversionView", "conversion_view.go"},
		{"URLs", "urls.go"},
		{"TMDBShowIDs", "tmdb_show_ids.go"},
	}
	for _, testCase := range cases {
		if got := typeFileName(testCase.typeName); got != testCase.want {
			t.Errorf("typeFileName(%q) = %q, want %q", testCase.typeName, got, testCase.want)
		}
	}
}

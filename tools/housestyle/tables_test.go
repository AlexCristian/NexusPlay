package housestyle

import "testing"

func TestEmbeddedTablesLoaded(t *testing.T) {
	if replacement := abbreviations["eps"]; replacement != "episodes" {
		t.Errorf(`abbreviations["eps"] = %q, want "episodes"`, replacement)
	}
	if allowedType := shortNames["ctx"]; allowedType != "context.Context" {
		t.Errorf(`shortNames["ctx"] = %q, want "context.Context"`, allowedType)
	}
	if allowedType := shortNames["t"]; allowedType != "*testing.T" {
		t.Errorf(`shortNames["t"] = %q, want "*testing.T"`, allowedType)
	}
	if !codeWords.Contains("nil") {
		t.Error(`codeWords does not contain "nil"`)
	}
	for key := range abbreviations {
		if key == "" || abbreviations[key] == "" {
			t.Errorf("abbreviations has an incomplete entry %q: %q", key, abbreviations[key])
		}
	}
}

func TestReadTable(t *testing.T) {
	table := readTable("# comment\n\n  Key  value \nlone\n")
	if len(table) != 2 || table["key"] != "value" || table["lone"] != "" {
		t.Errorf("readTable = %q, want key: value and lone: empty", table)
	}
}

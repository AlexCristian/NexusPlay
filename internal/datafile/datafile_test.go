package datafile

import "testing"

func TestLines(t *testing.T) {
	got := Lines("# a comment\n\n  one \ntwo\r\n")
	if len(got) != 2 || got[0] != "one" || got[1] != "two" {
		t.Errorf("Lines = %q; want one and two", got)
	}
}

func TestSet(t *testing.T) {
	got := Set("srt\nass\n")
	if got.Len() != 2 || !got.Contains("srt") || got.Contains("SRT") {
		t.Errorf("Set kept %d entries; want srt and ass, case-sensitive", got.Len())
	}
}

func TestTable(t *testing.T) {
	got := Table("test.txt", "# comment\n.mp4\tvideo/mp4\nscan   Scan started. New files appear.\n")
	if got[".mp4"] != "video/mp4" || got["scan"] != "Scan started. New files appear." || len(got) != 2 {
		t.Errorf("Table = %q", got)
	}
}

func TestTablePanicsOnMissingValue(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Table accepted a line with no value")
		}
	}()
	Table("test.txt", "key-only\n")
}

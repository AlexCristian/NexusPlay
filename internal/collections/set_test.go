package collections

import "testing"

func TestSet(t *testing.T) {
	set := NewSet("the", "a", "the")
	if set.Size() != 2 {
		t.Errorf("Len = %d; want 2, duplicates stored once", set.Size())
	}
	set.Add("of")
	for _, word := range []string{"the", "a", "of"} {
		if !set.Contains(word) {
			t.Errorf("Contains(%q) = false; want true", word)
		}
	}
	if set.Contains("part") {
		t.Error(`Contains("part") = true; want false`)
	}
	count := 0
	for range set.All() {
		count++
	}
	if count != 3 {
		t.Errorf("All listed %d values; want 3", count)
	}
}

package names

import "testing"

func TestNames(t *testing.T) {
	tc := 1        // want "name .tc. is too short"
	var cfg string // want "name .cfg. uses the abbreviation .cfg.: write .config. instead"
	words := map[string]bool{"one": true, "two": true, "three": true}
	_, _, _ = tc, cfg, words
	t.Run("subtest", func(t *testing.T) {})
}

func BenchmarkNames(b *testing.B) {}

func FuzzNames(f *testing.F) {}

func TestMain(m *testing.M) {}

func helper(t testing.TB, x *testing.T) {} // want "name .t. is only allowed for a value of type .testing.T" "name .x. is too short"

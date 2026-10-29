package files

import "testing"

// Test files are not checked for layout, literals, sets, brackets or doc
// comments, so HelperType and its method may be here.
type TestHelper struct{}

func (helper TestHelper) Check(t *testing.T) {
	words := map[string]bool{"one": true, "two": true, "three": true}
	_ = words
}

var table = []string{"one", "two", "three"}

func helperFunction() error { return errExample }

var errExample error

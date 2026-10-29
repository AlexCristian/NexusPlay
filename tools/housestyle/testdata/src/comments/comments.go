// Package [comments] holds declarations for the doc comment check.
package comments

import (
	_ "embed"
	"io"
)

// [episode] is a well-documented type.
type episode struct {
	// [number] is documented.
	number int
	title  string // want "\\[title\\] has no doc comment"
	// The season is documented, but does not start with its name.
	season int // want "doc comment should start with \\[season\\]"
	// [first] and [second] share one comment, which may start with either.
	first, second int
	// [Reader] is an embedded field, named after its type.
	io.Reader
	io.Writer // want "\\[Writer\\] has no doc comment"
}

type undocumented struct{} // want "\\[undocumented\\] has no doc comment"

// The type below does not start with its name.
type misnamed struct{} // want "doc comment should start with \\[misnamed\\]"

func noComment() {} // want "\\[noComment\\] has no doc comment"

// [heading] may start with the method's name.
//
// Returns:
//   - [string]: the title.
func (episode *episode) heading() string { return "" }

// [episode.label] may start with the qualified method's name.
//
// Returns:
//   - [string]: the label.
func (episode *episode) label() string { return "" }

// [find] has every list right, including nested brackets and labels.
//
// Parameters:
//   - [episodes]: the episodes.
//   - [words]: the words.
//
// Returns:
//   - [[]episode]: the matches.
//   - [map[string]int]: the words seen.
//   - [int64] (start): where it starts.
//   - [int64] (end): where it ends.
func find(episodes []episode, words map[string]int) ([]episode, map[string]int, int64, int64) {
	return nil, nil, 0, 0
}

// [named] has named results, one entry each.
//
// Returns:
//   - [int] (start): where it starts.
//   - [int] (end): where it ends.
//   - [error]: what went wrong.
func named() (start, end int, err error) { return 0, 0, nil }

// [missingLists] has no lists at all.
func missingLists(count int) bool { return false } // want "doc comment is missing the \"Parameters:\" list: \\[count\\]" "doc comment is missing the \"Returns:\" list: \\[bool\\]"

// [wrongEntries] names the wrong parameter and type.
//
// Parameters:
//   - [number]: the count.
//
// Returns:
//   - [int]: the result.
//   - [error]: an extra entry.
func wrongEntries(count int) bool { return false } // want "\"Parameters:\" entry 1 is \\[number\\] but should be \\[count\\]" "\"Returns:\" entry 1 is \\[int\\] but should be \\[bool\\]" "\"Returns:\" list has an extra entry \\[error\\]"

// [missingEntry] leaves out its second parameter.
//
// Parameters:
//   - [first]: the first.
func missingEntry(first, second int) {} // want "\"Parameters:\" list is missing parameter 2, \\[second\\]"

// [extraList] has a list but no parameters.
//
// Parameters:
//   - [nothing]: there is nothing.
func extraList() {} // want "doc comment has a \"Parameters:\" list, but there is no parameter"

// [ignored] has parameters that need no entry: "_" and unnamed ones.
func ignored(_ int, _ string) {}

// [unnamed] has unnamed parameters.
func unnamed(int, string) {}

// [generic] needs no entries for its type parameters.
//
// Parameters:
//   - [values]: the values.
//
// Returns:
//   - [[]T]: [values], unchanged.
func generic[T any](values []T) []T { return values }

// [list] is a generic type.
type list[T any] struct {
	// [items] holds the list's items.
	items []T
}

// [Len] is a method of a generic type.
//
// Returns:
//   - [int]: how many items [list] holds.
func (list *list[T]) Len() int { return len(list.items) }

// [inlined] has a directive after its doc comment.
//
//go:noinline
func inlined() {}

//go:noinline
func directiveOnly() {} // want "\\[directiveOnly\\] has no doc comment"

// [data] is built in from a file.
//
//go:embed data.txt
var data string

//go:embed data.txt
var directiveData string // want "\\[directiveData\\] has no doc comment"

// [limit] is a lone constant whose declaration's comment counts.
const limit = 3

// The group's comment does not count for its entries.
const (
	// [small] is documented.
	small = 1
	large = 2 // want "\\[large\\] has no doc comment"
)

// The group's comment does not count for its entries.
var (
	// [left] and [right] share a comment.
	left, right = 1, 2
	// The middle is misnamed.
	middle = 3 // want "doc comment should start with \\[middle\\]"
)

// A blank variable checks that a type implements an interface; it needs no
// comment of its own.
var _ io.Reader = (*episode)(nil)

// [Read] implements [io.Reader].
//
// Parameters:
//   - [buffer]: where to read into.
//
// Returns:
//   - [int]: how many bytes were read.
//   - [error]: always [io.EOF].
func (episode *episode) Read(buffer []byte) (int, error) { return 0, io.EOF }

// [fetcher] is an interface with documented methods.
type fetcher interface {
	// [Fetch] has its lists.
	//
	// Parameters:
	//   - [address]: where to fetch from.
	//
	// Returns:
	//   - [error]: what went wrong.
	Fetch(address string) error
	// [fetcher.Close] may use the qualified name.
	Close()
	// [Count] is missing its list.
	Count() int // want "doc comment is missing the \"Returns:\" list: \\[int\\]"
	Stop()      // want "\\[Stop\\] has no doc comment"
	// [Reader] is an embedded interface.
	io.Reader
}

// [number] is a type constraint; its union element needs no comment.
type number interface {
	~int | ~int64
}

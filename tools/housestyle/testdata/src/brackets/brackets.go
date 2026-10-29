// Package [brackets] holds comments for the bracket check.
package brackets

import (
	"math/rand/v2"
	"strings"
)

// [showInfo] is a show.
type showInfo struct {
	// [posterDirectory] is a camelCase field.
	posterDirectory string
	// [Title] is an exported field.
	Title string
}

// [describe] mentions code in many ways. These are fine: [[]showInfo],
// [map[string]showInfo], [showInfo.Title], [nil], "nil", similarity, ID,
// "posterDirectory spread over
// two lines", and the file strings.txt.
//
// These are not: posterDirectory, showInfo, and nil. // want "write .posterDirectory. in a comment as \\[posterDirectory\\]" "write .showInfo. in a comment as \\[showInfo\\]" "write .nil. in a comment as \\[nil\\]"
// So are showInfo.Title and strings.Builder. // want "write .showInfo.Title. in a comment as \\[showInfo.Title\\]" "write .strings.Builder. in a comment as \\[strings.Builder\\]"
// So are brackets.showInfo and rand.IntN. // want "write .brackets.showInfo. in a comment as \\[brackets.showInfo\\]" "write .rand.IntN. in a comment as \\[rand.IntN\\]"
// After a wide character… localCount is still found. // want "write .localCount. in a comment as \\[localCount\\]"
//
// A code block is skipped:
//
//	localCount := len(posterDirectory)
//
// Parameters:
//   - [info]: the show.
//
// Returns:
//   - [int]: a number.
func describe(info showInfo) int {
	localCount := rand.IntN(len(info.posterDirectory))
	var builder strings.Builder
	_ = builder
	return localCount
}

// [generated] has a directive line that is skipped.
//
//go:generate echo posterDirectory showInfo
func generated() {}

/* A block comment mentions showInfo, but block comments are skipped. */

// This comment has "Title" in quotes. A plain English word
// that happens to be a declared name at the start of a sentence is still
// flagged when a declaration has that name:
// Title case. // want "write .Title. in a comment as \\[Title\\]"

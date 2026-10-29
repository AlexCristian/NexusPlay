// Package [returnederrors] holds functions for the returned error check.
package returnederrors

import (
	"errors"
	"fmt"
	"io"
	database "store"
)

// [ErrNoMatch] means nothing matched.
var ErrNoMatch = errors.New("no match")

// [errHidden] is an unexported sentinel.
var errHidden = errors.New("hidden")

// [customError] is an error type.
type customError struct{}

// [Error] describes the error.
//
// Returns:
//   - [string]: the description.
func (problem *customError) Error() string { return "custom" }

// [errCustom] is a sentinel of a custom error type.
var errCustom = &customError{}

// [notAnError] is a package-level variable that is not an error.
var notAnError = 0

// [info] is a result.
type info struct{}

// [findDocumented] mentions its sentinel.
//
// Returns:
//   - [info]: the match.
//   - [error]: [ErrNoMatch] if nothing matched.
func findDocumented() (info, error) {
	return info{}, ErrNoMatch
}

// [findQualifiedMention] mentions its sentinel by its qualified name.
//
// Returns:
//   - [error]: [returnederrors.ErrNoMatch] if nothing matched.
func findQualifiedMention() error {
	return ErrNoMatch
}

// [findUndocumented] returns sentinels without saying so.
//
// Parameters:
//   - [which]: which error to return.
//
// Returns:
//   - [info]: the match.
//   - [error]: some error.
func findUndocumented(which int) (info, error) {
	switch which {
	case 0:
		return info{}, ErrNoMatch // want "returns \\[ErrNoMatch\\] but its doc comment does not mention it"
	case 1:
		return info{}, ErrNoMatch
	case 2:
		return info{}, (errHidden) // want "returns \\[errHidden\\] but its doc comment does not mention it"
	case 3:
		return info{}, database.ErrNotFound // want "returns \\[database.ErrNotFound\\] but its doc comment does not mention it"
	case 4:
		return info{}, io.EOF // want "returns \\[io.EOF\\] but its doc comment does not mention it"
	}
	return info{}, errCustom // want "returns \\[errCustom\\] but its doc comment does not mention it"
}

// [lookup] mentions the error of another package as the file imports it.
//
// Returns:
//   - [error]: [database.ErrNotFound] if there is no such row.
func lookup() error {
	return database.ErrNotFound
}

// [notChecked] returns errors that are not checked: wrapped, from a local
// variable, from a call, [nil], and from a function literal.
//
// Returns:
//   - [error]: some error.
//   - [int]: a number.
func notChecked() (error, int) {
	err := ErrNoMatch
	if notAnError > 0 {
		return fmt.Errorf("wrapped: %w", ErrNoMatch), notAnError
	}
	if notAnError > 1 {
		return err, 0
	}
	if notAnError > 2 {
		return errors.Join(err), 0
	}
	inner := func() error { return ErrNoMatch }
	if notAnError > 3 {
		return inner(), 0
	}
	return nil, 0
}

// [shadowed] returns a local variable that has a sentinel's name.
//
// Returns:
//   - [error]: [nil].
func shadowed() error {
	var ErrNoMatch error
	return ErrNoMatch
}

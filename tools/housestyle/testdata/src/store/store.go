// Package [store] stands in for a package with a sentinel error.
package store

import "errors"

// [ErrNotFound] means there is no such row.
var ErrNotFound = errors.New("not found")

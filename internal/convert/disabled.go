package convert

import (
	"context"
)

// [Disabled] stands in for the [Converter] when conversion is turned off, so
// callers never need a [nil] check.
type Disabled struct{}

// [Status] reports that conversion is turned off.
//
// Returns:
//   - [Status]: a [Status] with [Status.Off] set and nothing else.
//   - [error]: always [nil].
func (Disabled) Status(context.Context) (Status, error) { return Status{Off: true}, nil }

// [Wake] does nothing: there is no queue to look at.
func (Disabled) Wake() {}

package convert

import (
	"context"
)

// Disabled stands in for the converter when conversion is turned off, so
// callers never need a nil check.
type Disabled struct{}

// Status reports that conversion is off.
func (Disabled) Status(context.Context) (Status, error) { return Status{Off: true}, nil }

// Wake does nothing.
func (Disabled) Wake() {}

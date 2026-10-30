package web

import (
	"github.com/AlexCristian/NexusPlay/internal/convert"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [ConversionView] is the control panel's view of the conversion queue.
type ConversionView struct {
	// [Status] is what the converter is doing now.
	Status convert.Status
	// [Failures] lists the files whose conversion failed, by path.
	Failures []store.ConversionFailure
}

// [busy] reports whether conversions are running or waiting, which makes the
// panel refresh itself.
//
// Returns:
//   - [bool]: [true] if conversion is on and something is running or
//     queued, [false] otherwise.
func (view ConversionView) busy() bool {
	return !view.Status.Off && (len(view.Status.Active) > 0 || view.Status.Queued > 0)
}

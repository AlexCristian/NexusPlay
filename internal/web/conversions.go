package web

import (
	"context"

	"github.com/AlexCristian/NexusPlay/internal/convert"
)

// [Conversions] is the MP4 converter as the control panel sees it.
// [convert.Disabled] stands in when conversion is turned off.
type Conversions interface {
	// [Status] reports what the converter is doing.
	//
	// Parameters:
	//   - [ctx]: cancels the database query.
	//
	// Returns:
	//   - [convert.Status]: the snapshot.
	//   - [error]: [nil] on success, or the database error.
	Status(ctx context.Context) (convert.Status, error)
	// [Wake] asks the converter to look at its queue now, such as after a
	// retry.
	Wake()
}

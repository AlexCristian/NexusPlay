package web

import (
	"context"

	"github.com/AlexCristian/NexusPlay/internal/convert"
)

// Conversions is the MP4 converter as the control panel sees it.
// convert.Disabled stands in when conversion is turned off.
type Conversions interface {
	Status(ctx context.Context) (convert.Status, error)
	Wake()
}

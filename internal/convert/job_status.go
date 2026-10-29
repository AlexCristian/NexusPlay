package convert

import (
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [JobStatus] describes a conversion in progress, for the control panel.
type JobStatus struct {
	// [Media] is the file being converted, as it was when the conversion
	// started.
	Media store.Media
	// [Percent] is how far the conversion has got, from 0 to 99; it is 0 until
	// the file's length is known.
	Percent int
}

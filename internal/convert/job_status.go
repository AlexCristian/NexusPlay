package convert

import (
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// JobStatus describes a conversion in progress.
type JobStatus struct {
	Media   store.Media
	Percent int
}

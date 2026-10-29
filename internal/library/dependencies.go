package library

import (
	"log/slog"
)

// Dependencies are the scanner's collaborators.
type Dependencies struct {
	Store    Store
	Prober   Prober
	Metadata Metadata // nil turns lookups off
	Log      *slog.Logger
}

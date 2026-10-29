package library

import (
	_ "embed"
	"log/slog"
)

// Dependencies are the scanner's collaborators.
type Dependencies struct {
	Store  Store
	Prober Prober
	Meta   Metadata // nil turns lookups off
	Log    *slog.Logger
}

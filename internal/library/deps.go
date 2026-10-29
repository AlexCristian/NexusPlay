package library

import (
	_ "embed"
	"log/slog"
)

// Deps are the scanner's collaborators.
type Deps struct {
	Store  Store
	Prober Prober
	Meta   Metadata // nil turns lookups off
	Log    *slog.Logger
}

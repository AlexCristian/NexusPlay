package convert

import (
	"log/slog"
)

// Deps are the converter's collaborators.
type Deps struct {
	Store   Store
	Tools   Tools
	Library Library // nil in tests that run without a scanner
	Log     *slog.Logger
}

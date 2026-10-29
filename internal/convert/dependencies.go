package convert

import (
	"log/slog"
)

// Dependencies are the converter's collaborators.
type Dependencies struct {
	Store   Store
	Tools   Tools
	Library Library // nil in tests that run without a scanner
	Log     *slog.Logger
}

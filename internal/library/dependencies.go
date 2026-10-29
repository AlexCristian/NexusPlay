package library

import (
	"log/slog"
)

// [Dependencies] are the scanner's collaborators, passed to [NewScanner].
type Dependencies struct {
	// [Store] is the database; normally the [store.Store].
	Store Store
	// [Prober] reads new files' streams; normally an [ffmpeg.Tools].
	Prober Prober
	// [Metadata] looks titles up online; normally the [metadata.Client]. [nil]
	// turns lookups off.
	Metadata Metadata
	// [Log] records scan results and problems with single files.
	Log *slog.Logger
}

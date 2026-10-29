package convert

import (
	"log/slog"
)

// [Dependencies] are the converter's collaborators, passed to [New].
type Dependencies struct {
	// [Store] lists candidates and records results; normally the [store.Store].
	Store Store
	// [Tools] runs ffprobe and ffmpeg; normally an [ffmpeg.Tools].
	Tools Tools
	// [Library] is the scanner, whose lock the swap holds and which rescans after
	// a conversion to import extracted subtitles. [nil] in tests that run
	// without a scanner; the swap then runs unlocked.
	Library Library
	// [Log] records each conversion's progress and outcome.
	Log *slog.Logger
}

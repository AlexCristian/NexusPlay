package subtitle

// [Cue] is one timed subtitle entry, which may hold several lines of text.
type Cue struct {
	// [StartMS] is when the cue appears, in milliseconds from the start of the
	// video.
	StartMS int64
	// [EndMS] is when the cue disappears, in milliseconds from the start of the
	// video.
	EndMS int64
	// [Text] is the cue's text, with its lines joined by "\n" and spaces around
	// the whole text trimmed. It is never "" in cues from [Parse].
	Text string
}

package store

// [Cue] is one stored subtitle line.
type Cue struct {
	// [ID] identifies the line, for proposing corrections.
	ID int64
	// [StartMS] is when the line appears, in milliseconds from the start.
	StartMS int64
	// [EndMS] is when the line disappears, in milliseconds from the start.
	EndMS int64
	// [Text] is the line's current text, including any applied correction.
	Text string
}

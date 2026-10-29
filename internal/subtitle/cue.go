package subtitle

// Cue is one timed subtitle line (possibly several text lines).
type Cue struct {
	StartMS int64
	EndMS   int64
	Text    string
}

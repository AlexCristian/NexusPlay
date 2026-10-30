package subtitle

import "errors"

// [ErrNoCues] is returned by [Parse] when a file holds no cue with text.
var ErrNoCues = errors.New("no subtitle cues found")

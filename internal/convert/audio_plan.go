package convert

import (
	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
)

// AudioPlan is one audio track in the output.
type AudioPlan struct {
	Stream   ffmpeg.Stream
	Copy     bool
	Channels int // output channels when encoding
	Bitrate  int // kbit/s when encoding
}

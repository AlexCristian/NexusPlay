package convert

import (
	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
)

// SubtitlePlan is one subtitle track saved as a sidecar file.
type SubtitlePlan struct {
	Stream ffmpeg.Stream
	File   string // file name, in the video's folder
}

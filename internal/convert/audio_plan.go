package convert

import (
	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
)

// [AudioPlan] is how one audio track of the original goes into the MP4.
type AudioPlan struct {
	// [Stream] is the original's audio stream.
	Stream ffmpeg.Stream
	// [Copy] is [true] when the track is AAC or MP3 and is copied unchanged;
	// otherwise it is re-encoded to AAC.
	Copy bool
	// [Channels] is the output's channel count when re-encoding: the original's,
	// at most 6 (7.1 becomes 5.1), or 2 if the original's is unknown.
	Channels int
	// [Bitrate] is the output's bitrate in kbit/s when re-encoding: 64 per
	// channel, at least 128.
	Bitrate int
}

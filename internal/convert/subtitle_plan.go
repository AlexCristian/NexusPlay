package convert

import (
	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
)

// [SubtitlePlan] is one text subtitle track of the original, saved as an SRT
// file next to the video.
type SubtitlePlan struct {
	// [Stream] is the original's subtitle stream.
	Stream ffmpeg.Stream
	// [File] is the SRT file's name in the video's folder, such as
	// "Movie.en.srt" (see [nameSubtitles]).
	File string
}

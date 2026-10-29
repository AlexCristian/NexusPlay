// Package convert turns videos browsers cannot play into MP4 files in the
// background and replaces the originals with them.
//
// It owns the conversion queue, the ffmpeg arguments for a conversion
// (plan.go), verifying the result, and the swap that replaces the original
// (job.go). Which files need converting is media.NeedsConversion's decision,
// not this package's. Subtitle tracks are extracted to sidecar files rather
// than embedded, because corrections are applied to the database copy and
// must not require re-encoding the video.
//
// The swap is the one place NexusPlay deletes a user's file. It never
// overwrites an existing file, verifies the new file before deleting the
// old one, and holds the scanner's lock (Library.Exclusive) so a scan never
// sees the folder half-changed. See swap in job.go.
//
// app starts the Converter; web reads its Status through a small interface;
// Disabled stands in when NEXUSPLAY_CONVERT is off.
package convert

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/media"
)

// Plan is how one file will be converted.
type Plan struct {
	Video        ffmpeg.Stream
	CopyVideo    bool
	Audio        []AudioPlan
	Subtitles    []SubtitlePlan // text subtitles extracted to .srt files next to the video
	DroppedSubs  int            // image-based subtitles MP4 cannot hold
	DroppedOther int            // data and attachment streams (fonts, timecodes)
}

// AudioPlan is one audio track in the output.
type AudioPlan struct {
	Stream   ffmpeg.Stream
	Copy     bool
	Channels int // output channels when encoding
	Bitrate  int // kbit/s when encoding
}

// SubtitlePlan is one subtitle track saved as a sidecar file.
type SubtitlePlan struct {
	Stream ffmpeg.Stream
	File   string // file name, in the video's folder
}

// textSubtitleCodecs can be converted to SRT; others are bitmaps (DVD, Blu-ray).
var textSubtitleCodecs = map[string]bool{
	"subrip": true, "srt": true, "ass": true, "ssa": true, "mov_text": true, "webvtt": true, "text": true,
}

// MakePlan decides what to copy, what to re-encode and what to extract.
// videoBase is the video's file name without its extension. It returns
// ffmpeg.ErrNoVideo if src has no video stream.
func MakePlan(probe ffmpeg.Probe, videoBase string) (Plan, error) {
	var plan Plan
	video, hasVideo := probe.Video()
	if !hasVideo {
		return plan, ffmpeg.ErrNoVideo
	}
	plan.Video = video
	for _, stream := range probe.Streams {
		switch stream.Type {
		case "video":
			// The main video is chosen above; any other video stream (cover
			// art, a second angle) is left out.
		case "audio":
			plan.Audio = append(plan.Audio, planAudio(stream))
		case "subtitle":
			if textSubtitleCodecs[stream.Codec] {
				plan.Subtitles = append(plan.Subtitles, SubtitlePlan{Stream: stream})
			} else {
				plan.DroppedSubs++
			}
		default:
			plan.DroppedOther++
		}
	}
	// Browsers decode only 8-bit 4:2:0 H.264; anything else is re-encoded.
	// Here the pixel format comes straight from ffprobe, so an empty one is
	// treated as unknown and re-encoded too.
	plan.CopyVideo = plan.Video.Codec == "h264" && plan.Video.PixFmt != "" && media.BrowserSafeH264(plan.Video.PixFmt)
	nameSubtitles(plan.Subtitles, videoBase)
	return plan, nil
}

func planAudio(stream ffmpeg.Stream) AudioPlan {
	audioPlan := AudioPlan{Stream: stream, Copy: stream.Codec == "aac" || stream.Codec == "mp3"}
	audioPlan.Channels = stream.Channels
	if audioPlan.Channels <= 0 {
		audioPlan.Channels = 2
	}
	if audioPlan.Channels > 6 {
		audioPlan.Channels = 6 // 7.1 becomes 5.1, the most AAC players handle
	}
	// About 64 kbit/s per channel: 128k stereo, 384k for 5.1.
	audioPlan.Bitrate = 64 * audioPlan.Channels
	if audioPlan.Bitrate < 128 {
		audioPlan.Bitrate = 128
	}
	return audioPlan
}

// nameSubtitles names sidecar files the way the scanner reads them back:
// "Movie.en.srt", and "Movie.track2.en.srt" for a second English track.
func nameSubtitles(subs []SubtitlePlan, base string) {
	seen := map[string]int{}
	for i := range subs {
		language := safeLanguage(subs[i].Stream.Language)
		seen[language]++
		name := base + "." + language + ".srt"
		if count := seen[language]; count > 1 {
			name = base + ".track" + strconv.Itoa(count) + "." + language + ".srt"
		}
		subs[i].File = name
	}
}

// languageTag matches the language codes that go into sidecar file names:
// ISO 639 codes such as "en", "eng" or "pt-br".
var languageTag = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})?$`)

// safeLanguage returns the stream's language tag if it is a plain language
// code, and "und" otherwise. The tag comes from the media file itself, so it
// is untrusted: without this check a tag like "../../etc/x" would make the
// sidecar path escape the video's folder.
func safeLanguage(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if languageTag.MatchString(tag) {
		return tag
	}
	return "und"
}

// ConvertArguments builds the ffmpeg command that writes the MP4.
func (plan Plan) ConvertArguments(source, out string) []string {
	arguments := []string{"-nostdin", "-hide_banner", "-loglevel", "error",
		"-i", source, "-map", fmt.Sprintf("0:%d", plan.Video.Index)}
	for _, audioPlan := range plan.Audio {
		arguments = append(arguments, "-map", fmt.Sprintf("0:%d", audioPlan.Stream.Index))
	}
	if plan.CopyVideo {
		arguments = append(arguments, "-c:v", "copy")
	} else {
		arguments = append(arguments, "-c:v", "libx264", "-preset", "medium", "-crf", "20",
			"-pix_fmt", "yuv420p", "-profile:v", "high")
	}
	for i, audioPlan := range plan.Audio {
		index := strconv.Itoa(i)
		if audioPlan.Copy {
			arguments = append(arguments, "-c:a:"+index, "copy")
		} else {
			arguments = append(arguments, "-c:a:"+index, "aac", "-b:a:"+index, strconv.Itoa(audioPlan.Bitrate)+"k", "-ac:a:"+index, strconv.Itoa(audioPlan.Channels))
		}
	}
	if len(plan.Audio) > 0 {
		// Players start with the first audio track; keep the original's default one first in line.
		def := 0
		for i, audioPlan := range plan.Audio {
			if audioPlan.Stream.Default {
				def = i
				break
			}
		}
		for i := range plan.Audio {
			disp := "0"
			if i == def {
				disp = "default"
			}
			arguments = append(arguments, "-disposition:a:"+strconv.Itoa(i), disp)
		}
	}
	return append(arguments, "-map_metadata", "0", "-map_chapters", "0", "-sn", "-dn",
		"-max_muxing_queue_size", "9999", "-movflags", "+faststart", "-f", "mp4", "-y", out)
}

// SubtitleArguments builds the ffmpeg command that extracts one text subtitle as SRT.
func SubtitleArguments(source string, subtitlePlan SubtitlePlan, out string) []string {
	return []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-i", source,
		"-map", fmt.Sprintf("0:%d", subtitlePlan.Stream.Index), "-c:s", "srt", "-f", "srt", "-y", out}
}

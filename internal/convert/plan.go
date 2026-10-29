package convert

import (
	_ "embed"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/datafile"
	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/media"
)

// [Plan] is how one file will be converted, as decided by [MakePlan].
type Plan struct {
	// [Video] is the original's main video stream.
	Video ffmpeg.Stream
	// [CopyVideo] is [true] when the video is already browser-safe H.264 and is
	// copied unchanged; otherwise it is re-encoded.
	CopyVideo bool
	// [Audio] lists every audio track, in the original's order.
	Audio []AudioPlan
	// [Subtitles] lists the text subtitles extracted to SRT files next to the
	// video.
	Subtitles []SubtitlePlan
	// [DroppedSubtitles] counts the image-based subtitles, which MP4 cannot hold.
	DroppedSubtitles int
	// [DroppedOther] counts the data and attachment streams left out, such as
	// fonts and timecodes.
	DroppedOther int
}

// [textSubtitleCodecsFile] is the contents of text_subtitle_codecs.txt,
// built into the program by the compiler. The build fails if the file is
// missing.
//
//go:embed text_subtitle_codecs.txt
var textSubtitleCodecsFile string

// [textSubtitleCodecs] holds the subtitle codecs that can be converted to
// SRT, from text_subtitle_codecs.txt; others are bitmaps (DVD, Blu-ray).
var textSubtitleCodecs = datafile.Set(textSubtitleCodecsFile)

// [MakePlan] decides what to copy, what to re-encode and what to extract.
// Every audio track is kept, text subtitles are extracted, and other video
// streams (cover art, a second angle) are left out. The video is copied
// only if it is H.264 in a known pixel format that [media.BrowserSafeH264]
// accepts; an empty pixel format is treated as unknown here.
//
// Parameters:
//   - [probe]: the original's probe.
//   - [videoBase]: the video's file name without its extension, used to
//     name the subtitle files.
//
// Returns:
//   - [Plan]: the plan, or a zero [Plan] if [error] is not [nil].
//   - [error]: [nil] on success, or [ffmpeg.ErrNoVideo] if [probe] has no
//     video stream.
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
			if textSubtitleCodecs.Contains(stream.Codec) {
				plan.Subtitles = append(plan.Subtitles, SubtitlePlan{Stream: stream})
			} else {
				plan.DroppedSubtitles++
			}
		default:
			plan.DroppedOther++
		}
	}
	// Browsers decode only 8-bit 4:2:0 H.264; anything else is re-encoded.
	// Here the pixel format comes straight from ffprobe, so an empty one is
	// treated as unknown and re-encoded too.
	plan.CopyVideo = plan.Video.Codec == "h264" && plan.Video.PixelFormat != "" && media.BrowserSafeH264(plan.Video.PixelFormat)
	nameSubtitles(plan.Subtitles, videoBase)
	return plan, nil
}

// [planAudio] decides how one audio track goes into the MP4.
//
// Parameters:
//   - [stream]: the original's audio stream.
//
// Returns:
//   - [AudioPlan]: the plan: copy AAC and MP3; otherwise re-encode to AAC
//     with at most 6 channels (2 if unknown) at 64 kbit/s per channel, at
//     least 128.
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

// [nameSubtitles] names the SRT files the way the scanner reads them back:
// "Movie.en.srt", and "Movie.track2.en.srt" for a second English track.
// Languages that are not plain codes become "und" (see [safeLanguage]).
//
// Parameters:
//   - [subtitles]: the tracks to name; each one's [SubtitlePlan.File] is
//     set.
//   - [base]: the video's file name without its extension.
func nameSubtitles(subtitles []SubtitlePlan, base string) {
	seen := map[string]int{}
	for i := range subtitles {
		language := safeLanguage(subtitles[i].Stream.Language)
		seen[language]++
		name := base + "." + language + ".srt"
		if count := seen[language]; count > 1 {
			name = base + ".track" + strconv.Itoa(count) + "." + language + ".srt"
		}
		subtitles[i].File = name
	}
}

// [languageTag] matches the language codes allowed in subtitle file names:
// ISO 639 codes with an optional subtag, such as "en", "eng" or "pt-br".
var languageTag = regexp.MustCompile(`^[a-z]{2,3}(-[a-z0-9]{2,8})?$`)

// [safeLanguage] makes a stream's language tag safe to put in a file name.
// The tag comes from the media file itself, so it is untrusted: without this
// check a tag like "../../etc/x" would make the subtitle's path escape the
// video's folder.
//
// Parameters:
//   - [tag]: the stream's language tag. Case and spaces around it do not
//     matter.
//
// Returns:
//   - [string]: [tag] lowercased if it is a plain language code (see
//     [languageTag]), "und" otherwise.
func safeLanguage(tag string) string {
	tag = strings.ToLower(strings.TrimSpace(tag))
	if languageTag.MatchString(tag) {
		return tag
	}
	return "und"
}

// [ConvertArguments] builds the ffmpeg arguments that write the MP4: the
// main video and every audio track, copied or re-encoded as planned (video
// to H.264 high profile, CRF 20, 8-bit 4:2:0), with the original's default
// audio track marked default, metadata and chapters kept, no subtitles or
// data, and the index at the front so playback can start before the file is
// downloaded.
//
// Parameters:
//   - [source]: the original's path.
//   - [outputPath]: where to write the MP4; an existing file is overwritten.
//
// Returns:
//   - [[]string]: ffmpeg's arguments, without the program name.
func (plan Plan) ConvertArguments(source, outputPath string) []string {
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
		defaultIndex := 0
		for i, audioPlan := range plan.Audio {
			if audioPlan.Stream.Default {
				defaultIndex = i
				break
			}
		}
		for i := range plan.Audio {
			disposition := "0"
			if i == defaultIndex {
				disposition = "default"
			}
			arguments = append(arguments, "-disposition:a:"+strconv.Itoa(i), disposition)
		}
	}
	return append(arguments, "-map_metadata", "0", "-map_chapters", "0", "-sn", "-dn",
		"-max_muxing_queue_size", "9999", "-movflags", "+faststart", "-f", "mp4", "-y", outputPath)
}

// [SubtitleArguments] builds the ffmpeg arguments that extract one text
// subtitle track as SRT.
//
// Parameters:
//   - [source]: the original's path.
//   - [subtitlePlan]: the track to extract.
//   - [outputPath]: where to write the SRT file; an existing file is
//     overwritten.
//
// Returns:
//   - [[]string]: ffmpeg's arguments, without the program name.
func SubtitleArguments(source string, subtitlePlan SubtitlePlan, outputPath string) []string {
	return []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-i", source,
		"-map", fmt.Sprintf("0:%d", subtitlePlan.Stream.Index), "-c:s", "srt", "-f", "srt", "-y", outputPath}
}

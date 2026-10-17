// Package convert turns videos browsers cannot play into MP4 files in the
// background and replaces the originals with them.
package convert

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Stream is one stream of a source file, as ffprobe reports it.
type Stream struct {
	Index     int
	Type      string // video, audio, subtitle, data, attachment
	Codec     string
	PixFmt    string
	Channels  int
	Language  string
	Title     string
	Default   bool
	AttachPic bool // cover art stored as a video stream
}

// Source is what the converter needs to know about a file.
type Source struct {
	Duration float64
	Streams  []Stream
}

// ProbeSource lists a file's streams.
func ProbeSource(ctx context.Context, ffprobe, file string) (Source, error) {
	out, err := exec.CommandContext(ctx, ffprobe, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", file).Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return Source{}, fmt.Errorf("ffprobe: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return Source{}, fmt.Errorf("ffprobe: %w", err)
	}
	var raw struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Index       int               `json:"index"`
			CodecType   string            `json:"codec_type"`
			CodecName   string            `json:"codec_name"`
			PixFmt      string            `json:"pix_fmt"`
			Channels    int               `json:"channels"`
			Tags        map[string]string `json:"tags"`
			Disposition map[string]int    `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return Source{}, fmt.Errorf("ffprobe output: %w", err)
	}
	var src Source
	src.Duration, _ = strconv.ParseFloat(raw.Format.Duration, 64)
	for _, s := range raw.Streams {
		src.Streams = append(src.Streams, Stream{
			Index: s.Index, Type: s.CodecType, Codec: s.CodecName, PixFmt: s.PixFmt, Channels: s.Channels,
			Language: strings.ToLower(s.Tags["language"]), Title: s.Tags["title"],
			Default: s.Disposition["default"] == 1, AttachPic: s.Disposition["attached_pic"] == 1,
		})
	}
	return src, nil
}

// Plan is how one file will be converted.
type Plan struct {
	Video        Stream
	CopyVideo    bool
	Audio        []AudioPlan
	Subtitles    []SubtitlePlan // text subtitles extracted to .srt files next to the video
	DroppedSubs  int            // image-based subtitles MP4 cannot hold
	DroppedOther int            // data and attachment streams (fonts, timecodes)
}

// AudioPlan is one audio track in the output.
type AudioPlan struct {
	Stream   Stream
	Copy     bool
	Channels int // output channels when encoding
	Bitrate  int // kbit/s when encoding
}

// SubtitlePlan is one subtitle track saved as a sidecar file.
type SubtitlePlan struct {
	Stream Stream
	File   string // file name, in the video's folder
}

// textSubtitleCodecs can be converted to SRT; others are bitmaps (DVD, Blu-ray).
var textSubtitleCodecs = map[string]bool{
	"subrip": true, "srt": true, "ass": true, "ssa": true, "mov_text": true, "webvtt": true, "text": true,
}

// MakePlan decides what to copy, what to re-encode and what to extract.
// videoBase is the video's file name without its extension.
func MakePlan(src Source, videoBase string) (Plan, error) {
	var p Plan
	found := false
	for _, s := range src.Streams {
		switch s.Type {
		case "video":
			if s.AttachPic || found {
				continue
			}
			p.Video, found = s, true
		case "audio":
			p.Audio = append(p.Audio, planAudio(s))
		case "subtitle":
			if textSubtitleCodecs[s.Codec] {
				p.Subtitles = append(p.Subtitles, SubtitlePlan{Stream: s})
			} else {
				p.DroppedSubs++
			}
		default:
			p.DroppedOther++
		}
	}
	if !found {
		return p, fmt.Errorf("no video stream")
	}
	// Browsers decode 8-bit 4:2:0 H.264; 10-bit or 4:4:4 H.264 must be re-encoded.
	p.CopyVideo = p.Video.Codec == "h264" && (p.Video.PixFmt == "yuv420p" || p.Video.PixFmt == "yuvj420p")
	nameSubtitles(p.Subtitles, videoBase)
	return p, nil
}

func planAudio(s Stream) AudioPlan {
	a := AudioPlan{Stream: s, Copy: s.Codec == "aac" || s.Codec == "mp3"}
	a.Channels = s.Channels
	if a.Channels <= 0 {
		a.Channels = 2
	}
	if a.Channels > 6 {
		a.Channels = 6 // 7.1 becomes 5.1, the most AAC players handle
	}
	// About 64 kbit/s per channel: 128k stereo, 384k for 5.1.
	a.Bitrate = 64 * a.Channels
	if a.Bitrate < 128 {
		a.Bitrate = 128
	}
	return a
}

// nameSubtitles names sidecar files the way the scanner reads them back:
// "Movie.en.srt", and "Movie.track2.en.srt" for a second English track.
func nameSubtitles(subs []SubtitlePlan, base string) {
	seen := map[string]int{}
	for i := range subs {
		lang := safeLanguage(subs[i].Stream.Language)
		seen[lang]++
		name := base + "." + lang + ".srt"
		if n := seen[lang]; n > 1 {
			name = base + ".track" + strconv.Itoa(n) + "." + lang + ".srt"
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

// ConvertArgs builds the ffmpeg command that writes the MP4.
func (p Plan) ConvertArgs(src, out string) []string {
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-progress", "pipe:1", "-nostats",
		"-i", src, "-map", fmt.Sprintf("0:%d", p.Video.Index)}
	for _, a := range p.Audio {
		args = append(args, "-map", fmt.Sprintf("0:%d", a.Stream.Index))
	}
	if p.CopyVideo {
		args = append(args, "-c:v", "copy")
	} else {
		args = append(args, "-c:v", "libx264", "-preset", "medium", "-crf", "20",
			"-pix_fmt", "yuv420p", "-profile:v", "high")
	}
	for i, a := range p.Audio {
		n := strconv.Itoa(i)
		if a.Copy {
			args = append(args, "-c:a:"+n, "copy")
		} else {
			args = append(args, "-c:a:"+n, "aac", "-b:a:"+n, strconv.Itoa(a.Bitrate)+"k", "-ac:a:"+n, strconv.Itoa(a.Channels))
		}
	}
	if len(p.Audio) > 0 {
		// Players start with the first audio track; keep the original's default one first in line.
		def := 0
		for i, a := range p.Audio {
			if a.Stream.Default {
				def = i
				break
			}
		}
		for i := range p.Audio {
			disp := "0"
			if i == def {
				disp = "default"
			}
			args = append(args, "-disposition:a:"+strconv.Itoa(i), disp)
		}
	}
	return append(args, "-map_metadata", "0", "-map_chapters", "0", "-sn", "-dn",
		"-max_muxing_queue_size", "9999", "-movflags", "+faststart", "-f", "mp4", "-y", out)
}

// SubtitleArgs builds the ffmpeg command that extracts one text subtitle as SRT.
func SubtitleArgs(src string, s SubtitlePlan, out string) []string {
	return []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-i", src,
		"-map", fmt.Sprintf("0:%d", s.Stream.Index), "-c:s", "srt", "-f", "srt", "-y", out}
}

// NeedsConversion reports whether a file should be converted: anything that
// is not an MP4 with H.264 video and AAC or MP3 audio.
func NeedsConversion(file, videoCodec, audioCodec string) bool {
	ext := strings.ToLower(path.Ext(file))
	if ext != ".mp4" && ext != ".m4v" {
		return true
	}
	return videoCodec != "h264" || (audioCodec != "" && audioCodec != "aac" && audioCodec != "mp3")
}

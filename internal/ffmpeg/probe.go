package ffmpeg

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// [Probe] is everything NexusPlay reads from ffprobe about one file. Get one
// with [Tools.Probe].
type Probe struct {
	// [Duration] is the file's length in seconds, or 0 when ffprobe cannot tell.
	Duration float64
	// [Container] lists the container format names ffprobe matched, separated by
	// commas, such as "mov,mp4,m4a,3gp,3g2,mj2".
	Container string
	// [Streams] holds every stream in the file, in file order.
	Streams []Stream
}

// [ErrNoVideo] means a file has no video stream other than cover art.
var ErrNoVideo = errors.New("no video stream")

// [Video] finds the main video stream: the first video stream that is not
// cover art (see [Stream.AttachedPicture] and [isImageCodec]).
//
// Returns:
//   - [Stream]: the main video stream, or a zero [Stream] if there is none.
//   - [bool]: [true] if a video stream was found, [false] otherwise.
func (probe Probe) Video() (Stream, bool) {
	for _, stream := range probe.Streams {
		if stream.Type == "video" && !stream.AttachedPicture && !isImageCodec(stream.Codec) {
			return stream, true
		}
	}
	return Stream{}, false
}

// [Audio] finds the first audio stream.
//
// Returns:
//   - [Stream]: the first audio stream, or a zero [Stream] if there is none.
//   - [bool]: [true] if an audio stream was found, [false] otherwise.
func (probe Probe) Audio() (Stream, bool) {
	for _, stream := range probe.Streams {
		if stream.Type == "audio" {
			return stream, true
		}
	}
	return Stream{}, false
}

// [Count] counts the file's streams of one type. Cover art counts as video.
//
// Parameters:
//   - [streamType]: the type to count, such as "audio" or "subtitle" (see
//     [Stream.Type]).
//
// Returns:
//   - [int]: how many streams have that type.
func (probe Probe) Count(streamType string) int {
	count := 0
	for _, stream := range probe.Streams {
		if stream.Type == streamType {
			count++
		}
	}
	return count
}

// [VideoCodec] gets the codec of the main video stream (see [Probe.Video]).
//
// Returns:
//   - [string]: the codec as ffprobe names it, such as "h264", or "" if the
//     file has no video.
func (probe Probe) VideoCodec() string { stream, _ := probe.Video(); return stream.Codec }

// [AudioCodec] gets the codec of the first audio stream.
//
// Returns:
//   - [string]: the codec as ffprobe names it, such as "aac", or "" if the
//     file has no audio.
func (probe Probe) AudioCodec() string { stream, _ := probe.Audio(); return stream.Codec }

// [isImageCodec] reports whether a video stream's codec is a still-image
// format used for cover art, which some files store without the
// attached_pic flag.
//
// Parameters:
//   - [codec]: the codec as ffprobe names it.
//
// Returns:
//   - [bool]: [true] for "mjpeg", "png" and "bmp", [false] otherwise.
func isImageCodec(codec string) bool {
	return codec == "mjpeg" || codec == "png" || codec == "bmp"
}

// [parseProbe] decodes ffprobe's JSON output (from "-print_format json
// -show_format -show_streams"). The language of each stream is lowercased.
//
// Parameters:
//   - [output]: ffprobe's standard output.
//
// Returns:
//   - [Probe]: the file's details, with a [Probe.Duration] of 0 when
//     ffprobe reports none or "N/A"; a zero [Probe] if [error] is not [nil].
//   - [error]: [nil] on success, or an error wrapping the JSON error if
//     [output] is not ffprobe's JSON.
func parseProbe(output []byte) (Probe, error) {
	var raw struct {
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Index       int               `json:"index"`
			CodecType   string            `json:"codec_type"`
			CodecName   string            `json:"codec_name"`
			PixelFormat string            `json:"pix_fmt"`
			Channels    int               `json:"channels"`
			Tags        map[string]string `json:"tags"`
			Disposition map[string]int    `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(output, &raw); err != nil {
		return Probe{}, fmt.Errorf("reading ffprobe output: %w", err)
	}
	probe := Probe{Container: raw.Format.FormatName}
	probe.Duration, _ = strconv.ParseFloat(raw.Format.Duration, 64) // "N/A" for some files: 0 means unknown
	for _, rawStream := range raw.Streams {
		probe.Streams = append(probe.Streams, Stream{
			Index: rawStream.Index, Type: rawStream.CodecType, Codec: rawStream.CodecName, PixelFormat: rawStream.PixelFormat, Channels: rawStream.Channels,
			Language: strings.ToLower(rawStream.Tags["language"]), Title: rawStream.Tags["title"],
			Default: rawStream.Disposition["default"] == 1, AttachedPicture: rawStream.Disposition["attached_pic"] == 1,
		})
	}
	return probe, nil
}

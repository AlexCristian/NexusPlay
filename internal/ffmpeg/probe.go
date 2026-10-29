package ffmpeg

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Probe is everything NexusPlay reads from ffprobe about one file.
type Probe struct {
	Duration  float64 // seconds; 0 when ffprobe cannot tell
	Container string  // ffprobe's format names, e.g. "mov,mp4,m4a,3gp,3g2,mj2"
	Streams   []Stream
}

// ErrNoVideo means a file has no video stream (other than cover art).
var ErrNoVideo = errors.New("no video stream")

// Video returns the main video stream: the first video stream that is not
// cover art.
func (probe Probe) Video() (Stream, bool) {
	for _, stream := range probe.Streams {
		if stream.Type == "video" && !stream.AttachedPicture && !isImageCodec(stream.Codec) {
			return stream, true
		}
	}
	return Stream{}, false
}

// Audio returns the first audio stream.
func (probe Probe) Audio() (Stream, bool) {
	for _, stream := range probe.Streams {
		if stream.Type == "audio" {
			return stream, true
		}
	}
	return Stream{}, false
}

// Count returns how many streams of a type the file has.
func (probe Probe) Count(streamType string) int {
	count := 0
	for _, stream := range probe.Streams {
		if stream.Type == streamType {
			count++
		}
	}
	return count
}

// VideoCodec and AudioCodec return the main streams' codecs, or "" if absent.
func (probe Probe) VideoCodec() string { stream, _ := probe.Video(); return stream.Codec }
func (probe Probe) AudioCodec() string { stream, _ := probe.Audio(); return stream.Codec }

// isImageCodec reports codecs used for cover art that some files store
// without the attached_pic flag.
func isImageCodec(codec string) bool {
	return codec == "mjpeg" || codec == "png" || codec == "bmp"
}

// parseProbe decodes ffprobe's JSON output.
func parseProbe(out []byte) (Probe, error) {
	var raw struct {
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			Index       int               `json:"index"`
			CodecType   string            `json:"codec_type"`
			CodecName   string            `json:"codec_name"`
			PixelFormat      string            `json:"pix_fmt"`
			Channels    int               `json:"channels"`
			Tags        map[string]string `json:"tags"`
			Disposition map[string]int    `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
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

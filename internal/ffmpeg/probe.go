package ffmpeg

import (
	"context"
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

// Stream is one stream of a file.
type Stream struct {
	Index     int    // position in the file, as used by ffmpeg's -map 0:N
	Type      string // "video", "audio", "subtitle", "data" or "attachment"
	Codec     string // e.g. "h264", "aac", "subrip"
	PixFmt    string // video only, e.g. "yuv420p"
	Channels  int    // audio only
	Language  string // lower-case language tag from the file; untrusted
	Title     string
	Default   bool // the file marks this stream as the one to play by default
	AttachPic bool // cover art stored as a video stream
}

// ErrNoVideo means a file has no video stream (other than cover art).
var ErrNoVideo = errors.New("no video stream")

// Video returns the main video stream: the first video stream that is not
// cover art.
func (p Probe) Video() (Stream, bool) {
	for _, s := range p.Streams {
		if s.Type == "video" && !s.AttachPic && !isImageCodec(s.Codec) {
			return s, true
		}
	}
	return Stream{}, false
}

// Audio returns the first audio stream.
func (p Probe) Audio() (Stream, bool) {
	for _, s := range p.Streams {
		if s.Type == "audio" {
			return s, true
		}
	}
	return Stream{}, false
}

// Count returns how many streams of a type the file has.
func (p Probe) Count(typ string) int {
	n := 0
	for _, s := range p.Streams {
		if s.Type == typ {
			n++
		}
	}
	return n
}

// VideoCodec and AudioCodec return the main streams' codecs, or "" if absent.
func (p Probe) VideoCodec() string { v, _ := p.Video(); return v.Codec }
func (p Probe) AudioCodec() string { a, _ := p.Audio(); return a.Codec }

// isImageCodec reports codecs used for cover art that some files store
// without the attached_pic flag.
func isImageCodec(codec string) bool {
	return codec == "mjpeg" || codec == "png" || codec == "bmp"
}

// Probe inspects a media file with ffprobe. Failures of ffprobe itself are
// *ExitError.
func (t Tools) Probe(ctx context.Context, file string) (Probe, error) {
	cmd := command(ctx, t.FFprobe, "-v", "error", "-print_format", "json",
		"-show_format", "-show_streams", file)
	stderr := &lastLineWriter{}
	cmd.Stderr = stderr
	out, err := cmd.Output()
	if err != nil {
		if exitErr := exitError("ffprobe", ctx, err, stderr); exitErr != nil {
			return Probe{}, exitErr
		}
	}
	return parseProbe(out)
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
			PixFmt      string            `json:"pix_fmt"`
			Channels    int               `json:"channels"`
			Tags        map[string]string `json:"tags"`
			Disposition map[string]int    `json:"disposition"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return Probe{}, fmt.Errorf("reading ffprobe output: %w", err)
	}
	p := Probe{Container: raw.Format.FormatName}
	p.Duration, _ = strconv.ParseFloat(raw.Format.Duration, 64) // "N/A" for some files: 0 means unknown
	for _, s := range raw.Streams {
		p.Streams = append(p.Streams, Stream{
			Index: s.Index, Type: s.CodecType, Codec: s.CodecName, PixFmt: s.PixFmt, Channels: s.Channels,
			Language: strings.ToLower(s.Tags["language"]), Title: s.Tags["title"],
			Default: s.Disposition["default"] == 1, AttachPic: s.Disposition["attached_pic"] == 1,
		})
	}
	return p, nil
}

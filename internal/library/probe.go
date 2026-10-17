package library

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// ProbeResult is the subset of ffprobe output NexusPlay uses.
type ProbeResult struct {
	Duration   float64
	Container  string // ffprobe format names, e.g. "mov,mp4,m4a,3gp,3g2,mj2"
	VideoCodec string
	AudioCodec string
}

// Prober runs ffprobe.
type Prober struct {
	Path string
}

// Probe inspects a media file.
func (p Prober) Probe(ctx context.Context, file string) (ProbeResult, error) {
	cmd := exec.CommandContext(ctx, p.Path, "-v", "error", "-print_format", "json",
		"-show_entries", "format=format_name,duration:stream=codec_type,codec_name", file)
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ProbeResult{}, fmt.Errorf("ffprobe: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return ProbeResult{}, fmt.Errorf("ffprobe: %w", err)
	}
	var parsed struct {
		Format struct {
			FormatName string `json:"format_name"`
			Duration   string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &parsed); err != nil {
		return ProbeResult{}, fmt.Errorf("ffprobe output: %w", err)
	}
	r := ProbeResult{Container: parsed.Format.FormatName}
	r.Duration, _ = strconv.ParseFloat(parsed.Format.Duration, 64) // "N/A" for some files: 0 means unknown
	for _, s := range parsed.Streams {
		switch {
		case s.CodecType == "video" && r.VideoCodec == "" && s.CodecName != "mjpeg" && s.CodecName != "png":
			r.VideoCodec = s.CodecName
		case s.CodecType == "audio" && r.AudioCodec == "":
			r.AudioCodec = s.CodecName
		}
	}
	if r.VideoCodec == "" {
		return r, fmt.Errorf("no video stream")
	}
	return r, nil
}

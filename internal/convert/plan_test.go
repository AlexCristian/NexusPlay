package convert

import (
	"slices"
	"strings"
	"testing"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
)

func TestMakePlan(t *testing.T) {
	src := ffmpeg.Probe{Duration: 60, Streams: []ffmpeg.Stream{
		{Index: 0, Type: "video", Codec: "h264", PixFmt: "yuv420p"},
		{Index: 1, Type: "audio", Codec: "ac3", Channels: 6, Language: "fre"},
		{Index: 2, Type: "audio", Codec: "aac", Channels: 2, Language: "eng", Default: true},
		{Index: 3, Type: "audio", Codec: "dts", Channels: 8, Language: "eng"},
		{Index: 4, Type: "subtitle", Codec: "subrip", Language: "eng"},
		{Index: 5, Type: "subtitle", Codec: "ass", Language: "eng"},
		{Index: 6, Type: "subtitle", Codec: "subrip"},
		{Index: 7, Type: "subtitle", Codec: "hdmv_pgs_subtitle", Language: "eng"},
		{Index: 8, Type: "attachment", Codec: "ttf"},
		{Index: 9, Type: "video", Codec: "mjpeg", AttachPic: true},
	}}
	p, err := MakePlan(src, "Movie (2020)")
	if err != nil {
		t.Fatal(err)
	}
	if !p.CopyVideo || p.Video.Index != 0 {
		t.Errorf("8-bit H.264 should be copied: %+v", p)
	}
	if len(p.Audio) != 3 || p.Audio[0].Copy || !p.Audio[1].Copy || p.Audio[2].Copy {
		t.Errorf("audio plan = %+v", p.Audio)
	}
	if p.Audio[0].Channels != 6 || p.Audio[0].Bitrate != 384 {
		t.Errorf("5.1 AC-3 should become 5.1 AAC at 384k: %+v", p.Audio[0])
	}
	if p.Audio[2].Channels != 6 {
		t.Errorf("7.1 should be reduced to 5.1: %+v", p.Audio[2])
	}
	var files []string
	for _, s := range p.Subtitles {
		files = append(files, s.File)
	}
	want := []string{"Movie (2020).eng.srt", "Movie (2020).track2.eng.srt", "Movie (2020).und.srt"}
	if !slices.Equal(files, want) {
		t.Errorf("subtitle files = %v, want %v", files, want)
	}
	if p.DroppedSubs != 1 || p.DroppedOther != 1 {
		t.Errorf("dropped subs=%d other=%d, want 1 and 1", p.DroppedSubs, p.DroppedOther)
	}

	args := strings.Join(p.ConvertArguments("in.mkv", "out.mp4"), " ")
	for _, wantArg := range []string{
		"-map 0:0 -map 0:1 -map 0:2 -map 0:3", "-c:v copy", "-c:a:0 aac -b:a:0 384k -ac:a:0 6", "-c:a:1 copy",
		"-disposition:a:0 0 -disposition:a:1 default -disposition:a:2 0", "-sn", "-movflags +faststart",
	} {
		if !strings.Contains(args, wantArg) {
			t.Errorf("ffmpeg args missing %q:\n%s", wantArg, args)
		}
	}
	if strings.Contains(args, "0:4") || strings.Contains(args, "0:9") {
		t.Errorf("subtitles and cover art must not be mapped into the MP4:\n%s", args)
	}
}

func TestMakePlanReencodes(t *testing.T) {
	for _, v := range []ffmpeg.Stream{
		{Type: "video", Codec: "hevc", PixFmt: "yuv420p"},
		{Type: "video", Codec: "h264", PixFmt: "yuv420p10le"}, // 10-bit H.264 does not play in browsers
		{Type: "video", Codec: "mpeg4", PixFmt: "yuv420p"},
	} {
		p, err := MakePlan(ffmpeg.Probe{Streams: []ffmpeg.Stream{v}}, "x")
		if err != nil {
			t.Fatal(err)
		}
		if p.CopyVideo {
			t.Errorf("%s/%s should be re-encoded", v.Codec, v.PixFmt)
		}
		if args := strings.Join(p.ConvertArguments("i", "o"), " "); !strings.Contains(args, "-c:v libx264") || strings.Contains(args, "-disposition:a") {
			t.Errorf("args for %s: %s", v.Codec, args)
		}
	}
	if _, err := MakePlan(ffmpeg.Probe{Streams: []ffmpeg.Stream{{Type: "audio", Codec: "aac"}}}, "x"); err == nil {
		t.Error("a file without video should be refused")
	}
}

// Language tags come from the media file and are untrusted: they must never
// steer the sidecar file outside the video's folder.
func TestSubtitleNamesIgnoreHostileLanguageTags(t *testing.T) {
	src := ffmpeg.Probe{Streams: []ffmpeg.Stream{
		{Index: 0, Type: "video", Codec: "h264", PixFmt: "yuv420p"},
		{Index: 1, Type: "subtitle", Codec: "subrip", Language: "../../../../tmp/pwned"},
		{Index: 2, Type: "subtitle", Codec: "subrip", Language: "EN"},
		{Index: 3, Type: "subtitle", Codec: "subrip", Language: "pt-BR"},
		{Index: 4, Type: "subtitle", Codec: "subrip", Language: "en/../x"},
	}}
	p, err := MakePlan(src, "Movie")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, s := range p.Subtitles {
		if strings.ContainsAny(s.File, "/\\") {
			t.Errorf("subtitle file name %q contains a path separator", s.File)
		}
		got = append(got, s.File)
	}
	want := []string{"Movie.und.srt", "Movie.en.srt", "Movie.pt-br.srt", "Movie.track2.und.srt"}
	if !slices.Equal(got, want) {
		t.Errorf("subtitle files = %v, want %v", got, want)
	}
}

func TestCheckConverted(t *testing.T) {
	plan := Plan{Audio: []AudioPlan{{}, {}}}
	source := ffmpeg.Probe{Duration: 600}
	good := ffmpeg.Probe{Duration: 599.5, Streams: []ffmpeg.Stream{
		{Type: "video", Codec: "h264"}, {Type: "audio", Codec: "aac"}, {Type: "audio", Codec: "aac"},
	}}
	if err := CheckConverted(good, source, plan); err != nil {
		t.Errorf("a good file was rejected: %v", err)
	}
	for name, got := range map[string]ffmpeg.Probe{
		"wrong codec":    {Duration: 600, Streams: []ffmpeg.Stream{{Type: "video", Codec: "hevc"}, {Type: "audio"}, {Type: "audio"}}},
		"lost a track":   {Duration: 600, Streams: []ffmpeg.Stream{{Type: "video", Codec: "h264"}, {Type: "audio"}}},
		"cut short":      {Duration: 300, Streams: good.Streams},
		"no video":       {Duration: 600, Streams: []ffmpeg.Stream{{Type: "audio"}, {Type: "audio"}}},
		"only cover art": {Duration: 600, Streams: []ffmpeg.Stream{{Type: "video", Codec: "h264", AttachPic: true}, {Type: "audio"}, {Type: "audio"}}},
	} {
		if err := CheckConverted(got, source, plan); err == nil {
			t.Errorf("%s: accepted, want an error (the original would be deleted)", name)
		}
	}
	// Unknown source length: the duration check is skipped, the rest still applies.
	if err := CheckConverted(good, ffmpeg.Probe{}, plan); err != nil {
		t.Errorf("unknown source duration: %v", err)
	}
}

func TestTextSubtitleCodecsLoaded(t *testing.T) {
	for _, codec := range []string{"subrip", "ass", "mov_text", "webvtt"} {
		if !textSubtitleCodecs.Contains(codec) {
			t.Errorf("text_subtitle_codecs.txt does not have %q", codec)
		}
	}
	if textSubtitleCodecs.Contains("hdmv_pgs_subtitle") {
		t.Error("a bitmap codec counts as text")
	}
}

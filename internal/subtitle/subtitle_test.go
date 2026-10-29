package subtitle

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseSRT(t *testing.T) {
	in := "\xef\xbb\xbf1\r\n00:00:01,000 --> 00:00:04,250\r\nHello there!\r\n\r\n2\r\n00:01:05,5 --> 00:01:08,500\r\nTwo\r\nlines\r\n"
	got, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []Cue{
		{StartMS: 1000, EndMS: 4250, Text: "Hello there!"},
		{StartMS: 65500, EndMS: 68500, Text: "Two\nlines"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseVTT(t *testing.T) {
	in := `WEBVTT - with a title

NOTE this is a comment
spanning two lines

STYLE
::cue { color: yellow }

intro
00:01.000 --> 00:02.000 align:start position:10%
First

01:00:00.000 --> 01:00:01.500
Last
`
	got, err := Parse([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	want := []Cue{
		{StartMS: 1000, EndMS: 2000, Text: "First"},
		{StartMS: 3600000, EndMS: 3601500, Text: "Last"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestParseLatin1(t *testing.T) {
	in := []byte("1\n00:00:01,000 --> 00:00:02,000\nCaf\xe9\n")
	got, err := Parse(in)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Text != "Café" {
		t.Errorf("got %q, want Café", got[0].Text)
	}
}

func TestParseErrors(t *testing.T) {
	if _, err := Parse([]byte("just some text\n")); !errors.Is(err, ErrNoCues) {
		t.Errorf("plain text: got %v, want ErrNoCues", err)
	}
	if _, err := Parse([]byte("1\nnot a time --> 00:00:01,000\nx\n")); err == nil {
		t.Error("bad timestamp: expected an error")
	}
}

func TestWriteVTTRoundTrip(t *testing.T) {
	cues := []Cue{
		{StartMS: 0, EndMS: 1500, Text: "Hello"},
		{StartMS: 3723004, EndMS: 3725000, Text: "Line one\nLine two"},
	}
	out := string(WriteVTT(cues))
	if !strings.HasPrefix(out, "WEBVTT\n\n00:00:00.000 --> 00:00:01.500\nHello\n\n") {
		t.Errorf("unexpected output:\n%s", out)
	}
	back, err := Parse([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(back, cues) {
		t.Errorf("round trip: got %+v, want %+v", back, cues)
	}
}

func TestWriteVTTCannotBreakOutOfCue(t *testing.T) {
	out := string(WriteVTT([]Cue{{StartMS: 0, EndMS: 1000, Text: "a\n\n00:00:05.000 --> 00:00:06.000\ninjected"}}))
	back, err := Parse([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != 1 {
		t.Fatalf("cue text created %d cues, want 1:\n%s", len(back), out)
	}
}

func TestLanguageName(t *testing.T) {
	for code, want := range map[string]string{"en": "English", "RO": "Română", "pt-BR": "Português", "xx": "XX", "und": "Subtitles"} {
		if got := LanguageName(code); got != want {
			t.Errorf("LanguageName(%q) = %q, want %q", code, got, want)
		}
	}
}

func TestLanguageNamesLoaded(t *testing.T) {
	for code, want := range map[string]string{"de": "Deutsch", "ger": "Deutsch", "ja": "日本語", "pt-br": "Português", "xx": "XX", "": "Subtitles"} {
		if got := LanguageName(code); got != want {
			t.Errorf("LanguageName(%q) = %q; want %q", code, got, want)
		}
	}
	if len(languageNames) < 38 {
		t.Errorf("language_names.txt loaded %d codes; want at least 38", len(languageNames))
	}
}

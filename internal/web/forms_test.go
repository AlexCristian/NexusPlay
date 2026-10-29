package web

import (
	"bytes"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// file is an upload in a test form.
type file struct {
	field, name string
	data        []byte
}

// formRequest builds a multipart POST like the admin forms send.
func formRequest(t *testing.T, fields map[string]string, files ...file) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range fields {
		mw.WriteField(k, v)
	}
	for _, f := range files {
		fw, err := mw.CreateFormFile(f.field, f.name)
		if err != nil {
			t.Fatal(err)
		}
		fw.Write(f.data)
	}
	mw.Close()
	r := httptest.NewRequest(http.MethodPost, "/admin/media/1", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	return r
}

var onePixelPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

// wantFormError checks that err is a *formError whose message contains want,
// or that err is nil when want is "".
func wantFormError(t *testing.T, err error, want string) {
	t.Helper()
	var fe *formError
	switch {
	case want == "" && err != nil:
		t.Errorf("unexpected error: %v", err)
	case want != "" && !errors.As(err, &fe):
		t.Errorf("error = %v, want a form error containing %q", err, want)
	case want != "" && !strings.Contains(fe.message, want):
		t.Errorf("message = %q, want it to contain %q", fe.message, want)
	}
}

func TestDecodeMediaForm(t *testing.T) {
	for _, tc := range []struct {
		name    string
		fields  map[string]string
		files   []file
		wantErr string
		check   func(*testing.T, mediaInput)
	}{
		{
			name:   "trimmed fields",
			fields: map[string]string{"title": "  Pilot ", "collection": " Cosmos ", "season": "1", "episode": " 2 ", "publish": "1"},
			check: func(t *testing.T, in mediaInput) {
				if in.update.Title != "Pilot" || in.collection != "Cosmos" || in.update.Season != 1 || in.update.Episode != 2 || !in.publish {
					t.Errorf("decoded %+v", in)
				}
			},
		},
		{
			name:   "blank season is zero",
			fields: map[string]string{"title": "Alien", "season": ""},
			check: func(t *testing.T, in mediaInput) {
				if in.update.Season != 0 || in.publish || in.poster != nil {
					t.Errorf("decoded %+v", in)
				}
			},
		},
		{name: "no title", fields: map[string]string{"title": "  "}, wantErr: "title cannot be empty"},
		{
			name:   "episode range",
			fields: map[string]string{"title": "Encounter\nat  Farpoint", "season": "1", "episode": "1", "episode_end": "2"},
			check: func(t *testing.T, in mediaInput) {
				if in.update.Title != "Encounter at Farpoint" || in.update.Episode != 1 || in.update.EpisodeEnd != 2 {
					t.Errorf("decoded %+v", in.update)
				}
			},
		},
		{
			name:   "a range of one episode is one episode",
			fields: map[string]string{"title": "T", "episode": "4", "episode_end": "4"},
			check: func(t *testing.T, in mediaInput) {
				if in.update.EpisodeEnd != 0 {
					t.Errorf("EpisodeEnd = %d, want 0", in.update.EpisodeEnd)
				}
			},
		},
		{name: "range backwards", fields: map[string]string{"title": "T", "episode": "3", "episode_end": "2"}, wantErr: "must come after the first"},
		{name: "season not a number", fields: map[string]string{"title": "T", "season": "one"}, wantErr: "Season must be a number"},
		{name: "negative episode", fields: map[string]string{"title": "T", "episode": "-1"}, wantErr: "Episode must be a number"},
		{
			name:   "poster upload",
			fields: map[string]string{"title": "T"},
			files:  []file{{"poster", "p.png", onePixelPNG}},
			check: func(t *testing.T, in mediaInput) {
				if in.poster == nil || in.poster.ext != ".png" || in.update.RemovePoster {
					t.Errorf("poster = %+v, remove = %v", in.poster, in.update.RemovePoster)
				}
			},
		},
		{
			name:   "remove poster",
			fields: map[string]string{"title": "T", "remove_poster": "1"},
			check: func(t *testing.T, in mediaInput) {
				if in.poster != nil || !in.update.RemovePoster {
					t.Errorf("poster = %+v, remove = %v", in.poster, in.update.RemovePoster)
				}
			},
		},
		{
			name:    "upload and remove",
			fields:  map[string]string{"title": "T", "remove_poster": "1"},
			files:   []file{{"poster", "p.png", onePixelPNG}},
			wantErr: "not both",
		},
		{
			name:    "poster that is not an image",
			fields:  map[string]string{"title": "T"},
			files:   []file{{"poster", "p.png", []byte("<html>not an image</html>")}},
			wantErr: "JPEG, PNG or WebP",
		},
		{
			name:   "empty file input is no upload",
			fields: map[string]string{"title": "T"},
			files:  []file{{"poster", "", nil}},
			check: func(t *testing.T, in mediaInput) {
				if in.poster != nil {
					t.Errorf("poster = %+v", in.poster)
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in, err := decodeMediaForm(formRequest(t, tc.fields, tc.files...))
			wantFormError(t, err, tc.wantErr)
			if err == nil && tc.check != nil {
				tc.check(t, in)
			}
		})
	}
}

func TestDecodeCollectionForm(t *testing.T) {
	in, err := decodeCollectionForm(formRequest(t, map[string]string{"name": " Cosmos ", "category": "TV Shows", "lookup": "1"}))
	wantFormError(t, err, "")
	if in.update.Name != "Cosmos" || in.update.Category != "TV Shows" || !in.lookup {
		t.Errorf("decoded %+v", in)
	}
	_, err = decodeCollectionForm(formRequest(t, map[string]string{"name": "Cosmos"}))
	wantFormError(t, err, "needs a name and a category")
}

func TestDecodeSubtitleUpload(t *testing.T) {
	srt := []byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n")
	for _, tc := range []struct {
		name    string
		lang    string
		files   []file
		wantErr string
	}{
		{name: "srt", lang: " EN ", files: []file{{"file", "a.srt", srt}}},
		{name: "no language", lang: "", files: []file{{"file", "a.srt", srt}}, wantErr: "language code"},
		{name: "language too long", lang: "abcdefghijklm", files: []file{{"file", "a.srt", srt}}, wantErr: "language code"},
		{name: "no file", lang: "en", wantErr: "Choose a subtitle file"},
		{name: "not a subtitle", lang: "en", files: []file{{"file", "a.srt", []byte("hello")}}, wantErr: "not a valid SRT or WebVTT"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lang, cues, err := decodeSubtitleUpload(formRequest(t, map[string]string{"language": tc.lang}, tc.files...))
			wantFormError(t, err, tc.wantErr)
			if err == nil && (lang != "en" || len(cues) != 1 || cues[0].Text != "Hello") {
				t.Errorf("lang = %q, cues = %+v", lang, cues)
			}
		})
	}
}

func TestDataFilesLoaded(t *testing.T) {
	if posterTypes["image/png"] != ".png" || len(posterTypes) != 3 {
		t.Errorf("poster_types.txt loaded %v; want jpeg, png and webp", posterTypes)
	}
	if doneMessages["scan"] != "Scan started. New files appear here within a minute." || len(doneMessages) != 4 {
		t.Errorf("done_messages.txt loaded %v", doneMessages)
	}
}

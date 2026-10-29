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
	var buffer bytes.Buffer
	multipartWriter := multipart.NewWriter(&buffer)
	for key, value := range fields {
		multipartWriter.WriteField(key, value)
	}
	for _, file := range files {
		writer, err := multipartWriter.CreateFormFile(file.field, file.name)
		if err != nil {
			t.Fatal(err)
		}
		writer.Write(file.data)
	}
	multipartWriter.Close()
	r := httptest.NewRequest(http.MethodPost, "/admin/media/1", &buffer)
	r.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	return r
}

var onePixelPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89")

// wantFormError checks that err is a *formError whose message contains want,
// or that err is nil when want is "".
func wantFormError(t *testing.T, err error, want string) {
	t.Helper()
	var formErr *formError
	switch {
	case want == "" && err != nil:
		t.Errorf("unexpected error: %v", err)
	case want != "" && !errors.As(err, &formErr):
		t.Errorf("error = %v, want a form error containing %q", err, want)
	case want != "" && !strings.Contains(formErr.message, want):
		t.Errorf("message = %q, want it to contain %q", formErr.message, want)
	}
}

func TestDecodeMediaForm(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		fields  map[string]string
		files   []file
		wantErr string
		check   func(*testing.T, mediaInput)
	}{
		{
			name:   "trimmed fields",
			fields: map[string]string{"title": "  Pilot ", "collection": " Cosmos ", "season": "1", "episode": " 2 ", "publish": "1"},
			check: func(t *testing.T, input mediaInput) {
				if input.update.Title != "Pilot" || input.collection != "Cosmos" || input.update.Season != 1 || input.update.Episode != 2 || !input.publish {
					t.Errorf("decoded %+v", input)
				}
			},
		},
		{
			name:   "blank season is zero",
			fields: map[string]string{"title": "Alien", "season": ""},
			check: func(t *testing.T, input mediaInput) {
				if input.update.Season != 0 || input.publish || input.poster != nil {
					t.Errorf("decoded %+v", input)
				}
			},
		},
		{name: "no title", fields: map[string]string{"title": "  "}, wantErr: "title cannot be empty"},
		{
			name:   "episode range",
			fields: map[string]string{"title": "Encounter\nat  Farpoint", "season": "1", "episode": "1", "episode_end": "2"},
			check: func(t *testing.T, input mediaInput) {
				if input.update.Title != "Encounter at Farpoint" || input.update.Episode != 1 || input.update.EpisodeEnd != 2 {
					t.Errorf("decoded %+v", input.update)
				}
			},
		},
		{
			name:   "a range of one episode is one episode",
			fields: map[string]string{"title": "T", "episode": "4", "episode_end": "4"},
			check: func(t *testing.T, input mediaInput) {
				if input.update.EpisodeEnd != 0 {
					t.Errorf("EpisodeEnd = %d, want 0", input.update.EpisodeEnd)
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
			check: func(t *testing.T, input mediaInput) {
				if input.poster == nil || input.poster.extension != ".png" || input.update.RemovePoster {
					t.Errorf("poster = %+v, remove = %v", input.poster, input.update.RemovePoster)
				}
			},
		},
		{
			name:   "remove poster",
			fields: map[string]string{"title": "T", "remove_poster": "1"},
			check: func(t *testing.T, input mediaInput) {
				if input.poster != nil || !input.update.RemovePoster {
					t.Errorf("poster = %+v, remove = %v", input.poster, input.update.RemovePoster)
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
			check: func(t *testing.T, input mediaInput) {
				if input.poster != nil {
					t.Errorf("poster = %+v", input.poster)
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			input, err := decodeMediaForm(formRequest(t, testCase.fields, testCase.files...))
			wantFormError(t, err, testCase.wantErr)
			if err == nil && testCase.check != nil {
				testCase.check(t, input)
			}
		})
	}
}

func TestDecodeCollectionForm(t *testing.T) {
	input, err := decodeCollectionForm(formRequest(t, map[string]string{"name": " Cosmos ", "category": "TV Shows", "lookup": "1"}))
	wantFormError(t, err, "")
	if input.update.Name != "Cosmos" || input.update.Category != "TV Shows" || !input.lookup {
		t.Errorf("decoded %+v", input)
	}
	_, err = decodeCollectionForm(formRequest(t, map[string]string{"name": "Cosmos"}))
	wantFormError(t, err, "needs a name and a category")
}

func TestDecodeSubtitleUpload(t *testing.T) {
	srt := []byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n")
	for _, testCase := range []struct {
		name     string
		language string
		files    []file
		wantErr  string
	}{
		{name: "srt", language: " EN ", files: []file{{"file", "a.srt", srt}}},
		{name: "no language", language: "", files: []file{{"file", "a.srt", srt}}, wantErr: "language code"},
		{name: "language too long", language: "abcdefghijklm", files: []file{{"file", "a.srt", srt}}, wantErr: "language code"},
		{name: "no file", language: "en", wantErr: "Choose a subtitle file"},
		{name: "not a subtitle", language: "en", files: []file{{"file", "a.srt", []byte("hello")}}, wantErr: "not a valid SRT or WebVTT"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			language, cues, err := decodeSubtitleUpload(formRequest(t, map[string]string{"language": testCase.language}, testCase.files...))
			wantFormError(t, err, testCase.wantErr)
			if err == nil && (language != "en" || len(cues) != 1 || cues[0].Text != "Hello") {
				t.Errorf("lang = %q, cues = %+v", language, cues)
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

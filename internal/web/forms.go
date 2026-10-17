package web

// Form decoders for the control panel. Each reads a submitted form and
// validates it without side effects: nothing is stored until the input is
// known to be good. Problems the user can fix are *formError, which the
// handler shows in the form again.

import (
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

// formError is a problem with submitted input that the user can fix. The
// handler shows the form again with the message instead of an error page.
type formError struct{ msg string }

func (e *formError) Error() string { return e.msg }

func formErr(msg string) error { return &formError{msg: msg} }

// mediaInput is the media edit form, decoded.
type mediaInput struct {
	// update has everything but CollectionID and Poster, which exist only
	// once the collection is found or created and the poster stored.
	update     store.MediaUpdate
	collection string        // a collection name; empty for none
	poster     *posterUpload // nil when no new poster was sent
	publish    bool
}

func decodeMediaForm(r *http.Request) (mediaInput, error) {
	if err := parseForm(r, maxUpload); err != nil {
		return mediaInput{}, formErr("The upload is too large or malformed.")
	}
	in := mediaInput{
		update: store.MediaUpdate{
			Title:    strings.TrimSpace(r.FormValue("title")),
			Year:     strings.TrimSpace(r.FormValue("year")),
			Overview: strings.TrimSpace(r.FormValue("overview")),
			Category: strings.TrimSpace(r.FormValue("category")),
		},
		collection: strings.TrimSpace(r.FormValue("collection")),
		publish:    r.FormValue("publish") == "1",
	}
	var ok bool
	if in.update.Season, ok = formCount(r, "season"); !ok {
		return in, formErr("Season must be a number.")
	}
	if in.update.Episode, ok = formCount(r, "episode"); !ok {
		return in, formErr("Episode must be a number.")
	}
	if in.update.Title == "" {
		return in, formErr("The title cannot be empty.")
	}
	var err error
	in.poster, in.update.RemovePoster, err = decodePoster(r)
	return in, err
}

// collectionInput is the collection form, decoded.
type collectionInput struct {
	update store.CollectionUpdate // Poster is set once the upload is stored
	poster *posterUpload          // nil when no new poster was sent
	lookup bool                   // fill empty fields from TMDB
}

func decodeCollectionForm(r *http.Request) (collectionInput, error) {
	if err := parseForm(r, maxUpload); err != nil {
		return collectionInput{}, formErr("The upload is too large or malformed.")
	}
	in := collectionInput{
		update: store.CollectionUpdate{
			Name:     strings.TrimSpace(r.FormValue("name")),
			Category: strings.TrimSpace(r.FormValue("category")),
			Year:     strings.TrimSpace(r.FormValue("year")),
			Overview: strings.TrimSpace(r.FormValue("overview")),
		},
		lookup: r.FormValue("lookup") == "1",
	}
	if in.update.Name == "" || in.update.Category == "" {
		return in, formErr("A collection needs a name and a category.")
	}
	var err error
	in.poster, in.update.RemovePoster, err = decodePoster(r)
	return in, err
}

// decodeSubtitleUpload reads the subtitle upload form: a language code and
// an SRT or WebVTT file.
func decodeSubtitleUpload(r *http.Request) (lang string, cues []subtitle.Cue, err error) {
	if err := parseForm(r, maxUpload); err != nil {
		return "", nil, formErr("The upload is too large or malformed.")
	}
	lang = strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	if lang == "" || len(lang) > 12 {
		return "", nil, formErr("Enter a language code such as en or ro.")
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return "", nil, formErr("Choose a subtitle file to upload.")
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return "", nil, formErr("Could not read the uploaded file.")
	}
	if cues, err = subtitle.Parse(data); err != nil {
		return "", nil, formErr("That file is not a valid SRT or WebVTT subtitle: " + err.Error())
	}
	return lang, cues, nil
}

// posterUpload is an uploaded poster whose content is a supported image.
type posterUpload struct {
	file *multipart.FileHeader
	ext  string // from the content, not the file name
}

var posterTypes = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}

// decodePoster reads the poster upload and the "Remove the current poster"
// box, which cannot both be used at once.
func decodePoster(r *http.Request) (upload *posterUpload, remove bool, err error) {
	remove = r.FormValue("remove_poster") == "1"
	var fh *multipart.FileHeader
	if r.MultipartForm != nil {
		for _, f := range r.MultipartForm.File["poster"] {
			if f.Size > 0 {
				fh = f
				break
			}
		}
	}
	if fh == nil {
		return nil, remove, nil
	}
	if remove {
		return nil, false, formErr("Upload a new poster or remove the current one, not both.")
	}
	f, err := fh.Open()
	if err != nil {
		return nil, false, formErr("Could not read the uploaded poster.")
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head) // a file shorter than 512 bytes is fine: sniff what there is
	ext, ok := posterTypes[http.DetectContentType(head[:n])]
	if !ok {
		return nil, false, formErr("Posters must be JPEG, PNG or WebP images.")
	}
	return &posterUpload{file: fh, ext: ext}, false, nil
}

// formCount reads an optional whole number that cannot be negative, such
// as a season. A blank field is 0.
func formCount(r *http.Request, key string) (int, bool) {
	v := strings.TrimSpace(r.FormValue(key))
	if v == "" {
		return 0, true
	}
	n, err := strconv.Atoi(v)
	return n, err == nil && n >= 0
}

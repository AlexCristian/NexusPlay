package web

// Form decoders for the control panel. Each reads a submitted form and
// validates it without side effects: nothing is stored until the input is
// known to be good. Problems the user can fix are *formError, which the
// handler shows in the form again.

import (
	_ "embed"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"

	"github.com/AlexCristian/NexusPlay/internal/datafile"
	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

// formError is a problem with submitted input that the user can fix. The
// handler shows the form again with the message instead of an error page.
type formError struct{ message string }

func (formError *formError) Error() string { return formError.message }

func formErr(message string) error { return &formError{message: message} }

// mediaInput is the media edit form, decoded.
type mediaInput struct {
	// update has everything but CollectionID and Poster, which exist only
	// once the collection is found or created and the poster stored.
	update     store.MediaUpdate
	collection string        // a collection name; empty for none
	poster     *posterUpload // nil when no new poster was sent
	publish    bool
}

// decodeMediaForm reads POST /admin/media/{id}.
func decodeMediaForm(r *http.Request) (mediaInput, error) {
	if err := parseForm(r, maxUpload); err != nil {
		return mediaInput{}, formErr("The upload is too large or malformed.")
	}
	input := mediaInput{
		update: store.MediaUpdate{
			Title:    oneLine(r.FormValue("title")),
			Year:     strings.TrimSpace(r.FormValue("year")),
			Overview: strings.TrimSpace(r.FormValue("overview")),
			Category: strings.TrimSpace(r.FormValue("category")),
		},
		collection: strings.TrimSpace(r.FormValue("collection")),
		publish:    r.FormValue("publish") == "1",
	}
	var valid bool
	if input.update.Season, valid = formCount(r, "season"); !valid {
		return input, formErr("Season must be a number.")
	}
	if input.update.Episode, valid = formCount(r, "episode"); !valid {
		return input, formErr("Episode must be a number.")
	}
	if input.update.EpisodeEnd, valid = formCount(r, "episode_end"); !valid {
		return input, formErr("The last episode must be a number.")
	}
	if input.update.EpisodeEnd == input.update.Episode {
		input.update.EpisodeEnd = 0 // "1 to 1" is one episode
	}
	if input.update.EpisodeEnd != 0 && input.update.EpisodeEnd < input.update.Episode {
		return input, formErr("The last episode must come after the first.")
	}
	if input.update.Title == "" {
		return input, formErr("The title cannot be empty.")
	}
	var err error
	input.poster, input.update.RemovePoster, err = decodePoster(r)
	return input, err
}

// collectionInput is the collection form, decoded.
type collectionInput struct {
	update store.CollectionUpdate // Poster is set once the upload is stored
	poster *posterUpload          // nil when no new poster was sent
	lookup bool                   // fill empty fields from TMDB
}

// decodeCollectionForm reads the new-collection and edit-collection forms.
func decodeCollectionForm(r *http.Request) (collectionInput, error) {
	if err := parseForm(r, maxUpload); err != nil {
		return collectionInput{}, formErr("The upload is too large or malformed.")
	}
	input := collectionInput{
		update: store.CollectionUpdate{
			Name:     oneLine(r.FormValue("name")),
			Category: strings.TrimSpace(r.FormValue("category")),
			Year:     strings.TrimSpace(r.FormValue("year")),
			Overview: strings.TrimSpace(r.FormValue("overview")),
		},
		lookup: r.FormValue("lookup") == "1",
	}
	if input.update.Name == "" || input.update.Category == "" {
		return input, formErr("A collection needs a name and a category.")
	}
	var err error
	input.poster, input.update.RemovePoster, err = decodePoster(r)
	return input, err
}

// decodeSubtitleUpload reads the subtitle upload form: a language code and
// an SRT or WebVTT file.
func decodeSubtitleUpload(r *http.Request) (language string, cues []subtitle.Cue, err error) {
	if err := parseForm(r, maxUpload); err != nil {
		return "", nil, formErr("The upload is too large or malformed.")
	}
	language = strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	if language == "" || len(language) > 12 {
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
	return language, cues, nil
}

// posterUpload is an uploaded poster whose content is a supported image.
type posterUpload struct {
	file      *multipart.FileHeader
	extension string // from the content, not the file name
}

// [posterTypesFile] is the contents of poster_types.txt, built into the
// program by the compiler. The build fails if the file is missing.
//
//go:embed poster_types.txt
var posterTypesFile string

// [posterTypes] maps the image types accepted as poster uploads to the
// extension each is saved with, from poster_types.txt.
var posterTypes = datafile.Table("poster_types.txt", posterTypesFile)

// decodePoster reads the poster upload and the "Remove the current poster"
// box, which cannot both be used at once.
func decodePoster(r *http.Request) (upload *posterUpload, remove bool, err error) {
	remove = r.FormValue("remove_poster") == "1"
	var posterFile *multipart.FileHeader
	if r.MultipartForm != nil {
		for _, fileHeader := range r.MultipartForm.File["poster"] {
			if fileHeader.Size > 0 {
				posterFile = fileHeader
				break
			}
		}
	}
	if posterFile == nil {
		return nil, remove, nil
	}
	if remove {
		return nil, false, formErr("Upload a new poster or remove the current one, not both.")
	}
	file, err := posterFile.Open()
	if err != nil {
		return nil, false, formErr("Could not read the uploaded poster.")
	}
	defer file.Close()
	head := make([]byte, 512)
	bytesRead, _ := io.ReadFull(file, head) // a file shorter than 512 bytes is fine: sniff what there is
	extension, allowed := posterTypes[http.DetectContentType(head[:bytesRead])]
	if !allowed {
		return nil, false, formErr("Posters must be JPEG, PNG or WebP images.")
	}
	return &posterUpload{file: posterFile, extension: extension}, false, nil
}

// oneLine trims s and joins its lines and runs of spaces with single spaces:
// titles are edited in a growing text area, where Enter adds a line break.
func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

// formCount reads an optional whole number that cannot be negative, such
// as a season. A blank field is 0.
func formCount(r *http.Request, key string) (int, bool) {
	value := strings.TrimSpace(r.FormValue(key))
	if value == "" {
		return 0, true
	}
	number, err := strconv.Atoi(value)
	return number, err == nil && number >= 0
}

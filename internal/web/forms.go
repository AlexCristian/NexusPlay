package web

// Form decoders for the control panel. Each reads a submitted form and
// validates it without side effects: nothing is stored until the input is
// known to be good. Problems the user can fix are a [*formError], which the
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

// [formError] is a problem with submitted input that the user can fix. The
// handler shows the form again with the message instead of an error page.
type formError struct {
	// [message] is the sentence shown above the form.
	message string
}

// [Error] gives the message to show.
//
// Returns:
//   - [string]: [formError.message].
func (formError *formError) Error() string { return formError.message }

// [newFormError] reports a problem with the input that the user can fix.
//
// Parameters:
//   - [message]: the sentence shown above the form.
//
// Returns:
//   - [error]: a [*formError] with the message.
func newFormError(message string) error { return &formError{message: message} }

// [mediaInput] is the media edit form, decoded by [decodeMediaForm].
type mediaInput struct {
	// [update] has every field except [store.MediaUpdate.CollectionID] and
	// [store.MediaUpdate.Poster], which exist only once the collection is found
	// or created and the poster stored.
	update store.MediaUpdate
	// [collection] is the collection's name as typed, or "" for none.
	collection string
	// [poster] is the new poster upload, or [nil] if none was sent.
	poster *posterUpload
	// [publish] is [true] when the "Publish" button was used.
	publish bool
}

// [decodeMediaForm] reads and checks the media edit form (POST
// /admin/media/{id}). The season, episode and last episode must be whole
// numbers of 0 or more; a last episode equal to the first means one episode,
// and one below the first (other than 0) is refused; the title must not be
// empty; and the poster fields must pass [decodePoster]. The title is joined onto one line and other
// fields are trimmed.
//
// Parameters:
//   - [r]: the request with the form, at most [maxUpload] bytes.
//
// Returns:
//   - [mediaInput]: the decoded form, filled as far as it was read, or a
//     zero [mediaInput] if the body could not be parsed. On an error the
//     caller shows the stored item again, not this value.
//   - [error]: [nil] if the form is valid, or a [*formError] saying what to
//     fix.
func decodeMediaForm(r *http.Request) (mediaInput, error) {
	if err := parseForm(r, maxUpload); err != nil {
		return mediaInput{}, newFormError("The upload is too large or malformed.")
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
	if input.update.Season, valid = formWholeNumber(r, "season"); !valid {
		return input, newFormError("Season must be a number.")
	}
	if input.update.Episode, valid = formWholeNumber(r, "episode"); !valid {
		return input, newFormError("Episode must be a number.")
	}
	if input.update.EpisodeEnd, valid = formWholeNumber(r, "episode_end"); !valid {
		return input, newFormError("The last episode must be a number.")
	}
	if input.update.EpisodeEnd == input.update.Episode {
		input.update.EpisodeEnd = 0 // "1 to 1" is one episode
	}
	if input.update.EpisodeEnd != 0 && input.update.EpisodeEnd < input.update.Episode {
		return input, newFormError("The last episode must come after the first.")
	}
	if input.update.Title == "" {
		return input, newFormError("The title cannot be empty.")
	}
	var err error
	input.poster, input.update.RemovePoster, err = decodePoster(r)
	return input, err
}

// [collectionInput] is the collection form, decoded by
// [decodeCollectionForm].
type collectionInput struct {
	// [update] has every field except [store.CollectionUpdate.Poster], which is
	// set once the upload is stored.
	update store.CollectionUpdate
	// [poster] is the new poster upload, or [nil] if none was sent.
	poster *posterUpload
	// [lookup] is [true] when the form asks to fill empty fields from TMDB.
	lookup bool
}

// [decodeCollectionForm] reads and checks the new-collection and
// edit-collection forms. The name and category are required.
//
// Parameters:
//   - [r]: the request with the form, at most [maxUpload] bytes.
//
// Returns:
//   - [collectionInput]: the decoded form, filled as far as it was read, or
//     a zero [collectionInput] if the body could not be parsed.
//   - [error]: [nil] if the form is valid, or a [*formError] saying what to
//     fix.
func decodeCollectionForm(r *http.Request) (collectionInput, error) {
	if err := parseForm(r, maxUpload); err != nil {
		return collectionInput{}, newFormError("The upload is too large or malformed.")
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
		return input, newFormError("A collection needs a name and a category.")
	}
	var err error
	input.poster, input.update.RemovePoster, err = decodePoster(r)
	return input, err
}

// [decodeSubtitleUpload] reads and checks the subtitle upload form: a
// language code and an SRT or WebVTT file.
//
// Parameters:
//   - [r]: the request with the form, at most [maxUpload] bytes.
//
// Returns:
//   - [string] ([language]): the language code, lowercased, or "" if
//     [error] is not [nil].
//   - [[]subtitle.Cue] ([cues]): the file's lines, or [nil] if [error] is
//     not [nil].
//   - [error] ([err]): [nil] if the form is valid, or a [*formError] if the
//     upload is too large or malformed, the language is empty or longer than
//     [maxLanguageCodeLength], the file is missing or unreadable, or it is
//     not a valid subtitle file.
func decodeSubtitleUpload(r *http.Request) (language string, cues []subtitle.Cue, err error) {
	if err := parseForm(r, maxUpload); err != nil {
		return "", nil, newFormError("The upload is too large or malformed.")
	}
	language = strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	if language == "" || len(language) > maxLanguageCodeLength {
		return "", nil, newFormError("Enter a language code such as en or ro.")
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return "", nil, newFormError("Choose a subtitle file to upload.")
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return "", nil, newFormError("Could not read the uploaded file.")
	}
	if cues, err = subtitle.Parse(data); err != nil {
		return "", nil, newFormError("That file is not a valid SRT or WebVTT subtitle: " + err.Error())
	}
	return language, cues, nil
}

// [maxLanguageCodeLength] is the longest language code a subtitle upload may
// give, in bytes, enough for a code with a region or script such as
// "zh-hant-tw".
const maxLanguageCodeLength = 12

// [sniffLength] is how many bytes of an upload [http.DetectContentType]
// looks at.
const sniffLength = 512

// [posterUpload] is an uploaded poster whose content is a supported image.
type posterUpload struct {
	// [file] is the uploaded file.
	file *multipart.FileHeader
	// [extension] is the extension to save it with, such as ".jpg", decided from
	// its content, not its name.
	extension string
}

// [posterTypesFile] is the contents of poster_types.txt, built into the
// program by the compiler. The build fails if the file is missing.
//
//go:embed poster_types.txt
var posterTypesFile string

// [posterTypes] maps the image types accepted as poster uploads to the
// extension each is saved with, from poster_types.txt.
var posterTypes = datafile.Table("poster_types.txt", posterTypesFile)

// [decodePoster] reads a form's poster upload and its "Remove the current
// poster" box, which cannot both be used at once. Only the first non-empty
// file in the "poster" field counts, and it must be a JPEG, PNG or WebP
// image by its content (see poster_types.txt).
//
// Parameters:
//   - [r]: the request, whose multipart form has already been parsed.
//
// Returns:
//   - [*posterUpload] ([upload]): the new poster, or [nil] if none was sent
//     or [error] is not [nil].
//   - [bool] ([remove]): [true] if the box asks to remove the poster and no
//     new one was sent.
//   - [error] ([err]): [nil] if the poster fields are valid, or a
//     [*formError] if both were used, the file cannot be read, or it is not
//     a supported image.
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
		return nil, false, newFormError("Upload a new poster or remove the current one, not both.")
	}
	file, err := posterFile.Open()
	if err != nil {
		return nil, false, newFormError("Could not read the uploaded poster.")
	}
	defer file.Close()
	head := make([]byte, sniffLength)
	bytesRead, _ := io.ReadFull(file, head) // a shorter file is fine: sniff what there is
	extension, allowed := posterTypes[http.DetectContentType(head[:bytesRead])]
	if !allowed {
		return nil, false, newFormError("Posters must be JPEG, PNG or WebP images.")
	}
	return &posterUpload{file: posterFile, extension: extension}, false, nil
}

// [oneLine] joins text onto one line: titles are edited in a growing text
// area, where Enter adds a line break.
//
// Parameters:
//   - [text]: the text as typed.
//
// Returns:
//   - [string]: [text] with spaces around it trimmed and every run of spaces
//     and line breaks made a single space.
func oneLine(text string) string { return strings.Join(strings.Fields(text), " ") }

// [formWholeNumber] reads an optional form field holding a whole number that
// cannot be negative, such as a season.
//
// Parameters:
//   - [r]: the request with the form.
//   - [key]: the field's name.
//
// Returns:
//   - [int]: the number, or 0 for a blank or missing field.
//   - [bool]: [true] if the field is blank or a number of 0 or more, [false]
//     otherwise, in which case the [int] must not be used.
func formWholeNumber(r *http.Request, key string) (int, bool) {
	value := strings.TrimSpace(r.FormValue(key))
	if value == "" {
		return 0, true
	}
	number, err := strconv.Atoi(value)
	return number, err == nil && number >= 0
}

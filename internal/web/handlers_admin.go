package web

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/auth"
	"github.com/AlexCristian/NexusPlay/internal/metadata"
	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

// formError is a problem with submitted input that the user can fix. The
// handler shows the form again with the message instead of an error page.
type formError struct{ msg string }

func (e *formError) Error() string { return e.msg }

func formErr(msg string) error { return &formError{msg: msg} }

// controlPanel handles GET /admin: media waiting to be published, the
// collections, and the conversion queue.
func (s *Server) controlPanel(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	pending, err := s.Store.PendingMedia(ctx)
	if err != nil {
		return err
	}
	colls, err := s.Store.Collections(ctx)
	if err != nil {
		return err
	}
	cats, err := s.Store.CategoryNames(ctx)
	if err != nil {
		return err
	}
	conv, err := s.conversionView(ctx)
	if err != nil {
		return err
	}
	s.render(w, r, http.StatusOK, controlPanel(s.page(r, "Control panel"), pending, colls, cats, s.Meta.Enabled(), conv,
		doneMessages[r.URL.Query().Get("done")]))
	return nil
}

// doneMessages confirm an action after the redirect back to /admin?done=.
var doneMessages = map[string]string{
	"scan":      "Scan started. New files appear here within a minute.",
	"published": "Published.",
	"deleted":   "Collection deleted.",
	"retry":     "Queued for another conversion attempt.",
}

func (s *Server) conversionView(ctx context.Context) (ConversionView, error) {
	var v ConversionView
	var err error
	if v.Status, err = s.Converter.Status(ctx); err != nil {
		return v, err
	}
	v.Failures, err = s.Store.ConversionFailures(ctx)
	return v, err
}

// conversionsPanel handles GET /admin/conversions, polled by the control
// panel while conversions run.
func (s *Server) conversionsPanel(w http.ResponseWriter, r *http.Request) error {
	v, err := s.conversionView(r.Context())
	if err != nil {
		return err
	}
	s.render(w, r, http.StatusOK, conversionsSection(viewer(r).CSRF, v))
	return nil
}

// retryConversion handles POST /admin/media/{id}/retry-conversion.
func (s *Server) retryConversion(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.Store.RetryConversion(r.Context(), id); err != nil {
		return err
	}
	s.Converter.Wake()
	http.Redirect(w, r, "/admin?done=retry", http.StatusSeeOther)
	return nil
}

// scanNow handles POST /admin/scan.
func (s *Server) scanNow(w http.ResponseWriter, r *http.Request) error {
	s.Scanner.Trigger()
	http.Redirect(w, r, "/admin?done=scan", http.StatusSeeOther)
	return nil
}

func (s *Server) mediaForm(ctx context.Context, m store.Media) (MediaForm, error) {
	f := MediaForm{Media: m, CollectionName: m.CollectionName, MetaEnabled: s.Meta.Enabled()}
	var err error
	if f.Collections, err = s.Store.Collections(ctx); err != nil {
		return f, err
	}
	if f.Categories, err = s.Store.CategoryNames(ctx); err != nil {
		return f, err
	}
	if f.Subtitles, err = s.Store.Subtitles(ctx, m.ID); err != nil {
		return f, err
	}
	if !m.Published {
		// Prefill from the file name: an episode's show becomes its collection.
		g := guessFor(m)
		if g.IsEpisode() && f.CollectionName == "" {
			f.CollectionName = g.Show
			for _, c := range f.Collections {
				if strings.EqualFold(c.Name, g.Show) {
					f.CollectionName = c.Name
				}
			}
		}
		if f.Media.Category == "" && !g.IsEpisode() {
			f.Media.Category = "Movies"
		}
	}
	return f, nil
}

// editMediaPage handles GET /admin/media/{id}.
func (s *Server) editMediaPage(w http.ResponseWriter, r *http.Request) error {
	m, err := s.visibleMedia(r)
	if err != nil {
		return err
	}
	f, err := s.mediaForm(r.Context(), m)
	if err != nil {
		return err
	}
	f.Saved = r.URL.Query().Get("saved") == "1"
	s.render(w, r, http.StatusOK, editMediaPage(s.page(r, "Edit "+m.Title), f))
	return nil
}

// showMediaFormError shows the edit page again when err is a *formError,
// and returns any other error unchanged.
func (s *Server) showMediaFormError(w http.ResponseWriter, r *http.Request, m store.Media, err error) error {
	var fe *formError
	if !errors.As(err, &fe) {
		return err
	}
	f, ferr := s.mediaForm(r.Context(), m)
	if ferr != nil {
		return ferr
	}
	f.Error = fe.msg
	s.render(w, r, http.StatusBadRequest, editMediaPage(s.page(r, "Edit "+m.Title), f))
	return nil
}

// saveMedia handles POST /admin/media/{id}: the edit form, whose second
// button also publishes.
func (s *Server) saveMedia(w http.ResponseWriter, r *http.Request) error {
	m, err := s.visibleMedia(r)
	if err != nil {
		return err
	}
	publish := r.FormValue("publish") == "1"
	if err := s.updateMediaFromForm(r, m.ID, publish); err != nil {
		return s.showMediaFormError(w, r, m, err)
	}
	if publish {
		http.Redirect(w, r, "/admin?done=published", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/admin/media/"+itoa(m.ID)+"?saved=1", http.StatusSeeOther)
	}
	return nil
}

func (s *Server) updateMediaFromForm(r *http.Request, id int64, publish bool) error {
	if err := parseForm(r, maxUpload); err != nil {
		return formErr("The upload is too large or malformed.")
	}
	u := store.MediaUpdate{
		Title:    strings.TrimSpace(r.FormValue("title")),
		Year:     strings.TrimSpace(r.FormValue("year")),
		Overview: strings.TrimSpace(r.FormValue("overview")),
		Category: strings.TrimSpace(r.FormValue("category")),
	}
	var err error
	if u.Season, err = formInt(r, "season"); err != nil {
		return formErr("Season must be a number.")
	}
	if u.Episode, err = formInt(r, "episode"); err != nil {
		return formErr("Episode must be a number.")
	}
	if u.Title == "" {
		return formErr("The title cannot be empty.")
	}
	ctx := r.Context()
	if name := strings.TrimSpace(r.FormValue("collection")); name != "" {
		c, err := s.findOrCreateCollection(ctx, name, u.Category, u.Year)
		if err != nil {
			return err
		}
		u.CollectionID = c.ID
	}
	if u.Poster, err = s.savePoster(r, "poster"); err != nil {
		return err
	}
	return s.Store.UpdateMedia(ctx, id, u, publish)
}

// findOrCreateCollection returns the named collection, creating it (with
// TMDB details when available) if it does not exist yet.
func (s *Server) findOrCreateCollection(ctx context.Context, name, category, year string) (store.Collection, error) {
	c, err := s.Store.CollectionByName(ctx, name)
	if err == nil || !errors.Is(err, store.ErrNotFound) {
		return c, err
	}
	if category == "" {
		category = "TV Shows"
	}
	// For an episode, the year guessed from "Show (2019)/..." is the show's year.
	c = store.Collection{Name: name, Category: category, Year: year}
	s.fillFromTMDB(ctx, &c)
	if c.ID, err = s.Store.CreateCollection(ctx, c); errors.Is(err, store.ErrDuplicate) {
		return s.Store.CollectionByName(ctx, name)
	}
	return c, err
}

func (s *Server) fillFromTMDB(ctx context.Context, c *store.Collection) {
	if !s.Meta.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	info, err := s.Meta.Show(ctx, c.Name, "")
	if err != nil {
		if !errors.Is(err, metadata.ErrNoMatch) {
			s.Log.Warn("collection lookup failed", "name", c.Name, "err", err)
		}
		return
	}
	if c.Year == "" {
		c.Year = info.Year
	}
	if c.Overview == "" {
		c.Overview = info.Overview
	}
	if c.Poster == "" {
		c.Poster = info.Poster
	}
}

func formInt(r *http.Request, key string) (int, error) {
	v := strings.TrimSpace(r.FormValue(key))
	if v == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("bad %s", key)
	}
	return n, nil
}

var posterTypes = map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "image/webp": ".webp"}

// savePoster stores an uploaded image and returns its file name, or "" when
// none was uploaded. The type is checked from the content, not the name.
func (s *Server) savePoster(r *http.Request, field string) (string, error) {
	f, hdr, err := r.FormFile(field)
	if errors.Is(err, http.ErrMissingFile) {
		return "", nil
	} else if err != nil {
		return "", formErr("Could not read the uploaded poster.")
	}
	defer f.Close()
	if hdr.Size == 0 {
		return "", nil
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head) // a file shorter than 512 bytes is fine: sniff what there is
	ext, ok := posterTypes[http.DetectContentType(head[:n])]
	if !ok {
		return "", formErr("Posters must be JPEG, PNG or WebP images.")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	token, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	name := "upload-" + token[:16] + ext
	out, err := os.OpenFile(filepath.Join(s.Cfg.PosterDir(), name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(out, f); err != nil {
		out.Close()
		return "", err
	}
	return name, out.Close()
}

// uploadSubtitle handles POST /admin/media/{id}/subtitles: an SRT or
// WebVTT file for the video.
func (s *Server) uploadSubtitle(w http.ResponseWriter, r *http.Request) error {
	m, err := s.visibleMedia(r)
	if err != nil {
		return err
	}
	if err := s.addSubtitleFromForm(r, m.ID); err != nil {
		return s.showMediaFormError(w, r, m, err)
	}
	http.Redirect(w, r, "/admin/media/"+itoa(m.ID)+"?saved=1", http.StatusSeeOther)
	return nil
}

func (s *Server) addSubtitleFromForm(r *http.Request, mediaID int64) error {
	if err := parseForm(r, maxUpload); err != nil {
		return formErr("The upload is too large or malformed.")
	}
	lang := strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	if lang == "" || len(lang) > 12 {
		return formErr("Enter a language code such as en or ro.")
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return formErr("Choose a subtitle file to upload.")
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return formErr("Could not read the uploaded file.")
	}
	cues, err := subtitle.Parse(data)
	if err != nil {
		return formErr("That file is not a valid SRT or WebVTT subtitle: " + err.Error())
	}
	// An upload has no source, so it is always inserted.
	_, _, err = s.Store.AddSubtitle(r.Context(), store.NewSubtitle{MediaID: mediaID, Language: lang, Label: subtitle.LanguageName(lang), Cues: cues})
	return err
}

// saveSubtitle handles POST /admin/subtitles/{id}: a track's language and label.
func (s *Server) saveSubtitle(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	t, err := s.Store.SubtitleByID(r.Context(), id)
	if err != nil {
		return err
	}
	lang := strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	label := strings.TrimSpace(r.FormValue("label"))
	if lang == "" || label == "" {
		return &statusError{status: http.StatusBadRequest, title: "Missing fields",
			msg: "A subtitle track needs a language code and a label."}
	}
	if err := s.Store.UpdateSubtitleLabel(r.Context(), id, lang, label); err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/media/"+itoa(t.MediaID)+"?saved=1", http.StatusSeeOther)
	return nil
}

// deleteSubtitle handles POST /admin/subtitles/{id}/delete.
func (s *Server) deleteSubtitle(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	t, err := s.Store.SubtitleByID(r.Context(), id)
	if err != nil {
		return err
	}
	if err := s.Store.DeleteSubtitle(r.Context(), id); err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/media/"+itoa(t.MediaID)+"?saved=1", http.StatusSeeOther)
	return nil
}

// collectionFromForm reads the collection form. Problems the user can fix
// are *formError.
func (s *Server) collectionFromForm(r *http.Request) (store.Collection, error) {
	if err := parseForm(r, maxUpload); err != nil {
		return store.Collection{}, formErr("The upload is too large or malformed.")
	}
	c := store.Collection{
		Name:     strings.TrimSpace(r.FormValue("name")),
		Category: strings.TrimSpace(r.FormValue("category")),
		Year:     strings.TrimSpace(r.FormValue("year")),
		Overview: strings.TrimSpace(r.FormValue("overview")),
	}
	if c.Name == "" || c.Category == "" {
		return c, formErr("A collection needs a name and a category.")
	}
	var err error
	c.Poster, err = s.savePoster(r, "poster")
	return c, err
}

// createCollection handles POST /admin/collections.
func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) error {
	const title = "Could not create the collection"
	c, err := s.collectionFromForm(r)
	var fe *formError
	if errors.As(err, &fe) {
		return &statusError{status: http.StatusBadRequest, title: title, msg: fe.msg}
	}
	if err != nil {
		return err
	}
	if r.FormValue("lookup") == "1" {
		s.fillFromTMDB(r.Context(), &c)
	}
	id, err := s.Store.CreateCollection(r.Context(), c)
	if errors.Is(err, store.ErrDuplicate) {
		return &statusError{status: http.StatusConflict, title: title, msg: "A collection with that name already exists."}
	}
	if err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/collections/"+itoa(id), http.StatusSeeOther)
	return nil
}

// editCollectionPage handles GET /admin/collections/{id}.
func (s *Server) editCollectionPage(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	c, err := s.Store.CollectionByID(r.Context(), id)
	if err != nil {
		return err
	}
	cats, err := s.Store.CategoryNames(r.Context())
	if err != nil {
		return err
	}
	s.render(w, r, http.StatusOK, editCollectionPage(s.page(r, "Edit "+c.Name), c, cats, s.Meta.Enabled(), ""))
	return nil
}

// saveCollection handles POST /admin/collections/{id}.
func (s *Server) saveCollection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	existing, err := s.Store.CollectionByID(r.Context(), id)
	if err != nil {
		return err
	}
	c, err := s.collectionFromForm(r)
	if err == nil {
		c.ID = id
		if r.FormValue("lookup") == "1" {
			// The lookup only fills empty fields, so an existing poster is kept.
			if c.Poster == "" {
				c.Poster = existing.Poster
			}
			s.fillFromTMDB(r.Context(), &c)
		}
		err = s.Store.UpdateCollection(r.Context(), c)
		if errors.Is(err, store.ErrDuplicate) {
			err = formErr("Another collection already has that name.")
		}
	}
	var fe *formError
	if errors.As(err, &fe) {
		cats, err := s.Store.CategoryNames(r.Context())
		if err != nil {
			return err
		}
		existing.Name, existing.Category, existing.Year, existing.Overview = c.Name, c.Category, c.Year, c.Overview
		s.render(w, r, http.StatusBadRequest, editCollectionPage(s.page(r, "Edit collection"), existing, cats, s.Meta.Enabled(), fe.msg))
		return nil
	}
	if err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/collections/"+itoa(id), http.StatusSeeOther)
	return nil
}

// deleteCollection handles POST /admin/collections/{id}/delete. Its media
// stays in the library, outside any collection.
func (s *Server) deleteCollection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := s.Store.DeleteCollection(r.Context(), id); err != nil {
		return err
	}
	http.Redirect(w, r, "/admin?done=deleted", http.StatusSeeOther)
	return nil
}

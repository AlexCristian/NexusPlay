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

const maxUpload = 20 << 20

func (s *Server) controlPanel(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pending, err := s.Store.PendingMedia(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	colls, err := s.Store.Collections(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	cats, err := s.Store.CategoryNames(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	msg := ""
	switch r.URL.Query().Get("done") {
	case "scan":
		msg = "Scan started. New files appear here within a minute."
	case "published":
		msg = "Published."
	case "deleted":
		msg = "Collection deleted."
	}
	s.render(w, r, http.StatusOK, controlPanel(s.page(r, "Control panel"), pending, colls, cats, s.Meta.Enabled(), msg))
}

func (s *Server) scanNow(w http.ResponseWriter, r *http.Request) {
	s.Scanner.Trigger()
	http.Redirect(w, r, "/admin?done=scan", http.StatusSeeOther)
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

func (s *Server) editMediaPage(w http.ResponseWriter, r *http.Request) {
	m, ok := s.loadMedia(w, r)
	if !ok {
		return
	}
	f, err := s.mediaForm(r.Context(), m)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	f.Saved = r.URL.Query().Get("saved") == "1"
	s.render(w, r, http.StatusOK, editMediaPage(s.page(r, "Edit "+m.Title), f))
}

func (s *Server) saveMedia(w http.ResponseWriter, r *http.Request) {
	m, ok := s.loadMedia(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	fail := func(status int, msg string) {
		f, err := s.mediaForm(ctx, m)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		f.Error = msg
		s.render(w, r, status, editMediaPage(s.page(r, "Edit "+m.Title), f))
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		fail(http.StatusBadRequest, "The upload is too large or malformed.")
		return
	}
	u := store.MediaUpdate{
		Title:    strings.TrimSpace(r.FormValue("title")),
		Year:     strings.TrimSpace(r.FormValue("year")),
		Overview: strings.TrimSpace(r.FormValue("overview")),
		Category: strings.TrimSpace(r.FormValue("category")),
	}
	var err error
	if u.Season, err = formInt(r, "season"); err != nil {
		fail(http.StatusBadRequest, "Season must be a number.")
		return
	}
	if u.Episode, err = formInt(r, "episode"); err != nil {
		fail(http.StatusBadRequest, "Episode must be a number.")
		return
	}
	if u.Title == "" {
		fail(http.StatusBadRequest, "The title cannot be empty.")
		return
	}
	if name := strings.TrimSpace(r.FormValue("collection")); name != "" {
		c, err := s.findOrCreateCollection(ctx, name, u.Category)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		u.CollectionID = c.ID
	}
	if u.Poster, err = s.savePoster(r, "poster"); err != nil {
		fail(http.StatusBadRequest, err.Error())
		return
	}
	publish := r.FormValue("publish") == "1"
	if err := s.Store.UpdateMedia(ctx, m.ID, u, publish); err != nil {
		s.serverError(w, r, err)
		return
	}
	if publish {
		http.Redirect(w, r, "/admin?done=published", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/media/"+itoa(m.ID)+"?saved=1", http.StatusSeeOther)
}

// findOrCreateCollection returns the named collection, creating it (with
// TMDB details when available) if it does not exist yet.
func (s *Server) findOrCreateCollection(ctx context.Context, name, category string) (store.Collection, error) {
	c, err := s.Store.CollectionByName(ctx, name)
	if err == nil || !errors.Is(err, store.ErrNotFound) {
		return c, err
	}
	if category == "" {
		category = "TV Shows"
	}
	c = store.Collection{Name: name, Category: category}
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
		return "", errors.New("could not read the uploaded poster")
	}
	defer f.Close()
	if hdr.Size == 0 {
		return "", nil
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	ext, ok := posterTypes[http.DetectContentType(head[:n])]
	if !ok {
		return "", errors.New("posters must be JPEG, PNG or WebP images")
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

func (s *Server) uploadSubtitle(w http.ResponseWriter, r *http.Request) {
	m, ok := s.loadMedia(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	fail := func(msg string) {
		f, err := s.mediaForm(ctx, m)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		f.Error = msg
		s.render(w, r, http.StatusBadRequest, editMediaPage(s.page(r, "Edit "+m.Title), f))
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		fail("The upload is too large or malformed.")
		return
	}
	lang := strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	if lang == "" || len(lang) > 12 {
		fail("Enter a language code such as en or ro.")
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		fail("Choose a subtitle file to upload.")
		return
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		fail("Could not read the uploaded file.")
		return
	}
	cues, err := subtitle.Parse(data)
	if err != nil {
		fail("That file is not a valid SRT or WebVTT subtitle: " + err.Error())
		return
	}
	if _, err := s.Store.AddSubtitle(ctx, m.ID, lang, subtitle.LanguageName(lang), "", cues); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/media/"+itoa(m.ID)+"?saved=1", http.StatusSeeOther)
}

func (s *Server) saveSubtitle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	t, err := s.Store.SubtitleByID(r.Context(), id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	lang := strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	label := strings.TrimSpace(r.FormValue("label"))
	if lang == "" || label == "" {
		s.errorPage(w, r, http.StatusBadRequest, "Missing fields", "A subtitle track needs a language code and a label.")
		return
	}
	if err := s.Store.UpdateSubtitleLabel(r.Context(), id, lang, label); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/media/"+itoa(t.MediaID)+"?saved=1", http.StatusSeeOther)
}

func (s *Server) deleteSubtitle(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	t, err := s.Store.SubtitleByID(r.Context(), id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := s.Store.DeleteSubtitle(r.Context(), id); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/media/"+itoa(t.MediaID)+"?saved=1", http.StatusSeeOther)
}

func (s *Server) collectionFromForm(w http.ResponseWriter, r *http.Request) (store.Collection, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	if err := r.ParseMultipartForm(maxUpload); err != nil {
		return store.Collection{}, errors.New("the upload is too large or malformed")
	}
	c := store.Collection{
		Name:     strings.TrimSpace(r.FormValue("name")),
		Category: strings.TrimSpace(r.FormValue("category")),
		Year:     strings.TrimSpace(r.FormValue("year")),
		Overview: strings.TrimSpace(r.FormValue("overview")),
	}
	if c.Name == "" || c.Category == "" {
		return c, errors.New("a collection needs a name and a category")
	}
	var err error
	c.Poster, err = s.savePoster(r, "poster")
	return c, err
}

func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) {
	c, err := s.collectionFromForm(w, r)
	if err != nil {
		s.errorPage(w, r, http.StatusBadRequest, "Could not create the collection", capitalize(err.Error())+".")
		return
	}
	if r.FormValue("lookup") == "1" {
		s.fillFromTMDB(r.Context(), &c)
	}
	id, err := s.Store.CreateCollection(r.Context(), c)
	if errors.Is(err, store.ErrDuplicate) {
		s.errorPage(w, r, http.StatusConflict, "Could not create the collection", "A collection with that name already exists.")
		return
	} else if err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/collections/"+itoa(id), http.StatusSeeOther)
}

func (s *Server) editCollectionPage(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	c, err := s.Store.CollectionByID(r.Context(), id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	cats, err := s.Store.CategoryNames(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.render(w, r, http.StatusOK, editCollectionPage(s.page(r, "Edit "+c.Name), c, cats, s.Meta.Enabled(), ""))
}

func (s *Server) saveCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	existing, err := s.Store.CollectionByID(r.Context(), id)
	if err != nil {
		s.notFound(w, r)
		return
	}
	c, err := s.collectionFromForm(w, r)
	c.ID = id
	if err == nil && r.FormValue("lookup") == "1" {
		// The lookup only fills empty fields, so an existing poster is kept.
		if c.Poster == "" {
			c.Poster = existing.Poster
		}
		s.fillFromTMDB(r.Context(), &c)
	}
	if err == nil {
		err = s.Store.UpdateCollection(r.Context(), c)
		if errors.Is(err, store.ErrDuplicate) {
			err = errors.New("another collection already has that name")
		} else if err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	if err != nil {
		cats, _ := s.Store.CategoryNames(r.Context())
		existing.Name, existing.Category, existing.Year, existing.Overview = c.Name, c.Category, c.Year, c.Overview
		s.render(w, r, http.StatusBadRequest, editCollectionPage(s.page(r, "Edit collection"), existing, cats, s.Meta.Enabled(), capitalize(err.Error())+"."))
		return
	}
	http.Redirect(w, r, "/admin/collections/"+itoa(id), http.StatusSeeOther)
}

func (s *Server) deleteCollection(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		s.notFound(w, r)
		return
	}
	if err := s.Store.DeleteCollection(r.Context(), id); err != nil {
		s.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin?done=deleted", http.StatusSeeOther)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

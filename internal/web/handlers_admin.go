package web

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/auth"
	"github.com/AlexCristian/NexusPlay/internal/metadata"
	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

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
	s.render(w, r, http.StatusOK, controlPanel(newPage(r, "Control panel"), pending, colls, cats, s.Meta.Enabled(), conv,
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
	s.render(w, r, http.StatusOK, editMediaPage(newPage(r, "Edit "+m.Title), f))
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
	s.render(w, r, http.StatusBadRequest, editMediaPage(newPage(r, "Edit "+m.Title), f))
	return nil
}

// saveMedia handles POST /admin/media/{id}: the edit form, whose second
// button also publishes.
func (s *Server) saveMedia(w http.ResponseWriter, r *http.Request) error {
	m, err := s.visibleMedia(r)
	if err != nil {
		return err
	}
	in, err := decodeMediaForm(r)
	if err != nil {
		return s.showMediaFormError(w, r, m, err)
	}
	if err := s.applyMediaInput(r.Context(), m.ID, in); err != nil {
		return err
	}
	if in.publish {
		http.Redirect(w, r, "/admin?done=published", http.StatusSeeOther)
	} else {
		http.Redirect(w, r, "/admin/media/"+itoa(m.ID)+"?saved=1", http.StatusSeeOther)
	}
	return nil
}

// applyMediaInput saves a decoded edit form: it finds or creates the
// collection, stores the poster, then updates the media.
func (s *Server) applyMediaInput(ctx context.Context, id int64, in mediaInput) error {
	u := in.update
	if in.collection != "" {
		c, err := s.findOrCreateCollection(ctx, in.collection, u.Category, u.Year)
		if err != nil {
			return err
		}
		u.CollectionID = c.ID
	}
	var err error
	if u.Poster, err = s.savePoster(in.poster); err != nil {
		return err
	}
	return s.Store.UpdateMedia(ctx, id, u, in.publish)
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

// fillCollectionUpdate applies a TMDB lookup to an edit of existing. The
// lookup only fills empty fields: an existing poster is kept unless the
// administrator removed it, in which case the lookup may supply a new one.
func (s *Server) fillCollectionUpdate(ctx context.Context, existing store.Collection, u *store.CollectionUpdate) {
	c := store.Collection{Name: u.Name, Year: u.Year, Overview: u.Overview, Poster: u.Poster}
	if c.Poster == "" && !u.RemovePoster {
		c.Poster = existing.Poster
	}
	s.fillFromTMDB(ctx, &c)
	u.Year, u.Overview = c.Year, c.Overview
	if c.Poster != existing.Poster || u.RemovePoster {
		u.Poster = c.Poster
		u.RemovePoster = c.Poster == ""
	}
}

// fillFromTMDB fills a collection's empty fields from TMDB, when lookups
// are on. A failed lookup is logged and leaves c unchanged.
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

// savePoster stores an uploaded poster under a new random name and returns
// the name, or "" when p is nil.
func (s *Server) savePoster(p *posterUpload) (string, error) {
	if p == nil {
		return "", nil
	}
	f, err := p.file.Open()
	if err != nil {
		return "", err
	}
	defer f.Close()
	token, err := auth.NewToken()
	if err != nil {
		return "", err
	}
	name := "upload-" + token[:16] + p.ext
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
	lang, cues, err := decodeSubtitleUpload(r)
	if err != nil {
		return s.showMediaFormError(w, r, m, err)
	}
	// An upload has no source, so it is always inserted.
	_, _, err = s.Store.AddSubtitle(r.Context(), store.NewSubtitle{MediaID: m.ID, Language: lang, Label: subtitle.LanguageName(lang), Cues: cues})
	if err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/media/"+itoa(m.ID)+"?saved=1", http.StatusSeeOther)
	return nil
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

// createCollection handles POST /admin/collections, from the control panel.
func (s *Server) createCollection(w http.ResponseWriter, r *http.Request) error {
	const title = "Could not create the collection"
	in, err := decodeCollectionForm(r)
	var fe *formError
	if errors.As(err, &fe) {
		return &statusError{status: http.StatusBadRequest, title: title, msg: fe.msg}
	}
	if err != nil {
		return err
	}
	u := in.update
	c := store.Collection{Name: u.Name, Category: u.Category, Year: u.Year, Overview: u.Overview}
	if c.Poster, err = s.savePoster(in.poster); err != nil {
		return err
	}
	if in.lookup {
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
	s.render(w, r, http.StatusOK, editCollectionPage(newPage(r, "Edit "+c.Name), c, cats, s.Meta.Enabled(), ""))
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
	in, err := decodeCollectionForm(r)
	if err != nil {
		return s.showCollectionFormError(w, r, existing, in.update, err)
	}
	u := in.update
	if u.Poster, err = s.savePoster(in.poster); err != nil {
		return err
	}
	if in.lookup {
		s.fillCollectionUpdate(r.Context(), existing, &u)
	}
	err = s.Store.UpdateCollection(r.Context(), id, u)
	if errors.Is(err, store.ErrDuplicate) {
		return s.showCollectionFormError(w, r, existing, u, formErr("Another collection already has that name."))
	}
	if err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/collections/"+itoa(id), http.StatusSeeOther)
	return nil
}

// showCollectionFormError shows the edit page again, with what was typed,
// when err is a *formError, and returns any other error unchanged.
func (s *Server) showCollectionFormError(w http.ResponseWriter, r *http.Request, c store.Collection, typed store.CollectionUpdate, err error) error {
	var fe *formError
	if !errors.As(err, &fe) {
		return err
	}
	cats, err := s.Store.CategoryNames(r.Context())
	if err != nil {
		return err
	}
	c.Name, c.Category, c.Year, c.Overview = typed.Name, typed.Category, typed.Year, typed.Overview
	s.render(w, r, http.StatusBadRequest, editCollectionPage(newPage(r, "Edit collection"), c, cats, s.Meta.Enabled(), fe.msg))
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

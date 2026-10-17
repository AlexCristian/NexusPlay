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

// adminHandlers serve the control panel: publishing and editing media,
// collections and subtitle tracks, scans and the conversion queue.
type adminHandlers struct {
	responder
	store       adminStore
	meta        metadataLookup
	scanner     scanTrigger
	conversions Conversions
	posterDir   string
}

// adminStore is the part of the database the control panel edits.
type adminStore interface {
	mediaGetter
	PendingMedia(ctx context.Context) ([]store.Media, error)
	UpdateMedia(ctx context.Context, id int64, u store.MediaUpdate, publish bool) error
	Collections(ctx context.Context) ([]store.Collection, error)
	CategoryNames(ctx context.Context) ([]string, error)
	CollectionByID(ctx context.Context, id int64) (store.Collection, error)
	CollectionByName(ctx context.Context, name string) (store.Collection, error)
	CreateCollection(ctx context.Context, c store.Collection) (id int64, err error)
	UpdateCollection(ctx context.Context, id int64, u store.CollectionUpdate) error
	DeleteCollection(ctx context.Context, id int64) error
	Subtitles(ctx context.Context, mediaID int64) ([]store.Subtitle, error)
	SubtitleByID(ctx context.Context, id int64) (store.Subtitle, error)
	AddSubtitle(ctx context.Context, t store.NewSubtitle) (id int64, inserted bool, err error)
	UpdateSubtitleLabel(ctx context.Context, id int64, language, label string) error
	DeleteSubtitle(ctx context.Context, id int64) error
	ConversionFailures(ctx context.Context) ([]store.ConversionFailure, error)
	RetryConversion(ctx context.Context, id int64) error
}

// metadataLookup finds show details on TMDB (metadata.Client).
type metadataLookup interface {
	Enabled() bool
	Show(ctx context.Context, name, year string) (metadata.Info, error)
}

// scanTrigger starts a library scan (library.Scanner).
type scanTrigger interface {
	Trigger()
}

// mediaFromPath loads the media named by {id}. Administrators see
// unpublished media too, so there is no visibility check.
func (h *adminHandlers) mediaFromPath(r *http.Request) (store.Media, error) {
	id, err := pathID(r)
	if err != nil {
		return store.Media{}, err
	}
	return h.store.MediaByID(r.Context(), id)
}

// controlPanel handles GET /admin: media waiting to be published, the
// collections, and the conversion queue.
func (h *adminHandlers) controlPanel(w http.ResponseWriter, r *http.Request) error {
	ctx := r.Context()
	pending, err := h.store.PendingMedia(ctx)
	if err != nil {
		return err
	}
	colls, err := h.store.Collections(ctx)
	if err != nil {
		return err
	}
	cats, err := h.store.CategoryNames(ctx)
	if err != nil {
		return err
	}
	conv, err := h.conversionView(ctx)
	if err != nil {
		return err
	}
	h.render(w, r, http.StatusOK, controlPanel(newPage(r, "Control panel"), pending, colls, cats, h.meta.Enabled(), conv,
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

func (h *adminHandlers) conversionView(ctx context.Context) (ConversionView, error) {
	var v ConversionView
	var err error
	if v.Status, err = h.conversions.Status(ctx); err != nil {
		return v, err
	}
	v.Failures, err = h.store.ConversionFailures(ctx)
	return v, err
}

// conversionsPanel handles GET /admin/conversions, polled by the control
// panel while conversions run.
func (h *adminHandlers) conversionsPanel(w http.ResponseWriter, r *http.Request) error {
	v, err := h.conversionView(r.Context())
	if err != nil {
		return err
	}
	h.render(w, r, http.StatusOK, conversionsSection(viewer(r).CSRF, v))
	return nil
}

// retryConversion handles POST /admin/media/{id}/retry-conversion.
func (h *adminHandlers) retryConversion(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := h.store.RetryConversion(r.Context(), id); err != nil {
		return err
	}
	h.conversions.Wake()
	http.Redirect(w, r, "/admin?done=retry", http.StatusSeeOther)
	return nil
}

// scanNow handles POST /admin/scan.
func (h *adminHandlers) scanNow(w http.ResponseWriter, r *http.Request) error {
	h.scanner.Trigger()
	http.Redirect(w, r, "/admin?done=scan", http.StatusSeeOther)
	return nil
}

func (h *adminHandlers) mediaForm(ctx context.Context, m store.Media) (MediaForm, error) {
	f := MediaForm{Media: m, CollectionName: m.CollectionName, MetaEnabled: h.meta.Enabled()}
	var err error
	if f.Collections, err = h.store.Collections(ctx); err != nil {
		return f, err
	}
	if f.Categories, err = h.store.CategoryNames(ctx); err != nil {
		return f, err
	}
	if f.Subtitles, err = h.store.Subtitles(ctx, m.ID); err != nil {
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
func (h *adminHandlers) editMediaPage(w http.ResponseWriter, r *http.Request) error {
	m, err := h.mediaFromPath(r)
	if err != nil {
		return err
	}
	f, err := h.mediaForm(r.Context(), m)
	if err != nil {
		return err
	}
	f.Saved = r.URL.Query().Get("saved") == "1"
	h.render(w, r, http.StatusOK, editMediaPage(newPage(r, "Edit "+m.Title), f))
	return nil
}

// showMediaFormError shows the edit page again when err is a *formError,
// and returns any other error unchanged.
func (h *adminHandlers) showMediaFormError(w http.ResponseWriter, r *http.Request, m store.Media, err error) error {
	var fe *formError
	if !errors.As(err, &fe) {
		return err
	}
	f, ferr := h.mediaForm(r.Context(), m)
	if ferr != nil {
		return ferr
	}
	f.Error = fe.msg
	h.render(w, r, http.StatusBadRequest, editMediaPage(newPage(r, "Edit "+m.Title), f))
	return nil
}

// saveMedia handles POST /admin/media/{id}: the edit form, whose second
// button also publishes.
func (h *adminHandlers) saveMedia(w http.ResponseWriter, r *http.Request) error {
	m, err := h.mediaFromPath(r)
	if err != nil {
		return err
	}
	in, err := decodeMediaForm(r)
	if err != nil {
		return h.showMediaFormError(w, r, m, err)
	}
	if err := h.applyMediaInput(r.Context(), m.ID, in); err != nil {
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
func (h *adminHandlers) applyMediaInput(ctx context.Context, id int64, in mediaInput) error {
	u := in.update
	if in.collection != "" {
		c, err := h.findOrCreateCollection(ctx, in.collection, u.Category, u.Year)
		if err != nil {
			return err
		}
		u.CollectionID = c.ID
	}
	var err error
	if u.Poster, err = h.savePoster(in.poster); err != nil {
		return err
	}
	return h.store.UpdateMedia(ctx, id, u, in.publish)
}

// findOrCreateCollection returns the named collection, creating it (with
// TMDB details when available) if it does not exist yet.
func (h *adminHandlers) findOrCreateCollection(ctx context.Context, name, category, year string) (store.Collection, error) {
	c, err := h.store.CollectionByName(ctx, name)
	if err == nil || !errors.Is(err, store.ErrNotFound) {
		return c, err
	}
	if category == "" {
		category = "TV Shows"
	}
	// For an episode, the year guessed from "Show (2019)/..." is the show's year.
	c = store.Collection{Name: name, Category: category, Year: year}
	h.fillFromTMDB(ctx, &c)
	if c.ID, err = h.store.CreateCollection(ctx, c); errors.Is(err, store.ErrDuplicate) {
		return h.store.CollectionByName(ctx, name)
	}
	return c, err
}

// fillCollectionUpdate applies a TMDB lookup to an edit of existing. The
// lookup only fills empty fields: an existing poster is kept unless the
// administrator removed it, in which case the lookup may supply a new one.
func (h *adminHandlers) fillCollectionUpdate(ctx context.Context, existing store.Collection, u *store.CollectionUpdate) {
	c := store.Collection{Name: u.Name, Year: u.Year, Overview: u.Overview, Poster: u.Poster}
	if c.Poster == "" && !u.RemovePoster {
		c.Poster = existing.Poster
	}
	h.fillFromTMDB(ctx, &c)
	u.Year, u.Overview = c.Year, c.Overview
	if c.Poster != existing.Poster || u.RemovePoster {
		u.Poster = c.Poster
		u.RemovePoster = c.Poster == ""
	}
}

// fillFromTMDB fills a collection's empty fields from TMDB, when lookups
// are on. A failed lookup is logged and leaves c unchanged.
func (h *adminHandlers) fillFromTMDB(ctx context.Context, c *store.Collection) {
	if !h.meta.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	info, err := h.meta.Show(ctx, c.Name, "")
	if err != nil {
		if !errors.Is(err, metadata.ErrNoMatch) {
			h.log.Warn("collection lookup failed", "name", c.Name, "err", err)
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
func (h *adminHandlers) savePoster(p *posterUpload) (string, error) {
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
	out, err := os.OpenFile(filepath.Join(h.posterDir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
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
func (h *adminHandlers) uploadSubtitle(w http.ResponseWriter, r *http.Request) error {
	m, err := h.mediaFromPath(r)
	if err != nil {
		return err
	}
	lang, cues, err := decodeSubtitleUpload(r)
	if err != nil {
		return h.showMediaFormError(w, r, m, err)
	}
	// An upload has no source, so it is always inserted.
	_, _, err = h.store.AddSubtitle(r.Context(), store.NewSubtitle{MediaID: m.ID, Language: lang, Label: subtitle.LanguageName(lang), Cues: cues})
	if err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/media/"+itoa(m.ID)+"?saved=1", http.StatusSeeOther)
	return nil
}

// saveSubtitle handles POST /admin/subtitles/{id}: a track's language and label.
func (h *adminHandlers) saveSubtitle(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	t, err := h.store.SubtitleByID(r.Context(), id)
	if err != nil {
		return err
	}
	lang := strings.ToLower(strings.TrimSpace(r.FormValue("language")))
	label := strings.TrimSpace(r.FormValue("label"))
	if lang == "" || label == "" {
		return &statusError{status: http.StatusBadRequest, title: "Missing fields",
			msg: "A subtitle track needs a language code and a label."}
	}
	if err := h.store.UpdateSubtitleLabel(r.Context(), id, lang, label); err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/media/"+itoa(t.MediaID)+"?saved=1", http.StatusSeeOther)
	return nil
}

// deleteSubtitle handles POST /admin/subtitles/{id}/delete.
func (h *adminHandlers) deleteSubtitle(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	t, err := h.store.SubtitleByID(r.Context(), id)
	if err != nil {
		return err
	}
	if err := h.store.DeleteSubtitle(r.Context(), id); err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/media/"+itoa(t.MediaID)+"?saved=1", http.StatusSeeOther)
	return nil
}

// createCollection handles POST /admin/collections, from the control panel.
func (h *adminHandlers) createCollection(w http.ResponseWriter, r *http.Request) error {
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
	if c.Poster, err = h.savePoster(in.poster); err != nil {
		return err
	}
	if in.lookup {
		h.fillFromTMDB(r.Context(), &c)
	}
	id, err := h.store.CreateCollection(r.Context(), c)
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
func (h *adminHandlers) editCollectionPage(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	c, err := h.store.CollectionByID(r.Context(), id)
	if err != nil {
		return err
	}
	cats, err := h.store.CategoryNames(r.Context())
	if err != nil {
		return err
	}
	h.render(w, r, http.StatusOK, editCollectionPage(newPage(r, "Edit "+c.Name), c, cats, h.meta.Enabled(), ""))
	return nil
}

// saveCollection handles POST /admin/collections/{id}.
func (h *adminHandlers) saveCollection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	existing, err := h.store.CollectionByID(r.Context(), id)
	if err != nil {
		return err
	}
	in, err := decodeCollectionForm(r)
	if err != nil {
		return h.showCollectionFormError(w, r, existing, in.update, err)
	}
	u := in.update
	if u.Poster, err = h.savePoster(in.poster); err != nil {
		return err
	}
	if in.lookup {
		h.fillCollectionUpdate(r.Context(), existing, &u)
	}
	err = h.store.UpdateCollection(r.Context(), id, u)
	if errors.Is(err, store.ErrDuplicate) {
		return h.showCollectionFormError(w, r, existing, u, formErr("Another collection already has that name."))
	}
	if err != nil {
		return err
	}
	http.Redirect(w, r, "/admin/collections/"+itoa(id), http.StatusSeeOther)
	return nil
}

// showCollectionFormError shows the edit page again, with what was typed,
// when err is a *formError, and returns any other error unchanged.
func (h *adminHandlers) showCollectionFormError(w http.ResponseWriter, r *http.Request, c store.Collection, typed store.CollectionUpdate, err error) error {
	var fe *formError
	if !errors.As(err, &fe) {
		return err
	}
	cats, err := h.store.CategoryNames(r.Context())
	if err != nil {
		return err
	}
	c.Name, c.Category, c.Year, c.Overview = typed.Name, typed.Category, typed.Year, typed.Overview
	h.render(w, r, http.StatusBadRequest, editCollectionPage(newPage(r, "Edit collection"), c, cats, h.meta.Enabled(), fe.msg))
	return nil
}

// deleteCollection handles POST /admin/collections/{id}/delete. Its media
// stays in the library, outside any collection.
func (h *adminHandlers) deleteCollection(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := h.store.DeleteCollection(r.Context(), id); err != nil {
		return err
	}
	http.Redirect(w, r, "/admin?done=deleted", http.StatusSeeOther)
	return nil
}

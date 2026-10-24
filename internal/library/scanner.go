package library

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/media"
	"github.com/AlexCristian/NexusPlay/internal/metadata"
	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

// Store is the database as the scanner uses it; *store.Store implements it.
type Store interface {
	MediaPaths(ctx context.Context) (map[string]int64, error)
	InsertScannedMedia(ctx context.Context, m store.ScannedMedia) (id int64, inserted bool, err error)
	SetScannedMetadata(ctx context.Context, id int64, md store.ScannedMetadata) error
	ReplaceMetadata(ctx context.Context, id int64, md store.ScannedMetadata) error
	MarkMetadataChecked(ctx context.Context, id int64) error
	MediaNeedingMetadata(ctx context.Context, checkedBefore time.Time, limit int) ([]store.Media, error)
	MediaByID(ctx context.Context, id int64) (store.Media, error)
	DeleteMedia(ctx context.Context, id int64) error
	SubtitleSources(ctx context.Context) (map[string]bool, error)
	AddSubtitle(ctx context.Context, t store.NewSubtitle) (id int64, inserted bool, err error)
	H264MissingPixFmt(ctx context.Context) (map[string]int64, error)
	SetPixFmt(ctx context.Context, id int64, pixFmt string) error
}

// Prober reads a media file's streams; ffmpeg.Tools is the real one.
type Prober interface {
	Probe(ctx context.Context, file string) (ffmpeg.Probe, error)
}

// Metadata looks titles up online; *metadata.Client is the real one. Its
// ErrDisabled and ErrNoMatch are expected and not logged.
type Metadata interface {
	Movie(ctx context.Context, title, year string) (metadata.Info, error)
	Show(ctx context.Context, name, year string) (metadata.Info, error)
	Episode(ctx context.Context, showID, season, episode int) (metadata.Info, error)
	Season(ctx context.Context, showID, season int) ([]metadata.Episode, error)
}

// Deps are the scanner's collaborators.
type Deps struct {
	Store  Store
	Prober Prober
	Meta   Metadata // nil turns lookups off
	Log    *slog.Logger
}

// Config tunes the scanner. Zero durations use the defaults.
type Config struct {
	MediaDir string
	Interval time.Duration // full rescan period (default 5 min)
	Settle   time.Duration // how long a new file must go unmodified before import (default 30 s)
}

// Scanner keeps the database in step with the media folder. It rescans on a
// timer and shortly after any filesystem change. Build it with NewScanner;
// it is safe for concurrent use.
//
// Locking: scan serialises whole scans, so only one runs at a time.
// reconcile is held only while the scan changes the database (and re-checks
// the disk), never while it probes files or calls TMDB. The converter holds
// reconcile too, through Exclusive, while it swaps a file and its row, so a
// scan never sees a file renamed but not yet updated in the database. Neither
// lock is reentrant: never call Scan from inside Exclusive.
type Scanner struct {
	deps    Deps
	cfg     Config
	trigger chan struct{}
	now     func() time.Time

	scan      sync.Mutex
	reconcile sync.Mutex
	onAdded   func() // set before Run; called after a scan that added media
}

// Result summarizes one scan.
type Result struct {
	Added     int
	Removed   int
	Subtitles int // sidecar subtitle files imported
	Waiting   int // new files skipped because they are still being written
}

// NewScanner returns a scanner. Call Run to keep it scanning.
func NewScanner(d Deps, c Config) *Scanner {
	if c.Interval <= 0 {
		c.Interval = 5 * time.Minute
	}
	if c.Settle <= 0 {
		c.Settle = 30 * time.Second
	}
	return &Scanner{deps: d, cfg: c, trigger: make(chan struct{}, 1), now: time.Now}
}

// OnAdded registers fn to be called after a scan that added media (the
// converter uses it to look at new files at once). Call it before Run.
func (s *Scanner) OnAdded(fn func()) { s.onAdded = fn }

// Trigger asks Run for a scan as soon as possible. Triggers that arrive while
// one is pending coalesce into a single scan.
func (s *Scanner) Trigger() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// Exclusive runs fn while no scan is changing the database. It waits at most
// for one reconcile step, which only touches the database and stats files.
func (s *Scanner) Exclusive(fn func() error) error {
	s.reconcile.Lock()
	defer s.reconcile.Unlock()
	return fn()
}

// Run scans immediately and then whenever the timer fires, Trigger is
// called, or the media folder changes (debounced by 5 s), until ctx ends.
func (s *Scanner) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		s.deps.Log.Warn("filesystem watching unavailable; relying on periodic scans", "err", err)
	} else {
		defer watcher.Close()
	}
	var events <-chan fsnotify.Event
	var errs <-chan error
	if watcher != nil {
		events, errs = watcher.Events, watcher.Errors
	}

	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	soon := time.NewTimer(0) // fires at once: the initial scan
	defer soon.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-soon.C:
		case <-s.trigger:
		case ev, ok := <-events:
			if !ok {
				events = nil
			} else {
				s.onEvent(ev, soon)
			}
			continue
		case err, ok := <-errs:
			if !ok {
				errs = nil
			} else {
				s.onWatchError(err, soon)
			}
			continue
		}
		s.scanOnce(ctx, watcher, soon)
	}
}

// onEvent schedules a scan a few seconds after a change in the media
// folder, so a burst of changes (a file being copied in) causes one scan.
// Permission changes alone are ignored.
func (s *Scanner) onEvent(ev fsnotify.Event, soon *time.Timer) {
	if !ev.Has(fsnotify.Chmod) || ev.Has(fsnotify.Write) {
		soon.Reset(5 * time.Second)
	}
}

// onWatchError handles a watcher error. An overflow means changes were
// lost, and only a full scan can catch up.
func (s *Scanner) onWatchError(err error, soon *time.Timer) {
	if errors.Is(err, fsnotify.ErrEventOverflow) {
		s.deps.Log.Info("too many filesystem changes at once; rescanning")
		soon.Reset(time.Second)
		return
	}
	s.deps.Log.Warn("filesystem watcher", "err", err)
}

// scanOnce runs one scan for Run and acts on its result.
func (s *Scanner) scanOnce(ctx context.Context, watcher *fsnotify.Watcher, soon *time.Timer) {
	res, dirs, err := s.scanAll(ctx)
	if err != nil {
		if ctx.Err() == nil {
			s.deps.Log.Error("library scan failed", "err", err)
		}
		return
	}
	if watcher != nil {
		for _, d := range dirs {
			if err := watcher.Add(d); err != nil {
				// Usually the inotify watch limit; periodic scans still work.
				s.deps.Log.Debug("not watching folder for changes", "path", d, "err", err)
			}
		}
	}
	if res.Added+res.Removed+res.Subtitles > 0 {
		s.deps.Log.Info("library scan", "added", res.Added, "removed", res.Removed, "subtitles", res.Subtitles)
	}
	if res.Waiting > 0 {
		soon.Reset(s.cfg.Settle + 5*time.Second) // come back once they have settled
	}
	if res.Added > 0 && s.onAdded != nil {
		s.onAdded()
	}
}

// Scan brings the database in step with the media folder once: new files are
// probed and added as unpublished media, sidecar subtitles are imported, and
// media whose files are gone are removed.
func (s *Scanner) Scan(ctx context.Context) (Result, error) {
	res, _, err := s.scanAll(ctx)
	return res, err
}

func (s *Scanner) scanAll(ctx context.Context) (Result, []string, error) {
	s.scan.Lock()
	defer s.scan.Unlock()
	var res Result

	tree, err := s.walk()
	if err != nil {
		return res, nil, err
	}
	known, err := s.deps.Store.MediaPaths(ctx)
	if err != nil {
		return res, nil, err
	}
	probed, waiting := s.probeNew(ctx, tree.videos, known)
	res.Waiting = waiting

	s.reconcile.Lock()
	added, removed, known, err := s.applyChanges(ctx, tree, probed)
	s.reconcile.Unlock()
	if err != nil {
		return res, nil, err
	}
	res.Added, res.Removed = len(added), removed

	for _, a := range added {
		s.lookupMetadata(ctx, a.id, a.guess)
	}
	s.retryMetadata(ctx)
	res.Subtitles = s.importAllSidecars(ctx, tree, known)
	s.backfillPixFmt(ctx, tree.videos)
	return res, tree.dirs, nil
}

// tree is what one walk of the media folder found.
type tree struct {
	videos     []found
	subsByDir  map[string][]string // sidecar subtitle files by folder (relative paths)
	dirs       []string            // absolute folder paths, for the change watcher
	unreadable []string            // relative paths that could not be read
}

type found struct {
	rel  string // relative, slash-separated
	abs  string
	info fs.FileInfo
}

// walk lists the media folder. Hidden files and folders are skipped. Paths
// that cannot be read are recorded rather than treated as gone: a folder that
// fails to list on a flaky network share must not look deleted.
func (s *Scanner) walk() (tree, error) {
	t := tree{subsByDir: map[string][]string{}}
	unreadable := func(p string, err error) {
		s.deps.Log.Warn("skipping unreadable path; its library entries are kept", "path", p, "err", err)
		if rel, relErr := filepath.Rel(s.cfg.MediaDir, p); relErr == nil {
			t.unreadable = append(t.unreadable, filepath.ToSlash(rel))
		}
	}
	err := filepath.WalkDir(s.cfg.MediaDir, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil && p == s.cfg.MediaDir:
			return err
		case err != nil:
			unreadable(p, err)
			return nil
		case strings.HasPrefix(d.Name(), ".") && p != s.cfg.MediaDir:
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case d.IsDir():
			t.dirs = append(t.dirs, p)
			return nil
		}
		rel, err := filepath.Rel(s.cfg.MediaDir, p)
		if err != nil {
			unreadable(p, err)
			return nil
		}
		rel = filepath.ToSlash(rel)
		switch ext := strings.ToLower(path.Ext(rel)); {
		case media.IsVideo(rel):
			info, err := d.Info()
			if err != nil {
				unreadable(p, err)
				return nil
			}
			t.videos = append(t.videos, found{rel: rel, abs: p, info: info})
		case ext == ".srt" || ext == ".vtt":
			t.subsByDir[path.Dir(rel)] = append(t.subsByDir[path.Dir(rel)], rel)
		}
		return nil
	})
	return t, err
}

// probedFile is a new file ready to be added.
type probedFile struct {
	found
	media store.ScannedMedia
	guess Guess
}

// probeNew probes the files that are not in the library yet and have
// settled: modified at least Settle ago, so a file still being copied in is
// not read half-written. Files that fail to probe are logged and skipped
// (they are retried next scan). It returns how many files are still settling.
func (s *Scanner) probeNew(ctx context.Context, videos []found, known map[string]int64) (out []probedFile, waiting int) {
	for _, v := range videos {
		if _, ok := known[v.rel]; ok {
			continue
		}
		if s.now().Sub(v.info.ModTime()) < s.cfg.Settle {
			waiting++
			continue
		}
		p, err := s.probe(ctx, v)
		if err != nil {
			if ctx.Err() != nil {
				return out, waiting
			}
			s.deps.Log.Warn("could not add media", "path", v.rel, "err", err)
			continue
		}
		out = append(out, p)
	}
	return out, waiting
}

func (s *Scanner) probe(ctx context.Context, v found) (probedFile, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	probe, err := s.deps.Prober.Probe(ctx, v.abs)
	if err != nil {
		return probedFile{}, err
	}
	video, ok := probe.Video()
	if !ok {
		return probedFile{}, ffmpeg.ErrNoVideo
	}
	g := ParsePath(v.rel)
	return probedFile{found: v, guess: g, media: store.ScannedMedia{
		Path: v.rel, Title: g.Title, Year: g.Year, Season: g.Season, Episode: g.Episode,
		Duration: probe.Duration, Container: probe.Container,
		VideoCodec: video.Codec, AudioCodec: probe.AudioCodec(), PixFmt: video.PixFmt, Size: v.info.Size(),
	}}, nil
}

type addedMedia struct {
	id    int64
	guess Guess
}

// applyChanges inserts new files and deletes rows whose files are gone. It
// runs under the reconcile lock, after the slow walk and probes, so it
// re-reads the database and re-checks each file on disk instead of trusting
// the walk: the converter may have swapped a file in the meantime. It
// returns the added media, the number removed, and the updated path→ID map.
func (s *Scanner) applyChanges(ctx context.Context, t tree, probed []probedFile) ([]addedMedia, int, map[string]int64, error) {
	known, err := s.deps.Store.MediaPaths(ctx)
	if err != nil {
		return nil, 0, nil, err
	}
	var added []addedMedia
	for _, p := range probed {
		if _, ok := known[p.rel]; ok || !exists(p.abs) {
			continue
		}
		id, inserted, err := s.deps.Store.InsertScannedMedia(ctx, p.media)
		if err != nil {
			return added, 0, known, err
		}
		if inserted {
			known[p.rel] = id
			added = append(added, addedMedia{id: id, guess: p.guess})
		}
	}

	// If the folder has no videos while the database has rows, the drive is
	// most likely unmounted: keep everything rather than wipe the library.
	if len(t.videos) == 0 && len(known) > 0 {
		s.deps.Log.Warn("media folder has no videos; skipping removals in case it is unmounted", "dir", s.cfg.MediaDir)
		return added, 0, known, nil
	}
	onDisk := make(map[string]bool, len(t.videos))
	for _, v := range t.videos {
		onDisk[v.rel] = true
	}
	removed := 0
	for _, rel := range missingMedia(known, onDisk, t.unreadable) {
		if exists(filepath.Join(s.cfg.MediaDir, filepath.FromSlash(rel))) {
			continue // appeared after the walk (a converted file)
		}
		if err := s.deps.Store.DeleteMedia(ctx, known[rel]); err != nil {
			return added, removed, known, err
		}
		delete(known, rel)
		removed++
	}
	return added, removed, known, nil
}

func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// missingMedia returns the paths of known media whose files are gone: not
// seen on disk, and not inside a path that could not be read (whose files
// may well still exist).
func missingMedia(known map[string]int64, onDisk map[string]bool, unreadable []string) []string {
	var gone []string
	for p := range known {
		if !onDisk[p] && !underAny(p, unreadable) {
			gone = append(gone, p)
		}
	}
	return gone
}

// underAny reports whether p is one of roots or inside one of them.
func underAny(p string, roots []string) bool {
	for _, r := range roots {
		if p == r || strings.HasPrefix(p, r+"/") {
			return true
		}
	}
	return false
}

// Metadata retries: unpublished media still missing a poster or a
// description is looked up again, at most metadataRetryBatch items per scan
// and each at most once per metadataRetryAfter. That covers files scanned
// before an API key was set and lookups that failed for a passing reason.
const (
	metadataRetryAfter = time.Hour
	metadataRetryBatch = 25
)

// lookupMetadata enriches an unpublished item from TMDB. It is optional:
// failures are logged and the file-name guess stays.
func (s *Scanner) lookupMetadata(ctx context.Context, id int64, g Guess) {
	if s.deps.Meta == nil {
		return
	}
	md, err := s.metadataFor(ctx, g)
	switch {
	case errors.Is(err, metadata.ErrDisabled):
		return // not a lookup: try again once a key is set
	case errors.Is(err, metadata.ErrNoMatch):
		err = s.deps.Store.MarkMetadataChecked(ctx, id)
	case err != nil:
		s.deps.Log.Warn("metadata lookup failed; it is retried later", "title", g.Title, "err", err)
		err = s.deps.Store.MarkMetadataChecked(ctx, id)
	default:
		err = s.deps.Store.SetScannedMetadata(ctx, id, md)
	}
	if err != nil {
		s.deps.Log.Warn("saving metadata failed", "media", id, "err", err)
	}
}

// retryMetadata looks up again unpublished media that is still missing a
// poster or description (see metadataRetryAfter).
func (s *Scanner) retryMetadata(ctx context.Context) {
	if s.deps.Meta == nil {
		return
	}
	list, err := s.deps.Store.MediaNeedingMetadata(ctx, s.now().Add(-metadataRetryAfter), metadataRetryBatch)
	if err != nil {
		s.deps.Log.Warn("listing media without metadata", "err", err)
		return
	}
	for _, m := range list {
		if ctx.Err() != nil {
			return
		}
		s.lookupMetadata(ctx, m.ID, guessForMedia(m))
	}
}

// RefreshMetadata looks a media item up on TMDB again and saves what it
// finds, published or not: the control panel's "Refresh metadata". It uses
// the item's current collection, season and episode, which an
// administrator may have corrected. It returns metadata.ErrNoMatch when
// TMDB has nothing and metadata.ErrDisabled without an API key.
func (s *Scanner) RefreshMetadata(ctx context.Context, id int64) error {
	if s.deps.Meta == nil {
		return metadata.ErrDisabled
	}
	m, err := s.deps.Store.MediaByID(ctx, id)
	if err != nil {
		return err
	}
	md, err := s.metadataFor(ctx, guessForMedia(m))
	if err != nil {
		return err
	}
	return s.deps.Store.ReplaceMetadata(ctx, id, md)
}

// guessForMedia is what to look a stored item up by: the guess from its
// file name, with the collection, season and episode from the database.
func guessForMedia(m store.Media) Guess {
	g := ParsePath(m.Path)
	if !g.IsEpisode() && (m.CollectionName == "" || (m.Season == 0 && m.Episode == 0)) {
		return g // a film, possibly in a collection of films
	}
	if m.CollectionName != "" {
		if !strings.EqualFold(g.Show, m.CollectionName) {
			g.Year = "" // the year was a film's or another show's; TMDB knows the show's
		}
		g.Show = m.CollectionName
	}
	if m.Season > 0 || m.Episode > 0 {
		g.Season, g.Episode = m.Season, m.Episode
	}
	return g
}

// metadataFor looks g up, within 30 seconds.
func (s *Scanner) metadataFor(ctx context.Context, g Guess) (store.ScannedMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	info, err := s.findMetadata(ctx, g)
	if err != nil {
		return store.ScannedMetadata{}, err
	}
	return store.ScannedMetadata{Title: info.Title, Year: info.Year, Overview: info.Overview, Poster: info.Poster}, nil
}

// findMetadata looks up a film, or an episode with its show's poster.
func (s *Scanner) findMetadata(ctx context.Context, g Guess) (metadata.Info, error) {
	if !g.IsEpisode() {
		return s.deps.Meta.Movie(ctx, g.Title, g.Year)
	}
	show, err := s.deps.Meta.Show(ctx, g.Show, g.Year)
	if err != nil {
		return metadata.Info{}, err
	}
	ep, err := s.findEpisode(ctx, show.ID, g)
	if err != nil {
		return metadata.Info{}, err
	}
	ep.Poster = show.Poster
	return ep, nil
}

// findEpisode finds g's episode of a show: by the title in the file name
// when there is one, since TMDB's episode numbers can differ from the
// file's (see match.go), and otherwise by number.
func (s *Scanner) findEpisode(ctx context.Context, showID int, g Guess) (metadata.Info, error) {
	title := fileEpisodeTitle(g)
	if title == "" {
		return s.deps.Meta.Episode(ctx, showID, g.Season, g.Episode)
	}
	eps, err := s.deps.Meta.Season(ctx, showID, g.Season)
	if err != nil {
		return metadata.Info{}, err
	}
	if e, ok := episodeByTitle(eps, title, g.Episode); ok {
		if e.Number != g.Episode {
			s.deps.Log.Debug("episode matched by title", "show", g.Show, "season", g.Season,
				"file_episode", g.Episode, "tmdb_episode", e.Number, "title", e.Title)
		}
		return e.Info, nil
	}
	if e, ok := episodeByNumber(eps, g.Episode); ok {
		return e.Info, nil
	}
	return metadata.Info{}, metadata.ErrNoMatch
}

// importAllSidecars imports subtitle files next to every known video.
func (s *Scanner) importAllSidecars(ctx context.Context, t tree, known map[string]int64) int {
	imported, err := s.deps.Store.SubtitleSources(ctx)
	if err != nil {
		s.deps.Log.Warn("could not list imported subtitles", "err", err)
		return 0
	}
	n := 0
	for _, v := range t.videos {
		if id, ok := known[v.rel]; ok {
			n += s.importSidecars(ctx, id, v.rel, t.subsByDir[path.Dir(v.rel)], imported)
		}
	}
	return n
}

// importSidecars imports subtitle files named after a video, such as
// "Movie.srt" or "Movie.en.vtt" next to "Movie.mp4". The converter writes
// extracted subtitles with the same naming, so they come in here too.
func (s *Scanner) importSidecars(ctx context.Context, mediaID int64, videoRel string, candidates []string, imported map[string]bool) int {
	n := 0
	for _, sub := range candidates {
		if imported[sub] {
			continue
		}
		lang, ok := SidecarLanguage(videoRel, sub)
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join(s.cfg.MediaDir, filepath.FromSlash(sub)))
		if err != nil {
			s.deps.Log.Warn("could not read subtitle", "path", sub, "err", err)
			continue
		}
		cues, err := subtitle.Parse(data)
		if err != nil {
			s.deps.Log.Warn("could not parse subtitle", "path", sub, "err", err)
			imported[sub] = true // do not retry a broken file on every scan of this run
			continue
		}
		_, inserted, err := s.deps.Store.AddSubtitle(ctx, store.NewSubtitle{
			MediaID: mediaID, Language: lang, Label: subtitle.LanguageName(lang), Source: sub, Cues: cues,
		})
		if err != nil {
			s.deps.Log.Warn("could not save subtitle", "path", sub, "err", err)
			continue
		}
		imported[sub] = true
		if inserted {
			n++
		}
	}
	return n
}

// SidecarLanguage reports whether subRel is a sidecar subtitle of the video
// at videoRel and, if so, its language: "Movie.srt" is "und",
// "Movie.en.srt" is "en" and "Movie.forced.en.srt" is "en".
func SidecarLanguage(videoRel, subRel string) (lang string, ok bool) {
	base := strings.TrimSuffix(path.Base(videoRel), path.Ext(videoRel))
	name := strings.TrimSuffix(path.Base(subRel), path.Ext(subRel))
	if path.Dir(videoRel) != path.Dir(subRel) || (name != base && !strings.HasPrefix(name, base+".")) {
		return "", false
	}
	lang = strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(name, base), "."))
	if i := strings.LastIndexByte(lang, '.'); i >= 0 {
		lang = lang[i+1:]
	}
	if lang == "" {
		lang = "und"
	}
	return lang, true
}

// backfillLimit caps how many files one scan re-probes, so the first scan
// after upgrading a large library stays short; later scans do the rest.
const backfillLimit = 50

// backfillPixFmt records the pixel format of H.264 files added before it was
// stored. Without it, a 10-bit H.264 file (which no browser plays) would be
// served as is and never converted.
func (s *Scanner) backfillPixFmt(ctx context.Context, videos []found) {
	missing, err := s.deps.Store.H264MissingPixFmt(ctx)
	if err != nil {
		s.deps.Log.Warn("could not list media to re-probe", "err", err)
		return
	}
	done := 0
	for _, v := range videos {
		id, ok := missing[v.rel]
		if !ok || done == backfillLimit {
			continue
		}
		done++
		pctx, cancel := context.WithTimeout(ctx, time.Minute)
		probe, err := s.deps.Prober.Probe(pctx, v.abs)
		cancel()
		if err != nil {
			s.deps.Log.Warn("could not re-probe media", "path", v.rel, "err", err)
			continue
		}
		if video, ok := probe.Video(); ok && video.PixFmt != "" {
			if err := s.deps.Store.SetPixFmt(ctx, id, video.PixFmt); err != nil {
				s.deps.Log.Warn("could not record pixel format", "path", v.rel, "err", err)
			}
		}
	}
}

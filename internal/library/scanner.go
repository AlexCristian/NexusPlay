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

// settleTime is how long a file must go unmodified before it is imported, so
// a file still being copied into the library is not probed half-written.
const settleTime = 30 * time.Second

// Prober reads a media file's streams; ffmpeg.Tools is the real one.
type Prober interface {
	Probe(ctx context.Context, file string) (ffmpeg.Probe, error)
}

// Scanner keeps the database in step with the media folder. It rescans on a
// timer and shortly after any filesystem change.
type Scanner struct {
	Store    *store.Store
	Prober   Prober
	Meta     *metadata.Client
	MediaDir string
	Interval time.Duration
	Log      *slog.Logger
	// OnAdded, if set, is called after a scan that found new media.
	OnAdded func()

	trigger chan struct{}
	scanMu  sync.Mutex
	watcher *fsnotify.Watcher
	now     func() time.Time
}

// Result summarizes one scan.
type Result struct {
	Added    int
	Removed  int
	Subtitle int
	Waiting  int // files skipped because they are still being written
}

// Trigger asks the running scanner for a scan as soon as possible.
func (s *Scanner) Trigger() {
	select {
	case s.trigger <- struct{}{}:
	default:
	}
}

// Run scans immediately and then keeps scanning until ctx ends.
func (s *Scanner) Run(ctx context.Context) {
	s.init()
	w, err := fsnotify.NewWatcher()
	if err != nil {
		s.Log.Warn("filesystem watching unavailable; relying on periodic scans", "err", err)
	} else {
		s.watcher = w
		defer w.Close()
	}
	var events <-chan fsnotify.Event
	var errs <-chan error
	if s.watcher != nil {
		events, errs = s.watcher.Events, s.watcher.Errors
	}

	ticker := time.NewTicker(s.Interval)
	defer ticker.Stop()
	debounce := time.NewTimer(0) // fires at once: the initial scan
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.runScan(ctx, debounce)
		case <-debounce.C:
			s.runScan(ctx, debounce)
		case <-s.trigger:
			s.runScan(ctx, debounce)
		case ev, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if ev.Has(fsnotify.Chmod) && !ev.Has(fsnotify.Write) {
				continue
			}
			debounce.Reset(5 * time.Second)
		case err, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			s.Log.Warn("filesystem watcher", "err", err)
		}
	}
}

func (s *Scanner) init() {
	if s.trigger == nil {
		s.trigger = make(chan struct{}, 1)
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.Log == nil {
		s.Log = slog.Default()
	}
	if s.Interval <= 0 {
		s.Interval = 5 * time.Minute
	}
}

func (s *Scanner) runScan(ctx context.Context, debounce *time.Timer) {
	res, err := s.Scan(ctx)
	if err != nil {
		s.Log.Error("library scan failed", "err", err)
		return
	}
	if res.Added+res.Removed+res.Subtitle > 0 {
		s.Log.Info("library scan", "added", res.Added, "removed", res.Removed, "subtitles", res.Subtitle)
	}
	if res.Waiting > 0 {
		debounce.Reset(settleTime + 5*time.Second)
	}
	if res.Added > 0 && s.OnAdded != nil {
		s.OnAdded()
	}
}

// Exclusive runs fn while no scan is in progress. The converter uses it to
// swap a file and its database row without a scan seeing half the change.
func (s *Scanner) Exclusive(fn func() error) error {
	s.scanMu.Lock()
	defer s.scanMu.Unlock()
	return fn()
}

type found struct {
	rel  string
	abs  string
	info fs.FileInfo
}

// Scan walks the media folder once: new files are probed and added as
// unpublished media, sidecar subtitles are imported, and media whose files are
// gone are removed.
func (s *Scanner) Scan(ctx context.Context) (Result, error) {
	s.init()
	s.scanMu.Lock()
	defer s.scanMu.Unlock()

	var res Result
	known, err := s.Store.MediaPaths(ctx)
	if err != nil {
		return res, err
	}
	importedSubs, err := s.Store.SubtitleSources(ctx)
	if err != nil {
		return res, err
	}

	var videos []found
	subsByDir := map[string][]string{}
	// unreadable lists paths (relative, slash-separated) that could not be
	// read. Their rows are kept: a folder that fails to list on a flaky
	// network share must not look like a folder whose files were deleted.
	var unreadable []string
	markUnreadable := func(p string, err error) {
		s.Log.Warn("skipping unreadable path; its library entries are kept", "path", p, "err", err)
		if rel, relErr := filepath.Rel(s.MediaDir, p); relErr == nil {
			unreadable = append(unreadable, filepath.ToSlash(rel))
		}
	}
	walkErr := filepath.WalkDir(s.MediaDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if p == s.MediaDir {
				return err
			}
			markUnreadable(p, err)
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") && p != s.MediaDir {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			if s.watcher != nil {
				if err := s.watcher.Add(p); err != nil {
					// Usually the inotify watch limit; periodic scans still work.
					s.Log.Debug("not watching folder for changes", "path", p, "err", err)
				}
			}
			return nil
		}
		rel, err := filepath.Rel(s.MediaDir, p)
		if err != nil {
			markUnreadable(p, err)
			return nil
		}
		rel = filepath.ToSlash(rel)
		ext := strings.ToLower(path.Ext(rel))
		switch {
		case media.IsVideo(rel):
			info, err := d.Info()
			if err != nil {
				markUnreadable(p, err)
				return nil
			}
			videos = append(videos, found{rel: rel, abs: p, info: info})
		case ext == ".srt" || ext == ".vtt":
			subsByDir[path.Dir(rel)] = append(subsByDir[path.Dir(rel)], rel)
		}
		return nil
	})
	if walkErr != nil {
		return res, walkErr
	}

	onDisk := make(map[string]bool, len(videos))
	for _, v := range videos {
		onDisk[v.rel] = true
		id, isKnown := known[v.rel]
		if !isKnown {
			if s.now().Sub(v.info.ModTime()) < settleTime {
				res.Waiting++
				continue
			}
			newID, err := s.addMedia(ctx, v)
			if err != nil {
				s.Log.Warn("could not add media", "path", v.rel, "err", err)
				continue
			}
			if newID == 0 {
				continue
			}
			id = newID
			res.Added++
		}
		res.Subtitle += s.importSidecars(ctx, id, v.rel, subsByDir[path.Dir(v.rel)], importedSubs)
	}

	// If the folder looks empty while the database is not, the drive is most
	// likely unmounted: keep everything rather than wipe the library.
	if len(videos) == 0 && len(known) > 0 {
		s.Log.Warn("media folder has no videos; skipping removals in case it is unmounted", "dir", s.MediaDir)
		return res, nil
	}
	s.backfillPixFmt(ctx, videos)

	for _, id := range missingMedia(known, onDisk, unreadable) {
		if err := s.Store.DeleteMedia(ctx, id); err != nil {
			return res, err
		}
		res.Removed++
	}
	return res, nil
}

// backfillLimit caps how many files one scan re-probes, so the first scan
// after upgrading a large library does not hold the scan lock for long; the
// rest are picked up by later scans.
const backfillLimit = 50

// backfillPixFmt records the pixel format of H.264 files added before it was
// stored. Without it, a 10-bit H.264 file (which no browser plays) would be
// served as is and never converted.
func (s *Scanner) backfillPixFmt(ctx context.Context, videos []found) {
	missing, err := s.Store.H264MissingPixFmt(ctx)
	if err != nil || len(missing) == 0 {
		if err != nil {
			s.Log.Warn("could not list media to re-probe", "err", err)
		}
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
		probe, err := s.Prober.Probe(pctx, v.abs)
		cancel()
		if err != nil {
			s.Log.Warn("could not re-probe media", "path", v.rel, "err", err)
			continue
		}
		if video, ok := probe.Video(); ok && video.PixFmt != "" {
			if err := s.Store.SetPixFmt(ctx, id, video.PixFmt); err != nil {
				s.Log.Warn("could not record pixel format", "path", v.rel, "err", err)
			}
		}
	}
}

// missingMedia returns the IDs of known media whose files are gone: not seen
// on disk, and not inside a path that could not be read (whose files may well
// still exist).
func missingMedia(known map[string]int64, onDisk map[string]bool, unreadable []string) []int64 {
	var ids []int64
	for p, id := range known {
		if onDisk[p] || underAny(p, unreadable) {
			continue
		}
		ids = append(ids, id)
	}
	return ids
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

func (s *Scanner) addMedia(ctx context.Context, v found) (int64, error) {
	pctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	probe, err := s.Prober.Probe(pctx, v.abs)
	if err != nil {
		return 0, err
	}
	video, ok := probe.Video()
	if !ok {
		return 0, ffmpeg.ErrNoVideo
	}
	g := ParsePath(v.rel)
	id, err := s.Store.InsertScannedMedia(ctx, store.ScannedMedia{
		Path: v.rel, Title: g.Title, Year: g.Year, Season: g.Season, Episode: g.Episode,
		Duration: probe.Duration, Container: probe.Container,
		VideoCodec: video.Codec, AudioCodec: probe.AudioCodec(), PixFmt: video.PixFmt, Size: v.info.Size(),
	})
	if err != nil || id == 0 {
		return id, err
	}
	s.lookupMetadata(ctx, id, g)
	return id, nil
}

// lookupMetadata enriches a new item from TMDB. Failures are logged and ignored.
func (s *Scanner) lookupMetadata(ctx context.Context, id int64, g Guess) {
	if s.Meta == nil || !s.Meta.Enabled() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var info metadata.Info
	var err error
	if g.IsEpisode() {
		var show metadata.Info
		show, err = s.Meta.Show(ctx, g.Show, g.Year)
		if err == nil {
			info, err = s.Meta.Episode(ctx, show.ID, g.Season, g.Episode)
			info.Poster = show.Poster
		}
	} else {
		info, err = s.Meta.Movie(ctx, g.Title, g.Year)
	}
	if err != nil {
		if !errors.Is(err, metadata.ErrNoMatch) {
			s.Log.Warn("metadata lookup failed", "title", g.Title, "err", err)
		}
		return
	}
	if err := s.Store.SetScannedMetadata(ctx, id, info.Title, info.Year, info.Overview, info.Poster); err != nil {
		s.Log.Warn("saving metadata failed", "err", err)
	}
}

// importSidecars imports subtitle files named after a video, such as
// "Movie.srt" or "Movie.en.vtt" next to "Movie.mp4".
func (s *Scanner) importSidecars(ctx context.Context, mediaID int64, videoRel string, candidates []string, imported map[string]bool) int {
	base := strings.TrimSuffix(path.Base(videoRel), path.Ext(videoRel))
	n := 0
	for _, sub := range candidates {
		if imported[sub] {
			continue
		}
		name := strings.TrimSuffix(path.Base(sub), path.Ext(sub))
		if name != base && !strings.HasPrefix(name, base+".") {
			continue
		}
		lang := strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(name, base), "."))
		if i := strings.LastIndexByte(lang, '.'); i >= 0 {
			lang = lang[i+1:] // "Movie.forced.en.srt" -> "en"
		}
		if lang == "" {
			lang = "und"
		}
		data, err := os.ReadFile(filepath.Join(s.MediaDir, filepath.FromSlash(sub)))
		if err != nil {
			s.Log.Warn("could not read subtitle", "path", sub, "err", err)
			continue
		}
		cues, err := subtitle.Parse(data)
		if err != nil {
			s.Log.Warn("could not parse subtitle", "path", sub, "err", err)
			imported[sub] = true // do not retry a broken file on every scan
			continue
		}
		_, inserted, err := s.Store.AddSubtitle(ctx, store.NewSubtitle{
			MediaID: mediaID, Language: lang, Label: subtitle.LanguageName(lang), Source: sub, Cues: cues,
		})
		if err != nil {
			s.Log.Warn("could not save subtitle", "path", sub, "err", err)
			continue
		}
		imported[sub] = true
		if inserted {
			n++
		}
	}
	return n
}

package library

import (
	"context"
	"errors"
	"io/fs"
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
	config  Config
	trigger chan struct{}
	now     func() time.Time

	scan      sync.Mutex
	reconcile sync.Mutex
	onAdded   func() // set before Run; called after a scan that added media
}

// NewScanner returns a scanner. Call Run to keep it scanning.
func NewScanner(dependencies Deps, config Config) *Scanner {
	if config.Interval <= 0 {
		config.Interval = 5 * time.Minute
	}
	if config.Settle <= 0 {
		config.Settle = 30 * time.Second
	}
	return &Scanner{deps: dependencies, config: config, trigger: make(chan struct{}, 1), now: time.Now}
}

// OnAdded registers fn to be called after a scan that added media (the
// converter uses it to look at new files at once). Call it before Run.
func (scanner *Scanner) OnAdded(handler func()) { scanner.onAdded = handler }

// Trigger asks Run for a scan as soon as possible. Triggers that arrive while
// one is pending coalesce into a single scan.
func (scanner *Scanner) Trigger() {
	select {
	case scanner.trigger <- struct{}{}:
	default:
	}
}

// Exclusive runs fn while no scan is changing the database. It waits at most
// for one reconcile step, which only touches the database and stats files.
func (scanner *Scanner) Exclusive(operation func() error) error {
	scanner.reconcile.Lock()
	defer scanner.reconcile.Unlock()
	return operation()
}

// Run scans immediately and then whenever the timer fires, Trigger is
// called, or the media folder changes (debounced by 5 s), until ctx ends.
func (scanner *Scanner) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		scanner.deps.Log.Warn("filesystem watching unavailable; relying on periodic scans", "err", err)
	} else {
		defer watcher.Close()
	}
	var events <-chan fsnotify.Event
	var errs <-chan error
	if watcher != nil {
		events, errs = watcher.Events, watcher.Errors
	}

	ticker := time.NewTicker(scanner.config.Interval)
	defer ticker.Stop()
	soon := time.NewTimer(0) // fires at once: the initial scan
	defer soon.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		case <-soon.C:
		case <-scanner.trigger:
		case event, open := <-events:
			if !open {
				events = nil
			} else {
				scanner.onEvent(event, soon)
			}
			continue
		case err, open := <-errs:
			if !open {
				errs = nil
			} else {
				scanner.onWatchError(err, soon)
			}
			continue
		}
		scanner.scanOnce(ctx, watcher, soon)
	}
}

// onEvent schedules a scan a few seconds after a change in the media
// folder, so a burst of changes (a file being copied in) causes one scan.
// Permission changes alone are ignored.
func (scanner *Scanner) onEvent(event fsnotify.Event, soon *time.Timer) {
	if !event.Has(fsnotify.Chmod) || event.Has(fsnotify.Write) {
		soon.Reset(5 * time.Second)
	}
}

// onWatchError handles a watcher error. An overflow means changes were
// lost, and only a full scan can catch up.
func (scanner *Scanner) onWatchError(err error, soon *time.Timer) {
	if errors.Is(err, fsnotify.ErrEventOverflow) {
		scanner.deps.Log.Info("too many filesystem changes at once; rescanning")
		soon.Reset(time.Second)
		return
	}
	scanner.deps.Log.Warn("filesystem watcher", "err", err)
}

// scanOnce runs one scan for Run and acts on its result.
func (scanner *Scanner) scanOnce(ctx context.Context, watcher *fsnotify.Watcher, soon *time.Timer) {
	result, directories, err := scanner.scanAll(ctx)
	if err != nil {
		if ctx.Err() == nil {
			scanner.deps.Log.Error("library scan failed", "err", err)
		}
		return
	}
	if watcher != nil {
		for _, directory := range directories {
			if err := watcher.Add(directory); err != nil {
				// Usually the inotify watch limit; periodic scans still work.
				scanner.deps.Log.Debug("not watching folder for changes", "path", directory, "err", err)
			}
		}
	}
	if result.Added+result.Removed+result.Subtitles > 0 {
		scanner.deps.Log.Info("library scan", "added", result.Added, "removed", result.Removed, "subtitles", result.Subtitles)
	}
	if result.Waiting > 0 {
		soon.Reset(scanner.config.Settle + 5*time.Second) // come back once they have settled
	}
	if result.Added > 0 && scanner.onAdded != nil {
		scanner.onAdded()
	}
}

// Scan brings the database in step with the media folder once: new files are
// probed and added as unpublished media, sidecar subtitles are imported, and
// media whose files are gone are removed.
func (scanner *Scanner) Scan(ctx context.Context) (Result, error) {
	result, _, err := scanner.scanAll(ctx)
	return result, err
}

func (scanner *Scanner) scanAll(ctx context.Context) (Result, []string, error) {
	scanner.scan.Lock()
	defer scanner.scan.Unlock()
	var result Result

	tree, err := scanner.walk()
	if err != nil {
		return result, nil, err
	}
	known, err := scanner.deps.Store.MediaPaths(ctx)
	if err != nil {
		return result, nil, err
	}
	probed, waiting := scanner.probeNew(ctx, tree.videos, known)
	result.Waiting = waiting

	scanner.reconcile.Lock()
	added, removed, known, err := scanner.applyChanges(ctx, tree, probed)
	scanner.reconcile.Unlock()
	if err != nil {
		return result, nil, err
	}
	result.Added, result.Removed = len(added), removed

	for _, newMedia := range added {
		scanner.lookupMetadata(ctx, newMedia.id, newMedia.guess)
	}
	scanner.retryMetadata(ctx)
	result.Subtitles = scanner.importAllSidecars(ctx, tree, known)
	scanner.backfillPixFmt(ctx, tree.videos)
	scanner.backfillEpisodeEnd(ctx, tree.videos, known)
	return result, tree.directories, nil
}

// tree is what one walk of the media folder found.
type tree struct {
	videos               []found
	subtitlesByDirectory map[string][]string // sidecar subtitle files by folder (relative paths)
	directories          []string            // absolute folder paths, for the change watcher
	unreadable           []string            // relative paths that could not be read
}

type found struct {
	relativePath string // relative, slash-separated
	absolutePath string
	info         fs.FileInfo
}

// walk lists the media folder. Hidden files and folders are skipped. Paths
// that cannot be read are recorded rather than treated as gone: a folder that
// fails to list on a flaky network share must not look deleted.
func (scanner *Scanner) walk() (tree, error) {
	mediaTree := tree{subtitlesByDirectory: map[string][]string{}}
	unreadable := func(path string, err error) {
		scanner.deps.Log.Warn("skipping unreadable path; its library entries are kept", "path", path, "err", err)
		if relativePath, relativeErr := filepath.Rel(scanner.config.MediaDirectory, path); relativeErr == nil {
			mediaTree.unreadable = append(mediaTree.unreadable, filepath.ToSlash(relativePath))
		}
	}
	err := filepath.WalkDir(scanner.config.MediaDirectory, func(filePath string, entry fs.DirEntry, err error) error {
		switch {
		case err != nil && filePath == scanner.config.MediaDirectory:
			return err
		case err != nil:
			unreadable(filePath, err)
			return nil
		case strings.HasPrefix(entry.Name(), ".") && filePath != scanner.config.MediaDirectory:
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		case entry.IsDir():
			mediaTree.directories = append(mediaTree.directories, filePath)
			return nil
		}
		relativePath, err := filepath.Rel(scanner.config.MediaDirectory, filePath)
		if err != nil {
			unreadable(filePath, err)
			return nil
		}
		relativePath = filepath.ToSlash(relativePath)
		switch ext := strings.ToLower(path.Ext(relativePath)); {
		case media.IsVideo(relativePath):
			info, err := entry.Info()
			if err != nil {
				unreadable(filePath, err)
				return nil
			}
			mediaTree.videos = append(mediaTree.videos, found{relativePath: relativePath, absolutePath: filePath, info: info})
		case ext == ".srt" || ext == ".vtt":
			mediaTree.subtitlesByDirectory[path.Dir(relativePath)] = append(mediaTree.subtitlesByDirectory[path.Dir(relativePath)], relativePath)
		}
		return nil
	})
	return mediaTree, err
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
func (scanner *Scanner) probeNew(ctx context.Context, videos []found, known map[string]int64) (out []probedFile, waiting int) {
	for _, video := range videos {
		if _, isKnown := known[video.relativePath]; isKnown {
			continue
		}
		if scanner.now().Sub(video.info.ModTime()) < scanner.config.Settle {
			waiting++
			continue
		}
		probed, err := scanner.probe(ctx, video)
		if err != nil {
			if ctx.Err() != nil {
				return out, waiting
			}
			scanner.deps.Log.Warn("could not add media", "path", video.relativePath, "err", err)
			continue
		}
		out = append(out, probed)
	}
	return out, waiting
}

func (scanner *Scanner) probe(ctx context.Context, candidate found) (probedFile, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	probe, err := scanner.deps.Prober.Probe(ctx, candidate.absolutePath)
	if err != nil {
		return probedFile{}, err
	}
	video, hasVideo := probe.Video()
	if !hasVideo {
		return probedFile{}, ffmpeg.ErrNoVideo
	}
	guess := ParsePath(candidate.relativePath)
	return probedFile{found: candidate, guess: guess, media: store.ScannedMedia{
		Path: candidate.relativePath, Title: guess.Title, Year: guess.Year, Season: guess.Season, Episode: guess.Episode, EpisodeEnd: guess.EpisodeEnd,
		Duration: probe.Duration, Container: probe.Container,
		VideoCodec: video.Codec, AudioCodec: probe.AudioCodec(), PixFmt: video.PixFmt, Size: candidate.info.Size(),
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
func (scanner *Scanner) applyChanges(ctx context.Context, tree tree, probed []probedFile) ([]addedMedia, int, map[string]int64, error) {
	known, err := scanner.deps.Store.MediaPaths(ctx)
	if err != nil {
		return nil, 0, nil, err
	}
	var added []addedMedia
	for _, candidate := range probed {
		if _, isKnown := known[candidate.relativePath]; isKnown || !exists(candidate.absolutePath) {
			continue
		}
		id, inserted, err := scanner.deps.Store.InsertScannedMedia(ctx, candidate.media)
		if err != nil {
			return added, 0, known, err
		}
		if inserted {
			known[candidate.relativePath] = id
			added = append(added, addedMedia{id: id, guess: candidate.guess})
		}
	}

	// If the folder has no videos while the database has rows, the drive is
	// most likely unmounted: keep everything rather than wipe the library.
	if len(tree.videos) == 0 && len(known) > 0 {
		scanner.deps.Log.Warn("media folder has no videos; skipping removals in case it is unmounted", "dir", scanner.config.MediaDirectory)
		return added, 0, known, nil
	}
	onDisk := make(map[string]bool, len(tree.videos))
	for _, video := range tree.videos {
		onDisk[video.relativePath] = true
	}
	removed := 0
	for _, relativePath := range missingMedia(known, onDisk, tree.unreadable) {
		if exists(filepath.Join(scanner.config.MediaDirectory, filepath.FromSlash(relativePath))) {
			continue // appeared after the walk (a converted file)
		}
		if err := scanner.deps.Store.DeleteMedia(ctx, known[relativePath]); err != nil {
			return added, removed, known, err
		}
		delete(known, relativePath)
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
	for path := range known {
		if !onDisk[path] && !underAny(path, unreadable) {
			gone = append(gone, path)
		}
	}
	return gone
}

// underAny reports whether p is one of roots or inside one of them.
func underAny(path string, roots []string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+"/") {
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
func (scanner *Scanner) lookupMetadata(ctx context.Context, id int64, guess Guess) {
	if scanner.deps.Meta == nil {
		return
	}
	scannedMetadata, err := scanner.metadataFor(ctx, guess)
	switch {
	case errors.Is(err, metadata.ErrDisabled):
		return // not a lookup: try again once a key is set
	case errors.Is(err, metadata.ErrNoMatch):
		err = scanner.deps.Store.MarkMetadataChecked(ctx, id)
	case err != nil:
		scanner.deps.Log.Warn("metadata lookup failed; it is retried later", "title", guess.Title, "err", err)
		err = scanner.deps.Store.MarkMetadataChecked(ctx, id)
	default:
		err = scanner.deps.Store.SetScannedMetadata(ctx, id, scannedMetadata)
	}
	if err != nil {
		scanner.deps.Log.Warn("saving metadata failed", "media", id, "err", err)
	}
}

// retryMetadata looks up again unpublished media that is still missing a
// poster or description (see metadataRetryAfter).
func (scanner *Scanner) retryMetadata(ctx context.Context) {
	if scanner.deps.Meta == nil {
		return
	}
	list, err := scanner.deps.Store.MediaNeedingMetadata(ctx, scanner.now().Add(-metadataRetryAfter), metadataRetryBatch)
	if err != nil {
		scanner.deps.Log.Warn("listing media without metadata", "err", err)
		return
	}
	for _, media := range list {
		if ctx.Err() != nil {
			return
		}
		scanner.lookupMetadata(ctx, media.ID, guessForMedia(media))
	}
}

// RefreshMetadata looks a media item up on TMDB again and saves what it
// finds, published or not: the control panel's "Refresh metadata". It uses
// the item's current collection, season and episode, which an
// administrator may have corrected. It returns metadata.ErrNoMatch when
// TMDB has nothing and metadata.ErrDisabled without an API key.
func (scanner *Scanner) RefreshMetadata(ctx context.Context, id int64) error {
	if scanner.deps.Meta == nil {
		return metadata.ErrDisabled
	}
	media, err := scanner.deps.Store.MediaByID(ctx, id)
	if err != nil {
		return err
	}
	scannedMetadata, err := scanner.metadataFor(ctx, guessForMedia(media))
	if err != nil {
		return err
	}
	return scanner.deps.Store.ReplaceMetadata(ctx, id, scannedMetadata)
}

// guessForMedia is what to look a stored item up by: the guess from its
// file name, with the collection, season and episode from the database.
func guessForMedia(media store.Media) Guess {
	guess := ParsePath(media.Path)
	if !guess.IsEpisode() && (media.CollectionName == "" || (media.Season == 0 && media.Episode == 0)) {
		return guess // a film, possibly in a collection of films
	}
	if media.CollectionName != "" {
		if !strings.EqualFold(guess.Show, media.CollectionName) {
			guess.Year = "" // the year was a film's or another show's; TMDB knows the show's
		}
		guess.Show = media.CollectionName
	}
	if media.Season > 0 || media.Episode > 0 {
		guess.Season, guess.Episode = media.Season, media.Episode
	}
	return guess
}

// metadataFor looks g up, within 30 seconds.
func (scanner *Scanner) metadataFor(ctx context.Context, guess Guess) (store.ScannedMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	info, err := scanner.findMetadata(ctx, guess)
	if err != nil {
		return store.ScannedMetadata{}, err
	}
	return store.ScannedMetadata{Title: info.Title, Year: info.Year, Overview: info.Overview, Poster: info.Poster}, nil
}

// findMetadata looks up a film, or an episode with its show's poster.
func (scanner *Scanner) findMetadata(ctx context.Context, guess Guess) (metadata.Info, error) {
	if !guess.IsEpisode() {
		return scanner.deps.Meta.Movie(ctx, guess.Title, guess.Year)
	}
	show, err := scanner.deps.Meta.Show(ctx, guess.Show, guess.Year)
	if err != nil {
		return metadata.Info{}, err
	}
	episodeInfo, err := scanner.findEpisode(ctx, show.ID, guess)
	if err != nil {
		return metadata.Info{}, err
	}
	episodeInfo.Poster = show.Poster
	return episodeInfo, nil
}

// findEpisode finds g's episode of a show: by the title in the file name
// when there is one, since TMDB's episode numbers can differ from the
// file's (see match.go), and otherwise by number.
func (scanner *Scanner) findEpisode(ctx context.Context, showID int, guess Guess) (metadata.Info, error) {
	title := fileEpisodeTitle(guess)
	if title == "" {
		return scanner.deps.Meta.Episode(ctx, showID, guess.Season, guess.Episode)
	}
	episodes, err := scanner.deps.Meta.Season(ctx, showID, guess.Season)
	if err != nil {
		return metadata.Info{}, err
	}
	if episode, found := episodeByTitle(episodes, title, guess.Episode); found {
		if episode.Number != guess.Episode {
			scanner.deps.Log.Debug("episode matched by title", "show", guess.Show, "season", guess.Season,
				"file_episode", guess.Episode, "tmdb_episode", episode.Number, "title", episode.Title)
		}
		return episode.Info, nil
	}
	if episode, found := episodeByNumber(episodes, guess.Episode); found {
		return episode.Info, nil
	}
	return metadata.Info{}, metadata.ErrNoMatch
}

// importAllSidecars imports subtitle files next to every known video.
func (scanner *Scanner) importAllSidecars(ctx context.Context, tree tree, known map[string]int64) int {
	imported, err := scanner.deps.Store.SubtitleSources(ctx)
	if err != nil {
		scanner.deps.Log.Warn("could not list imported subtitles", "err", err)
		return 0
	}
	importedCount := 0
	for _, video := range tree.videos {
		if id, isKnown := known[video.relativePath]; isKnown {
			importedCount += scanner.importSidecars(ctx, id, video.relativePath, tree.subtitlesByDirectory[path.Dir(video.relativePath)], imported)
		}
	}
	return importedCount
}

// importSidecars imports subtitle files named after a video, such as
// "Movie.srt" or "Movie.en.vtt" next to "Movie.mp4". The converter writes
// extracted subtitles with the same naming, so they come in here too.
func (scanner *Scanner) importSidecars(ctx context.Context, mediaID int64, videoRelativePath string, candidates []string, imported map[string]bool) int {
	importedCount := 0
	for _, sub := range candidates {
		if imported[sub] {
			continue
		}
		language, isSidecar := SidecarLanguage(videoRelativePath, sub)
		if !isSidecar {
			continue
		}
		data, err := os.ReadFile(filepath.Join(scanner.config.MediaDirectory, filepath.FromSlash(sub)))
		if err != nil {
			scanner.deps.Log.Warn("could not read subtitle", "path", sub, "err", err)
			continue
		}
		cues, err := subtitle.Parse(data)
		if err != nil {
			scanner.deps.Log.Warn("could not parse subtitle", "path", sub, "err", err)
			imported[sub] = true // do not retry a broken file on every scan of this run
			continue
		}
		_, inserted, err := scanner.deps.Store.AddSubtitle(ctx, store.NewSubtitle{
			MediaID: mediaID, Language: language, Label: subtitle.LanguageName(language), Source: sub, Cues: cues,
		})
		if err != nil {
			scanner.deps.Log.Warn("could not save subtitle", "path", sub, "err", err)
			continue
		}
		imported[sub] = true
		if inserted {
			importedCount++
		}
	}
	return importedCount
}

// SidecarLanguage reports whether subRel is a sidecar subtitle of the video
// at videoRel and, if so, its language: "Movie.srt" is "und",
// "Movie.en.srt" is "en" and "Movie.forced.en.srt" is "en".
func SidecarLanguage(videoRelativePath, subtitleRelativePath string) (language string, isSidecar bool) {
	base := strings.TrimSuffix(path.Base(videoRelativePath), path.Ext(videoRelativePath))
	name := strings.TrimSuffix(path.Base(subtitleRelativePath), path.Ext(subtitleRelativePath))
	if path.Dir(videoRelativePath) != path.Dir(subtitleRelativePath) || (name != base && !strings.HasPrefix(name, base+".")) {
		return "", false
	}
	language = strings.ToLower(strings.TrimPrefix(strings.TrimPrefix(name, base), "."))
	if i := strings.LastIndexByte(language, '.'); i >= 0 {
		language = language[i+1:]
	}
	if language == "" {
		language = "und"
	}
	return language, true
}

// backfillLimit caps how many files one scan re-probes, so the first scan
// after upgrading a large library stays short; later scans do the rest.
const backfillLimit = 50

// backfillEpisodeEnd records the episode range of multi-episode files that
// were scanned before the range was stored (see store.SetEpisodeEnd).
func (scanner *Scanner) backfillEpisodeEnd(ctx context.Context, videos []found, known map[string]int64) {
	for _, video := range videos {
		id, isKnown := known[video.relativePath]
		if !isKnown {
			continue
		}
		if guess := ParsePath(video.relativePath); guess.EpisodeEnd > 0 {
			if err := scanner.deps.Store.SetEpisodeEnd(ctx, id, guess.Episode, guess.EpisodeEnd); err != nil {
				scanner.deps.Log.Warn("recording an episode range", "path", video.relativePath, "err", err)
			}
		}
	}
}

// backfillPixFmt records the pixel format of H.264 files added before it was
// stored. Without it, a 10-bit H.264 file (which no browser plays) would be
// served as is and never converted.
func (scanner *Scanner) backfillPixFmt(ctx context.Context, videos []found) {
	missing, err := scanner.deps.Store.H264MissingPixFmt(ctx)
	if err != nil {
		scanner.deps.Log.Warn("could not list media to re-probe", "err", err)
		return
	}
	done := 0
	for _, scannedVideo := range videos {
		id, isMissing := missing[scannedVideo.relativePath]
		if !isMissing || done == backfillLimit {
			continue
		}
		done++
		pctx, cancel := context.WithTimeout(ctx, time.Minute)
		probe, err := scanner.deps.Prober.Probe(pctx, scannedVideo.absolutePath)
		cancel()
		if err != nil {
			scanner.deps.Log.Warn("could not re-probe media", "path", scannedVideo.relativePath, "err", err)
			continue
		}
		if video, hasVideo := probe.Video(); hasVideo && video.PixFmt != "" {
			if err := scanner.deps.Store.SetPixFmt(ctx, id, video.PixFmt); err != nil {
				scanner.deps.Log.Warn("could not record pixel format", "path", scannedVideo.relativePath, "err", err)
			}
		}
	}
}

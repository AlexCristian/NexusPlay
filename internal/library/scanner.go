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

	"github.com/AlexCristian/NexusPlay/internal/collections"
	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/media"
	"github.com/AlexCristian/NexusPlay/internal/metadata"
	"github.com/AlexCristian/NexusPlay/internal/store"
	"github.com/AlexCristian/NexusPlay/internal/subtitle"
)

// [Scanner] keeps the database in step with the media folder. It rescans on a
// timer and shortly after any change in the folder. Create one with
// [NewScanner] and start it with [Scanner.Run]. It is safe for concurrent
// use.
//
// Locking: [Scanner.scan] lets only one scan run at a time.
// [Scanner.reconcile] is held only while a scan inserts new media rows and
// deletes rows whose files are gone (re-checking the disk for each); metadata,
// subtitle, pixel-format and episode-range writes happen after it is
// released, and it is never held while probing files or calling TMDB. The
// converter holds [Scanner.reconcile] too, through [Scanner.Exclusive],
// while it swaps a file and its row, so a scan never adds or removes a row
// for a file renamed but not yet updated in the database. Neither lock is reentrant: never call
// [Scanner.Scan] from inside [Scanner.Exclusive].
type Scanner struct {
	// [dependencies] are the database, prober, metadata lookups and log.
	dependencies Dependencies
	// [config] holds the settings, with defaults filled in.
	config Config
	// [trigger] carries scan requests from [Scanner.Trigger] to [Scanner.Run].
	// It holds at most one, so requests made while one is pending become one
	// scan.
	trigger chan struct{}
	// [now] tells the time, for the settle check. Tests replace it.
	now func() time.Time

	// [scan] is held for a whole scan.
	scan sync.Mutex
	// [reconcile] is held while the database changes; see [Scanner].
	reconcile sync.Mutex
	// [onAdded] is called after a scan that added media, or [nil]. Set it with
	// [Scanner.OnAdded] before [Scanner.Run].
	onAdded func()
	// [backfillSkipped] holds the paths whose re-probe for a pixel format
	// failed or found none, so [Scanner.backfillPixelFormat] does not try
	// them again until the program restarts. Only scans use it, and they run
	// one at a time.
	backfillSkipped collections.Set[string]
}

// [NewScanner] creates a scanner. Nothing runs until [Scanner.Run] or
// [Scanner.Scan].
//
// Parameters:
//   - [dependencies]: the database, prober, metadata lookups and log.
//   - [config]: the media folder and timings; durations of 0 or less get
//     their defaults.
//
// Returns:
//   - [*Scanner]: the new scanner.
func NewScanner(dependencies Dependencies, config Config) *Scanner {
	if config.Interval <= 0 {
		config.Interval = 5 * time.Minute
	}
	if config.Settle <= 0 {
		config.Settle = 30 * time.Second
	}
	return &Scanner{dependencies: dependencies, config: config, trigger: make(chan struct{}, 1), now: time.Now, backfillSkipped: collections.NewSet[string]()}
}

// [OnAdded] sets what to call after a scan that added media; the converter
// uses it to look at new files at once. Call it before [Scanner.Run], since
// it is not synchronized.
//
// Parameters:
//   - [handler]: called on the scanning goroutine after each such scan.
func (scanner *Scanner) OnAdded(handler func()) { scanner.onAdded = handler }

// [Trigger] asks [Scanner.Run] for a scan as soon as possible, without
// waiting for it. Requests that arrive while one is pending become a single
// scan.
func (scanner *Scanner) Trigger() {
	select {
	case scanner.trigger <- struct{}{}:
	default:
	}
}

// [Exclusive] runs an operation while no scan is adding or removing media
// rows. It waits at most for one such step, which only touches the database
// and checks whether files exist. A scan may meanwhile write metadata,
// subtitles and pixel formats.
//
// Parameters:
//   - [operation]: the work to do under the lock. It must not call
//     [Scanner.Scan] or [Scanner.Exclusive].
//
// Returns:
//   - [error]: the error [operation] returned.
func (scanner *Scanner) Exclusive(operation func() error) error {
	scanner.reconcile.Lock()
	defer scanner.reconcile.Unlock()
	return operation()
}

// [Run] scans at once and then whenever the timer fires, [Scanner.Trigger] is
// called, or the media folder changes (5 seconds after the last change),
// until [ctx] ends; also one second after the watcher loses changes, and
// [Config.Settle] plus 5 seconds after a scan that found files still
// settling. If no watcher can be created it logs a warning and relies on
// the timer; folders that cannot be watched are logged at debug level. It blocks, so call it in its own goroutine.
//
// Parameters:
//   - [ctx]: stops the scanner when cancelled.
//
// Returns:
//   - [error]: always [nil], once [ctx] ends; failed scans are logged.
func (scanner *Scanner) Run(ctx context.Context) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		scanner.dependencies.Log.Warn("filesystem watching unavailable; relying on periodic scans", "err", err)
	} else {
		defer watcher.Close()
	}
	var events <-chan fsnotify.Event
	var problems <-chan error
	if watcher != nil {
		events, problems = watcher.Events, watcher.Errors
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
		case err, open := <-problems:
			if !open {
				problems = nil
			} else {
				scanner.onWatchError(err, soon)
			}
			continue
		}
		scanner.scanOnce(ctx, watcher, soon)
	}
}

// [onEvent] schedules a scan 5 seconds after a change in the media folder,
// postponing any already scheduled, so a burst of changes (a file being
// copied in) causes one scan. Events that include a permission change but
// no write are ignored.
//
// Parameters:
//   - [event]: the change.
//   - [soon]: the timer that starts the next scan.
func (scanner *Scanner) onEvent(event fsnotify.Event, soon *time.Timer) {
	if !event.Has(fsnotify.Chmod) || event.Has(fsnotify.Write) {
		soon.Reset(5 * time.Second)
	}
}

// [onWatchError] handles an error from the folder watcher. An overflow means
// changes were lost, and only a full scan, scheduled in a second, can catch
// up; anything else is logged.
//
// Parameters:
//   - [err]: the watcher's error.
//   - [soon]: the timer that starts the next scan.
func (scanner *Scanner) onWatchError(err error, soon *time.Timer) {
	if errors.Is(err, fsnotify.ErrEventOverflow) {
		scanner.dependencies.Log.Info("too many filesystem changes at once; rescanning")
		soon.Reset(time.Second)
		return
	}
	scanner.dependencies.Log.Warn("filesystem watcher", "err", err)
}

// [scanOnce] runs one scan for [Scanner.Run] and acts on its result: it
// watches the folders found, logs what changed, schedules another scan for
// files still settling, and calls the [Scanner.OnAdded] handler if media
// were added. A failed scan is logged.
//
// Parameters:
//   - [ctx]: cancels the scan.
//   - [watcher]: the folder watcher, or [nil] if there is none.
//   - [soon]: the timer that starts the next scan.
func (scanner *Scanner) scanOnce(ctx context.Context, watcher *fsnotify.Watcher, soon *time.Timer) {
	result, directories, err := scanner.scanAll(ctx)
	if err != nil {
		if ctx.Err() == nil {
			scanner.dependencies.Log.Error("library scan failed", "err", err)
		}
		return
	}
	if watcher != nil {
		for _, directory := range directories {
			if err := watcher.Add(directory); err != nil {
				// Usually the inotify watch limit; periodic scans still work.
				scanner.dependencies.Log.Debug("not watching folder for changes", "path", directory, "err", err)
			}
		}
	}
	if result.Added+result.Removed+result.Subtitles > 0 {
		scanner.dependencies.Log.Info("library scan", "added", result.Added, "removed", result.Removed, "subtitles", result.Subtitles)
	}
	if result.Waiting > 0 {
		soon.Reset(scanner.config.Settle + 5*time.Second) // come back once they have settled
	}
	if result.Added > 0 && scanner.onAdded != nil {
		scanner.onAdded()
	}
}

// [Scan] brings the database in step with the media folder once: new files
// are probed and added as unpublished media and looked up online, sidecar
// subtitles are imported, and media whose files are gone are removed. It
// waits for any scan already running.
//
// Parameters:
//   - [ctx]: cancels the scan.
//
// Returns:
//   - [Result]: what the scan did. On an [error], only [Result.Waiting] is
//     set, even if some media were already added or removed.
//   - [error]: [nil] on success, the error from reading the media folder
//     itself, or a database error. Problems with single files are logged,
//     not returned.
func (scanner *Scanner) Scan(ctx context.Context) (Result, error) {
	result, _, err := scanner.scanAll(ctx)
	return result, err
}

// [scanAll] does the work of [Scanner.Scan] and also reports the folders it
// found, for [Scanner.Run] to watch.
//
// Parameters:
//   - [ctx]: cancels the scan.
//
// Returns:
//   - [Result]: what the scan did, so far as it got.
//   - [[]string]: the absolute paths of every folder found, or [nil] if
//     [error] is not [nil].
//   - [error]: as [Scanner.Scan] describes.
func (scanner *Scanner) scanAll(ctx context.Context) (Result, []string, error) {
	scanner.scan.Lock()
	defer scanner.scan.Unlock()
	var result Result

	tree, err := scanner.walk()
	if err != nil {
		return result, nil, err
	}
	known, err := scanner.dependencies.Store.MediaPaths(ctx)
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
	scanner.backfillPixelFormat(ctx, tree.videos)
	scanner.backfillEpisodeEnd(ctx, tree.videos, known)
	return result, tree.directories, nil
}

// [tree] is what one walk of the media folder found.
type tree struct {
	// [videos] lists the video files found.
	videos []videoFile
	// [subtitlesByDirectory] maps each folder, relative to the media folder, to
	// the ".srt" and ".vtt" files in it, as relative paths.
	subtitlesByDirectory map[string][]string
	// [directories] lists the absolute paths of every folder found, for the
	// change watcher.
	directories []string
	// [unreadable] lists the relative paths that could not be read; media under
	// them are kept.
	unreadable []string
}

// [videoFile] is one video file found by a walk of the media folder.
type videoFile struct {
	// [relativePath] is the file's path relative to the media folder, with
	// forward slashes.
	relativePath string
	// [absolutePath] is the file's full path.
	absolutePath string
	// [info] holds the file's size and modification time; for a symbolic
	// link, the link's own.
	info fs.FileInfo
}

// [walk] lists the media folder and all its subfolders. Hidden files and
// folders (starting with ".") are skipped, and symbolic links to folders are
// not followed. Paths that cannot be read are
// logged and recorded rather than treated as gone: a folder that fails to
// list on a flaky network share must not look deleted.
//
// Returns:
//   - [tree]: what was found.
//   - [error]: [nil] on success, or the error from reading the media folder
//     itself.
func (scanner *Scanner) walk() (tree, error) {
	mediaTree := tree{subtitlesByDirectory: map[string][]string{}}
	unreadable := func(path string, err error) {
		scanner.dependencies.Log.Warn("skipping unreadable path; its library entries are kept", "path", path, "err", err)
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
		switch extension := strings.ToLower(path.Ext(relativePath)); {
		case media.IsVideo(relativePath):
			info, err := entry.Info()
			if err != nil {
				unreadable(filePath, err)
				return nil
			}
			mediaTree.videos = append(mediaTree.videos, videoFile{relativePath: relativePath, absolutePath: filePath, info: info})
		case extension == ".srt" || extension == ".vtt":
			mediaTree.subtitlesByDirectory[path.Dir(relativePath)] = append(mediaTree.subtitlesByDirectory[path.Dir(relativePath)], relativePath)
		}
		return nil
	})
	return mediaTree, err
}

// [probedFile] is a new file that has been probed and is ready to be added.
type probedFile struct {
	// [videoFile] is the file as the walk found it.
	videoFile
	// [media] is what to store for it.
	media store.ScannedMedia
	// [guess] is what its path says, for the metadata lookup.
	guess Guess
}

// [probeNew] probes the files that are not in the library yet and have
// settled: modified at least [Config.Settle] ago, so a file still being
// copied in is not read half-written. Files that fail to probe are logged
// and skipped; the next scan tries them again.
//
// Parameters:
//   - [ctx]: cancels the probes; once it ends, the rest are not probed.
//   - [videos]: the video files the walk found.
//   - [known]: the paths already in the library.
//
// Returns:
//   - [[]probedFile] ([probedFiles]): the new files that probed
//     successfully.
//   - [int] ([waiting]): how many new files are still settling.
func (scanner *Scanner) probeNew(ctx context.Context, videos []videoFile, known map[string]int64) (probedFiles []probedFile, waiting int) {
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
				return probedFiles, waiting
			}
			scanner.dependencies.Log.Warn("could not add media", "path", video.relativePath, "err", err)
			continue
		}
		probedFiles = append(probedFiles, probed)
	}
	return probedFiles, waiting
}

// [probe] reads a new file's streams, limited to one minute, and prepares
// what to store for it.
//
// Parameters:
//   - [ctx]: cancels the probe.
//   - [candidate]: the file.
//
// Returns:
//   - [probedFile]: the file with what to store and its path's guess, or a
//     zero [probedFile] if [error] is not [nil].
//   - [error]: [nil] on success, [ffmpeg.ErrNoVideo] if the file has no
//     video stream, or the probe's error.
func (scanner *Scanner) probe(ctx context.Context, candidate videoFile) (probedFile, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	probe, err := scanner.dependencies.Prober.Probe(ctx, candidate.absolutePath)
	if err != nil {
		return probedFile{}, err
	}
	video, hasVideo := probe.Video()
	if !hasVideo {
		return probedFile{}, ffmpeg.ErrNoVideo
	}
	guess := ParsePath(candidate.relativePath)
	return probedFile{videoFile: candidate, guess: guess, media: store.ScannedMedia{
		Path: candidate.relativePath, Title: guess.Title, Year: guess.Year, Season: guess.Season, Episode: guess.Episode, EpisodeEnd: guess.EpisodeEnd,
		Duration: probe.Duration, Container: probe.Container,
		VideoCodec: video.Codec, AudioCodec: probe.AudioCodec(), PixelFormat: video.PixelFormat, Size: candidate.info.Size(),
	}}, nil
}

// [addedMedia] is a media item a scan has just added, for the metadata
// lookup.
type addedMedia struct {
	// [id] is the new media item's ID.
	id int64
	// [guess] is what its path says.
	guess Guess
}

// [applyChanges] inserts the new files and deletes the media whose files are
// gone. It runs under the reconcile lock, after the slow walk and probes, so
// it re-reads the database and re-checks each file on disk instead of
// trusting the walk: the converter may have swapped a file in the meantime.
// When the media folder has no videos at all but the library has media, it
// removes nothing, since the drive is most likely unmounted.
//
// Parameters:
//   - [ctx]: cancels the database work.
//   - [tree]: what the walk found.
//   - [probed]: the new files that probed successfully.
//
// Returns:
//   - [[]addedMedia]: the media added.
//   - [int]: how many media were removed.
//   - [map[string]int64]: every known path mapped to its media ID, after
//     the changes.
//   - [error]: [nil] on success, or the database error; changes made before
//     it stay.
func (scanner *Scanner) applyChanges(ctx context.Context, tree tree, probed []probedFile) ([]addedMedia, int, map[string]int64, error) {
	known, err := scanner.dependencies.Store.MediaPaths(ctx)
	if err != nil {
		return nil, 0, nil, err
	}
	var added []addedMedia
	for _, candidate := range probed {
		if _, isKnown := known[candidate.relativePath]; isKnown || !exists(candidate.absolutePath) {
			continue
		}
		id, inserted, err := scanner.dependencies.Store.InsertScannedMedia(ctx, candidate.media)
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
		scanner.dependencies.Log.Warn("media folder has no videos; skipping removals in case it is unmounted", "dir", scanner.config.MediaDirectory)
		return added, 0, known, nil
	}
	onDisk := collections.NewSet[string]()
	for _, video := range tree.videos {
		onDisk.Add(video.relativePath)
	}
	removed := 0
	for _, relativePath := range missingMedia(known, onDisk, tree.unreadable) {
		if exists(filepath.Join(scanner.config.MediaDirectory, filepath.FromSlash(relativePath))) {
			continue // appeared after the walk (a converted file)
		}
		if err := scanner.dependencies.Store.DeleteMedia(ctx, known[relativePath]); err != nil {
			return added, removed, known, err
		}
		delete(known, relativePath)
		removed++
	}
	return added, removed, known, nil
}

// [exists] reports whether something exists at a path. A symbolic link
// counts, even if what it points to does not exist.
//
// Parameters:
//   - [path]: the path to check.
//
// Returns:
//   - [bool]: [true] if the path exists, [false] if it does not or cannot be
//     checked.
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// [missingMedia] finds the known media whose files are gone: not seen on
// disk, and not inside a path that could not be read (whose files may well
// still exist).
//
// Parameters:
//   - [known]: every known path, relative to the media folder.
//   - [onDisk]: the paths the walk saw.
//   - [unreadable]: the paths that could not be read.
//
// Returns:
//   - [[]string]: the missing paths, in no particular order; [nil] if none.
func missingMedia(known map[string]int64, onDisk collections.Set[string], unreadable []string) []string {
	var gone []string
	for path := range known {
		if !onDisk.Contains(path) && !underAny(path, unreadable) {
			gone = append(gone, path)
		}
	}
	return gone
}

// [underAny] reports whether a path is one of some folders or inside one of
// them.
//
// Parameters:
//   - [path]: a path relative to the media folder.
//   - [roots]: the folders, relative to the media folder.
//
// Returns:
//   - [bool]: [true] if [path] equals or is under one of [roots], [false]
//     otherwise.
func underAny(path string, roots []string) bool {
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}

// Retrying lookups for unpublished media still missing a poster or a
// description.
const (
	// [metadataRetryAfter] is how long an unpublished item missing a poster or
	// description waits before it is looked up again. That covers files scanned
	// before an API key was set and lookups that failed for a passing reason.
	metadataRetryAfter = time.Hour
	// [metadataRetryBatch] is the most items one scan looks up again.
	metadataRetryBatch = 25
)

// [lookupMetadata] fills in an unpublished item's details from TMDB. It is
// optional: failures are logged and the file-name guess stays. A lookup that
// failed or found nothing is recorded, so it is retried only after
// [metadataRetryAfter]; with no API key nothing is recorded, so it is tried
// again once a key is set.
//
// Parameters:
//   - [ctx]: cancels the lookup.
//   - [id]: the media item.
//   - [guess]: what to look it up by.
func (scanner *Scanner) lookupMetadata(ctx context.Context, id int64, guess Guess) {
	if scanner.dependencies.Metadata == nil {
		return
	}
	scannedMetadata, err := scanner.metadataFor(ctx, guess)
	switch {
	case errors.Is(err, metadata.ErrDisabled):
		return // not a lookup: try again once a key is set
	case errors.Is(err, metadata.ErrNoMatch):
		err = scanner.dependencies.Store.MarkMetadataChecked(ctx, id)
	case err != nil:
		scanner.dependencies.Log.Warn("metadata lookup failed; it is retried later", "title", guess.Title, "err", err)
		err = scanner.dependencies.Store.MarkMetadataChecked(ctx, id)
	default:
		err = scanner.dependencies.Store.SetScannedMetadata(ctx, id, scannedMetadata)
	}
	if err != nil {
		scanner.dependencies.Log.Warn("saving metadata failed", "media", id, "err", err)
	}
}

// [retryMetadata] looks up again, with [Scanner.lookupMetadata], up to
// [metadataRetryBatch] unpublished items still missing a poster or
// description and not looked up for [metadataRetryAfter].
//
// Parameters:
//   - [ctx]: cancels the lookups; once it ends, the rest are skipped.
func (scanner *Scanner) retryMetadata(ctx context.Context) {
	if scanner.dependencies.Metadata == nil {
		return
	}
	needing, err := scanner.dependencies.Store.MediaNeedingMetadata(ctx, scanner.now().Add(-metadataRetryAfter), metadataRetryBatch)
	if err != nil {
		scanner.dependencies.Log.Warn("listing media without metadata", "err", err)
		return
	}
	for _, media := range needing {
		if ctx.Err() != nil {
			return
		}
		scanner.lookupMetadata(ctx, media.ID, guessForMedia(media))
	}
}

// [RefreshMetadata] looks a media item up on TMDB again and saves what it
// finds, published or not: the control panel's "Refresh metadata". It uses
// the item's current collection, season and episode, which an administrator
// may have corrected (see [guessForMedia]).
//
// Parameters:
//   - [ctx]: cancels the lookup.
//   - [id]: the media item.
//
// Returns:
//   - [error]: [nil] on success; [metadata.ErrDisabled] without an API key
//     or metadata lookups; [metadata.ErrNoMatch] when TMDB has nothing;
//     [store.ErrNotFound] if the item does not exist; otherwise the lookup's
//     or database's error.
func (scanner *Scanner) RefreshMetadata(ctx context.Context, id int64) error {
	if scanner.dependencies.Metadata == nil {
		return metadata.ErrDisabled
	}
	media, err := scanner.dependencies.Store.MediaByID(ctx, id)
	if err != nil {
		return err
	}
	scannedMetadata, err := scanner.metadataFor(ctx, guessForMedia(media))
	if err != nil {
		return err
	}
	return scanner.dependencies.Store.ReplaceMetadata(ctx, id, scannedMetadata)
}

// [guessForMedia] works out what to look a stored item up by: the guess from
// its file name, with the collection, season and episode from the database
// where they are set.
//
// Parameters:
//   - [media]: the stored item.
//
// Returns:
//   - [Guess]: the guess to look up. A film stays a film unless it has a
//     collection and a season or episode. When the collection's name differs
//     from the show the file name gives, the year is dropped, since it was a
//     film's or another show's.
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

// [metadataFor] looks up a guess, limited to 30 seconds, and keeps what the
// database stores.
//
// Parameters:
//   - [ctx]: cancels the lookup.
//   - [guess]: what to look up.
//
// Returns:
//   - [store.ScannedMetadata]: the title, year, overview and poster found,
//     or a zero [store.ScannedMetadata] if [error] is not [nil].
//   - [error]: as [Scanner.findMetadata] describes.
func (scanner *Scanner) metadataFor(ctx context.Context, guess Guess) (store.ScannedMetadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	info, err := scanner.findMetadata(ctx, guess)
	if err != nil {
		return store.ScannedMetadata{}, err
	}
	return store.ScannedMetadata{Title: info.Title, Year: info.Year, Overview: info.Overview, Poster: info.Poster}, nil
}

// [findMetadata] looks up a film, or an episode with its show's poster.
//
// Parameters:
//   - [ctx]: cancels the lookups.
//   - [guess]: what to look up.
//
// Returns:
//   - [metadata.Info]: the details found, or a zero [metadata.Info] if
//     [error] is not [nil].
//   - [error]: [nil] on success, or the lookup's error:
//     [metadata.ErrNoMatch], [metadata.ErrDisabled] or a network error.
func (scanner *Scanner) findMetadata(ctx context.Context, guess Guess) (metadata.Info, error) {
	if !guess.IsEpisode() {
		return scanner.dependencies.Metadata.Movie(ctx, guess.Title, guess.Year)
	}
	show, err := scanner.dependencies.Metadata.Show(ctx, guess.Show, guess.Year)
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

// [findEpisode] finds a guess's episode of a show: by the title in the file
// name when there is one, since TMDB's episode numbers can differ from the
// file's (see match.go), and otherwise, or if no title matches, by number.
//
// Parameters:
//   - [ctx]: cancels the lookups.
//   - [showID]: the show's TMDB ID.
//   - [guess]: the episode's guess.
//
// Returns:
//   - [metadata.Info]: the episode's details, or a zero [metadata.Info] if
//     [error] is not [nil].
//   - [error]: [nil] on success, [metadata.ErrNoMatch] if no episode matches,
//     or the lookup's error.
func (scanner *Scanner) findEpisode(ctx context.Context, showID int, guess Guess) (metadata.Info, error) {
	title := fileEpisodeTitle(guess)
	if title == "" {
		return scanner.dependencies.Metadata.Episode(ctx, showID, guess.Season, guess.Episode)
	}
	episodes, err := scanner.dependencies.Metadata.Season(ctx, showID, guess.Season)
	if err != nil {
		return metadata.Info{}, err
	}
	if episode, found := episodeByTitle(episodes, title, guess.Episode); found {
		if episode.Number != guess.Episode {
			scanner.dependencies.Log.Debug("episode matched by title", "show", guess.Show, "season", guess.Season,
				"file_episode", guess.Episode, "tmdb_episode", episode.Number, "title", episode.Title)
		}
		return episode.Info, nil
	}
	if episode, found := episodeByNumber(episodes, guess.Episode); found {
		return episode.Info, nil
	}
	return metadata.Info{}, metadata.ErrNoMatch
}

// [importAllSidecars] imports the subtitle files next to every known video
// (see [Scanner.importSidecars]). If the imported sources cannot be listed
// it logs the problem and imports nothing.
//
// Parameters:
//   - [ctx]: cancels the database work.
//   - [tree]: what the walk found.
//   - [known]: every known path mapped to its media ID.
//
// Returns:
//   - [int]: how many subtitle tracks were added.
func (scanner *Scanner) importAllSidecars(ctx context.Context, tree tree, known map[string]int64) int {
	imported, err := scanner.dependencies.Store.SubtitleSources(ctx)
	if err != nil {
		scanner.dependencies.Log.Warn("could not list imported subtitles", "err", err)
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

// [importSidecars] imports the subtitle files named after one video, such as
// "Movie.srt" or "Movie.en.vtt" next to "Movie.mp4" (see [SidecarLanguage]).
// The converter writes extracted subtitles with the same naming, so they
// come in here too. Files that cannot be read, parsed or saved are logged
// and skipped; one that cannot be parsed is skipped for the rest of this
// scan and tried again on the next.
//
// Parameters:
//   - [ctx]: cancels the database work.
//   - [mediaID]: the video's media ID.
//   - [videoRelativePath]: the video's path relative to the media folder.
//   - [candidates]: the subtitle files in the video's folder.
//   - [imported]: the sources already imported, as read at the start of
//     this scan; each file imported or found unparseable is added to it.
//
// Returns:
//   - [int]: how many subtitle tracks were added.
func (scanner *Scanner) importSidecars(ctx context.Context, mediaID int64, videoRelativePath string, candidates []string, imported collections.Set[string]) int {
	importedCount := 0
	for _, subtitlePath := range candidates {
		if imported.Contains(subtitlePath) {
			continue
		}
		language, isSidecar := SidecarLanguage(videoRelativePath, subtitlePath)
		if !isSidecar {
			continue
		}
		data, err := os.ReadFile(filepath.Join(scanner.config.MediaDirectory, filepath.FromSlash(subtitlePath)))
		if err != nil {
			scanner.dependencies.Log.Warn("could not read subtitle", "path", subtitlePath, "err", err)
			continue
		}
		cues, err := subtitle.Parse(data)
		if err != nil {
			scanner.dependencies.Log.Warn("could not parse subtitle", "path", subtitlePath, "err", err)
			imported.Add(subtitlePath) // skip it for the rest of this scan
			continue
		}
		_, inserted, err := scanner.dependencies.Store.AddSubtitle(ctx, store.NewSubtitle{
			MediaID: mediaID, Language: language, Label: subtitle.LanguageName(language), Source: subtitlePath, Cues: cues,
		})
		if err != nil {
			scanner.dependencies.Log.Warn("could not save subtitle", "path", subtitlePath, "err", err)
			continue
		}
		imported.Add(subtitlePath)
		if inserted {
			importedCount++
		}
	}
	return importedCount
}

// [SidecarLanguage] works out whether a subtitle file belongs to a video, and
// its language: "Movie.srt" is "und", "Movie.en.srt" is "en" and
// "Movie.forced.en.srt" is "en".
//
// Parameters:
//   - [videoRelativePath]: the video's path, such as "Movies/Movie.mp4".
//   - [subtitleRelativePath]: the subtitle file's path, such as
//     "Movies/Movie.en.srt".
//
// Returns:
//   - [string] ([language]): the lowercased last dot-separated part between
//     the video's name and the extension, "und" if there is none, or "" if
//     the file is not the video's.
//   - [bool] ([isSidecar]): [true] if the subtitle file is in the same
//     folder and named after the video, [false] otherwise.
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

// [backfillLimit] caps how many files one scan re-probes, so the first scan
// after upgrading a large library stays short; later scans do the rest.
// Files whose re-probe fails or finds no pixel format count against the cap
// once, then are skipped until the program restarts (see
// [Scanner.backfillSkipped]), so they cannot hold up the files after them.
const backfillLimit = 50

// [backfillEpisodeEnd] records the episode range of multi-episode files that
// were scanned before the range was stored (see [store.Store.SetEpisodeEnd]).
// Failures are logged.
//
// Parameters:
//   - [ctx]: cancels the database work.
//   - [videos]: the video files the walk found.
//   - [known]: every known path mapped to its media ID.
func (scanner *Scanner) backfillEpisodeEnd(ctx context.Context, videos []videoFile, known map[string]int64) {
	for _, video := range videos {
		id, isKnown := known[video.relativePath]
		if !isKnown {
			continue
		}
		if guess := ParsePath(video.relativePath); guess.EpisodeEnd > 0 {
			if err := scanner.dependencies.Store.SetEpisodeEnd(ctx, id, guess.Episode, guess.EpisodeEnd); err != nil {
				scanner.dependencies.Log.Warn("recording an episode range", "path", video.relativePath, "err", err)
			}
		}
	}
}

// [backfillPixelFormat] records the pixel format of H.264 files added before
// it was stored, re-probing at most [backfillLimit] of them per scan and
// skipping files whose re-probe already failed or found no pixel format.
// Without it, a 10-bit H.264 file (which no browser plays) would be served
// as it is and never converted. Failures are logged.
//
// Parameters:
//   - [ctx]: cancels the probes and database work.
//   - [videos]: the video files the walk found.
func (scanner *Scanner) backfillPixelFormat(ctx context.Context, videos []videoFile) {
	missing, err := scanner.dependencies.Store.H264MissingPixelFormat(ctx)
	if err != nil {
		scanner.dependencies.Log.Warn("could not list media to re-probe", "err", err)
		return
	}
	done := 0
	for _, scannedVideo := range videos {
		id, isMissing := missing[scannedVideo.relativePath]
		if !isMissing || done == backfillLimit || scanner.backfillSkipped.Contains(scannedVideo.relativePath) {
			continue
		}
		done++
		probeContext, cancel := context.WithTimeout(ctx, time.Minute)
		probe, err := scanner.dependencies.Prober.Probe(probeContext, scannedVideo.absolutePath)
		cancel()
		if err != nil {
			scanner.dependencies.Log.Warn("could not re-probe media", "path", scannedVideo.relativePath, "err", err)
			scanner.backfillSkipped.Add(scannedVideo.relativePath)
			continue
		}
		video, hasVideo := probe.Video()
		if !hasVideo || video.PixelFormat == "" {
			scanner.backfillSkipped.Add(scannedVideo.relativePath)
			continue
		}
		if err := scanner.dependencies.Store.SetPixelFormat(ctx, id, video.PixelFormat); err != nil {
			scanner.dependencies.Log.Warn("could not record pixel format", "path", scannedVideo.relativePath, "err", err)
		}
	}
}

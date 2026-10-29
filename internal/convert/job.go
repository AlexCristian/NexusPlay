package convert

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

const (
	// temporarySuffix names the hidden file an MP4 is written to before the swap.
	// The leading dot keeps the scanner away from it.
	temporarySuffix = ".nexusplay-converting.mp4"
	// spaceMargin is kept free on top of the source file's size.
	spaceMargin = 256 << 20
	// noSpaceRetry is how long a file waits after a disk-space check fails.
	noSpaceRetry = 30 * time.Minute
)

// Errors that mean "not now" rather than "this file cannot be converted".
var (
	// ErrNoSpace means the disk has too little free space; the file is
	// postponed, not marked failed.
	ErrNoSpace = errors.New("not enough free disk space")
	// ErrTargetExists means a file already sits at the MP4's name, for
	// example one the user added while the conversion ran. It is never
	// overwritten.
	ErrTargetExists = errors.New("a file with the converted file's name already exists")
	// ErrSourceChanged means the media row no longer points at the file that
	// was converted (a rescan replaced or removed it).
	ErrSourceChanged = errors.New("the file changed in the library while it was being converted")
)

// work is everything one conversion needs once it is planned.
type work struct {
	sourcePath    string // the original
	directory     string // its folder; every file below lives here
	target        string // the MP4's final name
	temporaryPath string // where ffmpeg writes the MP4
	source        ffmpeg.Probe
	plan          Plan
}

// convert turns one file into an MP4 that replaces it, and returns the new
// path. The steps are: prepare (checks and plan), extract subtitles, encode,
// verify, commit (swap the files and the database row).
func (converter *Converter) convert(ctx context.Context, job *job) (string, error) {
	work, err := converter.prepare(ctx, job)
	if err != nil {
		return "", err
	}
	converter.extractSubtitles(ctx, job.media.Path, work)
	defer os.Remove(work.temporaryPath) // only matters if we stop before the swap
	err = converter.deps.Tools.Run(ctx, work.plan.ConvertArguments(work.sourcePath, work.temporaryPath), ffmpeg.RunOptions{
		LowPriority: true,
		OnProgress:  func(seconds float64) { converter.setProgress(job, seconds) },
	})
	if err != nil {
		return "", err
	}
	result, err := converter.verify(ctx, work)
	if err != nil {
		return "", fmt.Errorf("the converted file failed its checks: %w", err)
	}
	relativePath, err := filepath.Rel(converter.config.MediaDirectory, work.target)
	if err != nil {
		return "", err
	}
	result.Path = filepath.ToSlash(relativePath)
	return result.Path, converter.commit(ctx, job.media, work, result)
}

// prepare checks there is room, probes the original and plans the
// conversion. It refuses early if a file already has the MP4's name (commit
// checks again, since the encode can take hours).
func (converter *Converter) prepare(ctx context.Context, job *job) (work, error) {
	sourcePath := filepath.Join(converter.config.MediaDirectory, filepath.FromSlash(job.media.Path))
	info, err := os.Stat(sourcePath)
	if err != nil {
		return work{}, fmt.Errorf("original file: %w", err)
	}
	directory := filepath.Dir(sourcePath)
	need := uint64(info.Size()) + spaceMargin
	free, err := converter.config.FreeSpace(directory)
	switch {
	case err != nil:
		// Not knowing is not a reason to stop; ffmpeg fails cleanly if the disk fills.
		converter.deps.Log.Debug("could not check free disk space", "dir", directory, "err", err)
	case free < need:
		return work{}, fmt.Errorf("%w: %d MB free, %d MB needed", ErrNoSpace, free>>20, need>>20)
	}

	pctx, cancel := context.WithTimeout(ctx, time.Minute)
	source, err := converter.deps.Tools.Probe(pctx, sourcePath)
	cancel()
	if err != nil {
		return work{}, err
	}
	converter.setDuration(job, source.Duration)
	base := strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath))
	plan, err := MakePlan(source, base)
	if err != nil {
		return work{}, err
	}
	conversionWork := work{sourcePath: sourcePath, directory: directory, target: filepath.Join(directory, base+".mp4"),
		temporaryPath: filepath.Join(directory, "."+base+temporarySuffix), source: source, plan: plan}
	if conversionWork.target != sourcePath {
		if _, err := os.Lstat(conversionWork.target); err == nil {
			return work{}, fmt.Errorf("%s: %w; rename one of them", filepath.Base(conversionWork.target), ErrTargetExists)
		}
	}
	return conversionWork, nil
}

// extractSubtitles saves the built-in text subtitles next to the video. A
// track that fails is logged and skipped: the video matters more.
func (converter *Converter) extractSubtitles(ctx context.Context, path string, work work) {
	for _, subtitlePlan := range work.plan.Subtitles {
		if err := converter.extractSubtitle(ctx, work.sourcePath, work.directory, subtitlePlan); err != nil {
			converter.deps.Log.Warn("could not extract a subtitle track", "path", path, "file", subtitlePlan.File, "err", err)
		}
	}
	if work.plan.DroppedSubs > 0 {
		converter.deps.Log.Info("image-based subtitles cannot be kept in MP4", "path", path, "tracks", work.plan.DroppedSubs)
	}
}

func (converter *Converter) extractSubtitle(ctx context.Context, source, directory string, subtitlePlan SubtitlePlan) error {
	out := filepath.Join(directory, subtitlePlan.File)
	// Defence in depth on top of safeLanguage: sidecars live next to the video.
	if filepath.Dir(out) != filepath.Clean(directory) {
		return fmt.Errorf("refusing to write subtitle %q outside the video's folder", subtitlePlan.File)
	}
	if _, err := os.Lstat(out); err == nil {
		return nil // keep an existing file of the same name
	}
	temporaryPath := filepath.Join(directory, "."+subtitlePlan.File+".part")
	defer os.Remove(temporaryPath)
	if err := converter.deps.Tools.Run(ctx, SubtitleArguments(source, subtitlePlan, temporaryPath), ffmpeg.RunOptions{}); err != nil {
		return err
	}
	return os.Rename(temporaryPath, out)
}

// verify checks the converted file before anything is deleted and returns
// what the library should record about it.
func (converter *Converter) verify(ctx context.Context, work work) (store.ReplacedFile, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	got, err := converter.deps.Tools.Probe(ctx, work.temporaryPath)
	if err != nil {
		return store.ReplacedFile{}, err
	}
	if err := CheckConverted(got, work.source, work.plan); err != nil {
		return store.ReplacedFile{}, err
	}
	info, err := os.Stat(work.temporaryPath)
	if err != nil {
		return store.ReplacedFile{}, err
	}
	video, _ := got.Video() // CheckConverted made sure there is one
	return store.ReplacedFile{Duration: got.Duration, Container: got.Container,
		VideoCodec: video.Codec, AudioCodec: got.AudioCodec(), PixFmt: video.PixFmt, Size: info.Size()}, nil
}

// CheckConverted compares a converted file with its source and plan: H.264
// video, every planned audio track, and the same length within max(2 s, 1%).
// The original is deleted only when this passes, so it is deliberately strict.
func CheckConverted(got, source ffmpeg.Probe, plan Plan) error {
	if videoCodec := got.VideoCodec(); videoCodec != "h264" {
		return fmt.Errorf("video is %q, want h264", videoCodec)
	}
	if audioCount := got.Count("audio"); audioCount != len(plan.Audio) {
		return fmt.Errorf("%d audio tracks, want %d", audioCount, len(plan.Audio))
	}
	if source.Duration > 0 {
		tolerance := math.Max(2, source.Duration*0.01)
		if math.Abs(got.Duration-source.Duration) > tolerance {
			return fmt.Errorf("lasts %.1fs, the original %.1fs", got.Duration, source.Duration)
		}
	}
	return nil
}

// commit swaps the verified MP4 in while no scan can run.
func (converter *Converter) commit(ctx context.Context, media store.Media, work work, result store.ReplacedFile) error {
	swap := func() error {
		return converter.swap(ctx, media, work.sourcePath, work.temporaryPath, work.target, result)
	}
	if converter.deps.Library == nil {
		return swap()
	}
	return converter.deps.Library.Exclusive(swap)
}

// swap makes the verified MP4 the library's copy of the media. The order of
// the steps is what keeps a file from ever being lost:
//
//  1. Put tmp at target. For a same-name MP4 (wrong codecs inside) this is a
//     rename, which atomically replaces the original, so the file is never
//     missing. For a different name (Movie.mkv -> Movie.mp4) it is a
//     no-overwrite move: if anything appeared at target meanwhile, swap stops
//     with ErrTargetExists and touches nothing.
//  2. Point the media row at target. If this fails and the names differ, the
//     file placed in step 1 (which this job created) is removed and the
//     original is untouched. If the names are the same, the converted file
//     stays; the row keeps the old codecs, the job is recorded as failed, and
//     Retry in the control panel simply remuxes it.
//  3. Delete the original. Failing here only leaves a stray file behind; the
//     library is already correct.
//
// swap runs under the scanner's Exclusive lock, so no scan sees the state
// between steps 1 and 2, and ignores cancellation: a half-finished swap is
// worse than a slow shutdown.
func (converter *Converter) swap(ctx context.Context, media store.Media, source, temporaryPath, target string, result store.ReplacedFile) error {
	ctx = context.WithoutCancel(ctx)
	current, err := converter.deps.Store.MediaByID(ctx, media.ID)
	if err != nil {
		return fmt.Errorf("re-reading media %d: %w", media.ID, err)
	}
	if current.Path != media.Path {
		return ErrSourceChanged
	}
	sameName := target == source
	if sameName {
		if err := os.Rename(temporaryPath, target); err != nil {
			return fmt.Errorf("putting the converted file in place: %w", err)
		}
	} else if err := moveNoReplace(temporaryPath, target); err != nil {
		return err
	}
	if err := converter.deps.Store.ReplaceMediaFile(ctx, media.ID, result); err != nil {
		if !sameName {
			if rmErr := os.Remove(target); rmErr != nil {
				converter.deps.Log.Error("could not remove the converted file after a failed update", "path", target, "err", rmErr)
			}
		}
		return fmt.Errorf("updating the library: %w", err)
	}
	if !sameName {
		if err := os.Remove(source); err != nil {
			converter.deps.Log.Error("could not delete the original; delete it by hand", "path", source, "err", err)
		}
	}
	return nil
}

// moveNoReplace moves tmp to target only if nothing exists at target. A hard
// link fails atomically when target exists, unlike rename, which would
// replace it. On file systems without hard links (FAT, some network shares)
// it falls back to checking first, which is safe here because the caller
// holds the scan lock and the scanner never creates files.
func moveNoReplace(temporaryPath, target string) error {
	err := os.Link(temporaryPath, target)
	switch {
	case err == nil:
		if err := os.Remove(temporaryPath); err != nil {
			return fmt.Errorf("removing the temporary file: %w", err)
		}
		return nil
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("%s: %w; rename one of them", filepath.Base(target), ErrTargetExists)
	}
	if _, statErr := os.Lstat(target); statErr == nil {
		return fmt.Errorf("%s: %w; rename one of them", filepath.Base(target), ErrTargetExists)
	}
	if err := os.Rename(temporaryPath, target); err != nil {
		return fmt.Errorf("putting the converted file in place: %w", err)
	}
	return nil
}

// removeLeftovers deletes half-written files from a conversion that was
// interrupted (a crash or a power cut).
func removeLeftovers(mediaDirectory string) error {
	var errs []error
	walkErr := filepath.WalkDir(mediaDirectory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable folder holds no leftovers we could remove
		}
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".") &&
			(strings.HasSuffix(entry.Name(), temporarySuffix) || strings.HasSuffix(entry.Name(), ".srt.part")) {
			if err := os.Remove(path); err != nil {
				errs = append(errs, err)
			}
		}
		return nil
	})
	return errors.Join(append(errs, walkErr)...)
}

// checkWritable reports whether files can be created in dir.
func checkWritable(directory string) error {
	file, err := os.CreateTemp(directory, ".nexusplay-write-test-*")
	if err != nil {
		return err
	}
	name := file.Name()
	file.Close()
	return os.Remove(name)
}

package convert

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// [convert] turns one file into an MP4 that replaces it. The steps are:
// prepare (checks and plan), extract subtitles, encode, check the result,
// and commit (swap the files and the database row). The partly written MP4
// is deleted if any step before the swap fails. Extracted subtitle files are
// kept even if a later step fails; the next scan imports them for the
// original.
//
// Parameters:
//   - [ctx]: stops the conversion when cancelled; the swap itself ignores it.
//   - [job]: the conversion, whose progress is updated as ffmpeg runs.
//
// Returns:
//   - [string]: the converted file's path relative to the media folder, with
//     forward slashes, such as "Movies/Movie.mp4". It is set as soon as the
//     file has passed its checks, even if the commit then fails, so use it
//     only when [error] is [nil].
//   - [error]: [nil] on success; [ErrNoSpace], [ErrTargetExists] or
//     [ErrSourceChanged] (see their comments); [ffmpeg.ErrNoVideo] for a file
//     with no video; [ctx]'s error if it was cancelled; an error saying the
//     converted file failed its checks; otherwise the ffmpeg, file or
//     database error.
func (converter *Converter) convert(ctx context.Context, job *job) (string, error) {
	work, err := converter.prepare(ctx, job)
	if err != nil {
		return "", err
	}
	converter.extractSubtitles(ctx, job.media.Path, work)
	defer os.Remove(work.temporaryPath) // only matters if we stop before the swap
	err = converter.dependencies.Tools.Run(ctx, work.plan.ConvertArguments(work.sourcePath, work.temporaryPath), ffmpeg.RunOptions{
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

// [prepare] checks there is room, probes the original and plans the
// conversion. It refuses early if another file already has the MP4's name;
// [Converter.swap] checks again, since the encode can take hours. A free
// space check that cannot be made is logged and ignored, since ffmpeg fails
// cleanly if the disk fills.
//
// Parameters:
//   - [ctx]: cancels the probe, which is also limited to one minute.
//   - [job]: the conversion; its length is recorded from the probe.
//
// Returns:
//   - [work]: the paths and plan, or a zero [work] if [error] is not [nil].
//   - [error]: [nil] on success; an error wrapping the file error if the
//     original cannot be read; [ErrNoSpace] if the disk has less than the
//     original's size plus [spaceMargin] free; [ErrTargetExists] if a
//     different file has the MP4's name; [ffmpeg.ErrNoVideo] for a file with
//     no video; otherwise the probe's error.
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
		converter.dependencies.Log.Debug("could not check free disk space", "dir", directory, "err", err)
	case free < need:
		return work{}, fmt.Errorf("%w: %d MB free, %d MB needed", ErrNoSpace, free>>20, need>>20)
	}

	probeContext, cancel := context.WithTimeout(ctx, time.Minute)
	source, err := converter.dependencies.Tools.Probe(probeContext, sourcePath)
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

// [extractSubtitles] saves the original's text subtitles as SRT files next
// to the video. A track that fails is logged and skipped: the video matters
// more. Image-based subtitles, which MP4 cannot hold, are logged as dropped.
//
// Parameters:
//   - [ctx]: stops the extraction when cancelled.
//   - [path]: the media item's path, for log messages.
//   - [work]: the conversion's paths and plan.
func (converter *Converter) extractSubtitles(ctx context.Context, path string, work work) {
	for _, subtitlePlan := range work.plan.Subtitles {
		if err := converter.extractSubtitle(ctx, work.sourcePath, work.directory, subtitlePlan); err != nil {
			converter.dependencies.Log.Warn("could not extract a subtitle track", "path", path, "file", subtitlePlan.File, "err", err)
		}
	}
	if work.plan.DroppedSubtitles > 0 {
		converter.dependencies.Log.Info("image-based subtitles cannot be kept in MP4", "path", path, "tracks", work.plan.DroppedSubtitles)
	}
}

// [extractSubtitle] saves one text subtitle track as an SRT file. An existing
// file of the same name is kept, not overwritten. ffmpeg writes to a hidden
// temporary file, which is renamed into place only once complete.
//
// Parameters:
//   - [ctx]: stops ffmpeg when cancelled.
//   - [source]: the original video's path.
//   - [directory]: the video's folder, where the SRT file goes.
//   - [subtitlePlan]: the track and its file name.
//
// Returns:
//   - [error]: [nil] on success or when the file already exists; an error
//     if [SubtitlePlan.File] would put the file outside [directory] (defence
//     in depth on top of [safeLanguage]); otherwise the ffmpeg or rename
//     error.
func (converter *Converter) extractSubtitle(ctx context.Context, source, directory string, subtitlePlan SubtitlePlan) error {
	outputPath := filepath.Join(directory, subtitlePlan.File)
	// Defence in depth on top of [safeLanguage]: sidecars live next to the video.
	if filepath.Dir(outputPath) != filepath.Clean(directory) {
		return fmt.Errorf("refusing to write subtitle %q outside the video's folder", subtitlePlan.File)
	}
	if _, err := os.Lstat(outputPath); err == nil {
		return nil // keep an existing file of the same name
	}
	temporaryPath := filepath.Join(directory, "."+subtitlePlan.File+".part")
	defer os.Remove(temporaryPath)
	if err := converter.dependencies.Tools.Run(ctx, SubtitleArguments(source, subtitlePlan, temporaryPath), ffmpeg.RunOptions{}); err != nil {
		return err
	}
	return os.Rename(temporaryPath, outputPath)
}

// [verify] checks the converted file before anything is deleted, with
// [CheckConverted], and reads what the library should record about it.
//
// Parameters:
//   - [ctx]: cancels the probe, which is also limited to one minute.
//   - [work]: the conversion's paths and plan.
//
// Returns:
//   - [store.ReplacedFile]: the converted file's details, without its path,
//     or a zero [store.ReplacedFile] if [error] is not [nil].
//   - [error]: [nil] if the file passed; otherwise the probe, check or file
//     error.
func (converter *Converter) verify(ctx context.Context, work work) (store.ReplacedFile, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	got, err := converter.dependencies.Tools.Probe(ctx, work.temporaryPath)
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
	video, _ := got.Video() // [CheckConverted] made sure there is one
	return store.ReplacedFile{Duration: got.Duration, Container: got.Container,
		VideoCodec: video.Codec, AudioCodec: got.AudioCodec(), PixelFormat: video.PixelFormat, Size: info.Size()}, nil
}

// [commit] swaps the checked MP4 in with [Converter.swap] under
// [Library.Exclusive], so no scan adds or removes media rows meanwhile (or
// without the lock when there is no scanner).
//
// Parameters:
//   - [ctx]: passed to the swap, which ignores its cancellation.
//   - [media]: the media item as it was when the conversion started.
//   - [work]: the conversion's paths.
//   - [result]: what to record about the converted file.
//
// Returns:
//   - [error]: [nil] on success, or the swap's error.
func (converter *Converter) commit(ctx context.Context, media store.Media, work work, result store.ReplacedFile) error {
	swap := func() error {
		return converter.swap(ctx, media, work.sourcePath, work.temporaryPath, work.target, result)
	}
	if converter.dependencies.Library == nil {
		return swap()
	}
	return converter.dependencies.Library.Exclusive(swap)
}

// [swap] makes the checked MP4 the library's copy of the media. The order of
// the steps is what keeps a file from ever being lost:
//
//  1. Put [temporaryPath] at [target]. For a same-name MP4 (wrong codecs
//     inside) this is a rename, which atomically replaces the original, so
//     the file is never missing. For a different name (Movie.mkv to
//     Movie.mp4) it is a move that never overwrites: if anything appeared at
//     [target] meanwhile, [swap] stops with [ErrTargetExists] and touches
//     nothing.
//  2. Point the media row at [target]. If this fails and the names differ,
//     the file placed in step 1 (which this job created) is removed and the
//     original is untouched. If the names are the same, the converted file
//     stays; the row keeps the old codecs, the job is recorded as failed,
//     and Retry in the control panel simply remuxes it.
//  3. Delete the original. Failing here only leaves a stray file behind,
//     which is logged; the library is already correct.
//
// [swap] runs under the scanner's reconcile lock, so no scan adds or removes
// rows between steps 1 and 2, and it ignores cancellation: a half-finished swap is worse
// than a slow shutdown.
//
// Parameters:
//   - [ctx]: carries values only; its cancellation is ignored.
//   - [media]: the media item as it was when the conversion started.
//   - [source]: the original's path.
//   - [temporaryPath]: where the checked MP4 was written.
//   - [target]: the MP4's final path.
//   - [result]: what to record about the converted file.
//
// Returns:
//   - [error]: [nil] on success; [ErrSourceChanged] if the media row no
//     longer points at [media]'s path or was removed; [ErrTargetExists] if
//     a file appeared at [target]; an error wrapping the database error if
//     the media item cannot be re-read or updated; otherwise the file error
//     from step 1.
func (converter *Converter) swap(ctx context.Context, media store.Media, source, temporaryPath, target string, result store.ReplacedFile) error {
	ctx = context.WithoutCancel(ctx)
	current, err := converter.dependencies.Store.MediaByID(ctx, media.ID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrSourceChanged // a scan removed the row
	}
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
	if err := converter.dependencies.Store.ReplaceMediaFile(ctx, media.ID, result); err != nil {
		if !sameName {
			if removeErr := os.Remove(target); removeErr != nil {
				converter.dependencies.Log.Error("could not remove the converted file after a failed update", "path", target, "err", removeErr)
			}
		}
		return fmt.Errorf("updating the library: %w", err)
	}
	if !sameName {
		if err := os.Remove(source); err != nil {
			converter.dependencies.Log.Error("could not delete the original; delete it by hand", "path", source, "err", err)
		}
	}
	return nil
}

package convert

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

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

// extractSubtitles saves the built-in text subtitles next to the video. A
// track that fails is logged and skipped: the video matters more.
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

func (converter *Converter) extractSubtitle(ctx context.Context, source, directory string, subtitlePlan SubtitlePlan) error {
	outputPath := filepath.Join(directory, subtitlePlan.File)
	// Defence in depth on top of safeLanguage: sidecars live next to the video.
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

// verify checks the converted file before anything is deleted and returns
// what the library should record about it.
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
	video, _ := got.Video() // CheckConverted made sure there is one
	return store.ReplacedFile{Duration: got.Duration, Container: got.Container,
		VideoCodec: video.Codec, AudioCodec: got.AudioCodec(), PixelFormat: video.PixelFormat, Size: info.Size()}, nil
}

// commit swaps the verified MP4 in while no scan can run.
func (converter *Converter) commit(ctx context.Context, media store.Media, work work, result store.ReplacedFile) error {
	swap := func() error {
		return converter.swap(ctx, media, work.sourcePath, work.temporaryPath, work.target, result)
	}
	if converter.dependencies.Library == nil {
		return swap()
	}
	return converter.dependencies.Library.Exclusive(swap)
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
	current, err := converter.dependencies.Store.MediaByID(ctx, media.ID)
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

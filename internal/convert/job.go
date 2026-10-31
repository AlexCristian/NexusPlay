package convert

import (
	"errors"
	"fmt"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
)

const (
	// [temporarySuffix] ends the name of the hidden file an MP4 is written to
	// before the swap. The leading dot added to the name keeps the scanner away
	// from it.
	temporarySuffix = ".nexusplay-converting.mp4"
	// [spaceMargin] is the free space, in bytes (256 MiB), required on top of
	// the original file's size before a conversion starts.
	spaceMargin = 256 << 20
	// [noSpaceRetry] is how long a file waits after a free space check fails.
	noSpaceRetry = 30 * time.Minute
)

// [work] is everything one conversion needs once it is planned. Every path
// is in the original's folder.
type work struct {
	// [sourcePath] is the original file's path.
	sourcePath string
	// [directory] is the original's folder.
	directory string
	// [target] is the MP4's final path: the original's name with ".mp4".
	target string
	// [temporaryPath] is the hidden file ffmpeg writes the MP4 to.
	temporaryPath string
	// [source] is the original's probe, which the result is checked against.
	source ffmpeg.Probe
	// [plan] is how the original is converted.
	plan Plan
}

// [CheckConverted] compares a converted file with its original and plan. The
// original is deleted only when this passes, so it is deliberately strict.
//
// Parameters:
//   - [got]: the probe of the converted file.
//   - [source]: the probe of the original.
//   - [plan]: the plan the conversion followed.
//
// Returns:
//   - [error]: [nil] if [got] has H.264 video, exactly as many audio tracks
//     as [plan] has, and the same length as [source] within 2 seconds or 1%,
//     whichever is more (the length is not checked when [source]'s is
//     unknown); otherwise an error describing the first difference.
func CheckConverted(got, source ffmpeg.Probe, plan Plan) error {
	if videoCodec := got.VideoCodec(); videoCodec != "h264" {
		return fmt.Errorf("video is %q, want h264", videoCodec)
	}
	if audioCount := got.Count("audio"); audioCount != len(plan.Audio) {
		return fmt.Errorf("%d audio tracks, want %d", audioCount, len(plan.Audio))
	}
	if source.Duration > 0 {
		tolerance := max(2, source.Duration*0.01)
		if math.Abs(got.Duration-source.Duration) > tolerance {
			return fmt.Errorf("lasts %.1fs, the original %.1fs", got.Duration, source.Duration)
		}
	}
	return nil
}

// [moveNoReplace] moves a file to a path only if nothing exists there. A hard
// link fails atomically when the target exists, unlike a rename, which would
// replace it. On file systems without hard links (FAT, some network shares)
// it falls back to checking first and then renaming, which can still replace
// a file the user creates in between; the reconcile lock only keeps the
// scanner out.
//
// Parameters:
//   - [temporaryPath]: the file to move.
//   - [target]: where to move it.
//
// Returns:
//   - [error]: [nil] on success; [ErrTargetExists] (wrapped with the file's
//     name) if something exists at [target]; an error wrapping the rename
//     error; or an error wrapping the remove error, in which case the file
//     is already at [target] and a copy remains at [temporaryPath].
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

// [removeLeftovers] deletes half-written files left in the media folder by a
// conversion that was interrupted, such as by a crash or a power cut: hidden
// files ending in [temporarySuffix] or ".srt.part". Unreadable folders are
// skipped.
//
// Parameters:
//   - [mediaDirectory]: the media folder, searched with all its subfolders.
//
// Returns:
//   - [error]: [nil] if every leftover found was deleted, otherwise the
//     deletion errors joined with [errors.Join]. Folders that cannot be
//     read, including [mediaDirectory] itself, are skipped silently.
func removeLeftovers(mediaDirectory string) error {
	var problems []error
	walkErr := filepath.WalkDir(mediaDirectory, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable folder holds no leftovers we could remove
		}
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".") &&
			(strings.HasSuffix(entry.Name(), temporarySuffix) || strings.HasSuffix(entry.Name(), ".srt.part")) {
			if err := os.Remove(path); err != nil {
				problems = append(problems, err)
			}
		}
		return nil
	})
	return errors.Join(append(problems, walkErr)...)
}

// [checkWritable] checks that files can be created and deleted in a folder,
// by creating and deleting a hidden test file.
//
// Parameters:
//   - [directory]: the folder to check.
//
// Returns:
//   - [error]: [nil] if the folder is writable, or the error from creating
//     or deleting the file.
func checkWritable(directory string) error {
	file, err := os.CreateTemp(directory, ".nexusplay-write-test-*")
	if err != nil {
		return err
	}
	name := file.Name()
	file.Close()
	return os.Remove(name)
}

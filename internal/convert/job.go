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

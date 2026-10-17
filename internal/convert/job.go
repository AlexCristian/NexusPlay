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
	// tmpSuffix names the hidden file an MP4 is written to before the swap.
	// The leading dot keeps the scanner away from it.
	tmpSuffix = ".nexusplay-converting.mp4"
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
	src    string // the original
	dir    string // its folder; every file below lives here
	target string // the MP4's final name
	tmp    string // where ffmpeg writes the MP4
	source ffmpeg.Probe
	plan   Plan
}

// convert turns one file into an MP4 that replaces it, and returns the new
// path. The steps are: prepare (checks and plan), extract subtitles, encode,
// verify, commit (swap the files and the database row).
func (c *Converter) convert(ctx context.Context, j *job) (string, error) {
	w, err := c.prepare(ctx, j)
	if err != nil {
		return "", err
	}
	c.extractSubtitles(ctx, j.media.Path, w)
	defer os.Remove(w.tmp) // only matters if we stop before the swap
	err = c.deps.Tools.Run(ctx, w.plan.ConvertArgs(w.src, w.tmp), ffmpeg.RunOptions{
		LowPriority: true,
		OnProgress:  func(seconds float64) { c.setProgress(j, seconds) },
	})
	if err != nil {
		return "", err
	}
	result, err := c.verify(ctx, w)
	if err != nil {
		return "", fmt.Errorf("the converted file failed its checks: %w", err)
	}
	rel, err := filepath.Rel(c.cfg.MediaDir, w.target)
	if err != nil {
		return "", err
	}
	result.Path = filepath.ToSlash(rel)
	return result.Path, c.commit(ctx, j.media, w, result)
}

// prepare checks there is room, probes the original and plans the
// conversion. It refuses early if a file already has the MP4's name (commit
// checks again, since the encode can take hours).
func (c *Converter) prepare(ctx context.Context, j *job) (work, error) {
	src := filepath.Join(c.cfg.MediaDir, filepath.FromSlash(j.media.Path))
	info, err := os.Stat(src)
	if err != nil {
		return work{}, fmt.Errorf("original file: %w", err)
	}
	dir := filepath.Dir(src)
	need := uint64(info.Size()) + spaceMargin
	free, err := c.cfg.FreeSpace(dir)
	switch {
	case err != nil:
		// Not knowing is not a reason to stop; ffmpeg fails cleanly if the disk fills.
		c.deps.Log.Debug("could not check free disk space", "dir", dir, "err", err)
	case free < need:
		return work{}, fmt.Errorf("%w: %d MB free, %d MB needed", ErrNoSpace, free>>20, need>>20)
	}

	pctx, cancel := context.WithTimeout(ctx, time.Minute)
	source, err := c.deps.Tools.Probe(pctx, src)
	cancel()
	if err != nil {
		return work{}, err
	}
	c.setDuration(j, source.Duration)
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	plan, err := MakePlan(source, base)
	if err != nil {
		return work{}, err
	}
	w := work{src: src, dir: dir, target: filepath.Join(dir, base+".mp4"),
		tmp: filepath.Join(dir, "."+base+tmpSuffix), source: source, plan: plan}
	if w.target != src {
		if _, err := os.Lstat(w.target); err == nil {
			return work{}, fmt.Errorf("%s: %w; rename one of them", filepath.Base(w.target), ErrTargetExists)
		}
	}
	return w, nil
}

// extractSubtitles saves the built-in text subtitles next to the video. A
// track that fails is logged and skipped: the video matters more.
func (c *Converter) extractSubtitles(ctx context.Context, path string, w work) {
	for _, sp := range w.plan.Subtitles {
		if err := c.extractSubtitle(ctx, w.src, w.dir, sp); err != nil {
			c.deps.Log.Warn("could not extract a subtitle track", "path", path, "file", sp.File, "err", err)
		}
	}
	if w.plan.DroppedSubs > 0 {
		c.deps.Log.Info("image-based subtitles cannot be kept in MP4", "path", path, "tracks", w.plan.DroppedSubs)
	}
}

func (c *Converter) extractSubtitle(ctx context.Context, src, dir string, sp SubtitlePlan) error {
	out := filepath.Join(dir, sp.File)
	// Defence in depth on top of safeLanguage: sidecars live next to the video.
	if filepath.Dir(out) != filepath.Clean(dir) {
		return fmt.Errorf("refusing to write subtitle %q outside the video's folder", sp.File)
	}
	if _, err := os.Lstat(out); err == nil {
		return nil // keep an existing file of the same name
	}
	tmp := filepath.Join(dir, "."+sp.File+".part")
	defer os.Remove(tmp)
	if err := c.deps.Tools.Run(ctx, SubtitleArgs(src, sp, tmp), ffmpeg.RunOptions{}); err != nil {
		return err
	}
	return os.Rename(tmp, out)
}

// verify checks the converted file before anything is deleted and returns
// what the library should record about it.
func (c *Converter) verify(ctx context.Context, w work) (store.ReplacedFile, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	got, err := c.deps.Tools.Probe(ctx, w.tmp)
	if err != nil {
		return store.ReplacedFile{}, err
	}
	if err := CheckConverted(got, w.source, w.plan); err != nil {
		return store.ReplacedFile{}, err
	}
	info, err := os.Stat(w.tmp)
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
	if v := got.VideoCodec(); v != "h264" {
		return fmt.Errorf("video is %q, want h264", v)
	}
	if n := got.Count("audio"); n != len(plan.Audio) {
		return fmt.Errorf("%d audio tracks, want %d", n, len(plan.Audio))
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
func (c *Converter) commit(ctx context.Context, m store.Media, w work, result store.ReplacedFile) error {
	swap := func() error { return c.swap(ctx, m, w.src, w.tmp, w.target, result) }
	if c.deps.Library == nil {
		return swap()
	}
	return c.deps.Library.Exclusive(swap)
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
func (c *Converter) swap(ctx context.Context, m store.Media, src, tmp, target string, result store.ReplacedFile) error {
	ctx = context.WithoutCancel(ctx)
	cur, err := c.deps.Store.MediaByID(ctx, m.ID)
	if err != nil {
		return fmt.Errorf("re-reading media %d: %w", m.ID, err)
	}
	if cur.Path != m.Path {
		return ErrSourceChanged
	}
	sameName := target == src
	if sameName {
		if err := os.Rename(tmp, target); err != nil {
			return fmt.Errorf("putting the converted file in place: %w", err)
		}
	} else if err := moveNoReplace(tmp, target); err != nil {
		return err
	}
	if err := c.deps.Store.ReplaceMediaFile(ctx, m.ID, result); err != nil {
		if !sameName {
			if rmErr := os.Remove(target); rmErr != nil {
				c.deps.Log.Error("could not remove the converted file after a failed update", "path", target, "err", rmErr)
			}
		}
		return fmt.Errorf("updating the library: %w", err)
	}
	if !sameName {
		if err := os.Remove(src); err != nil {
			c.deps.Log.Error("could not delete the original; delete it by hand", "path", src, "err", err)
		}
	}
	return nil
}

// moveNoReplace moves tmp to target only if nothing exists at target. A hard
// link fails atomically when target exists, unlike rename, which would
// replace it. On file systems without hard links (FAT, some network shares)
// it falls back to checking first, which is safe here because the caller
// holds the scan lock and the scanner never creates files.
func moveNoReplace(tmp, target string) error {
	err := os.Link(tmp, target)
	switch {
	case err == nil:
		if err := os.Remove(tmp); err != nil {
			return fmt.Errorf("removing the temporary file: %w", err)
		}
		return nil
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("%s: %w; rename one of them", filepath.Base(target), ErrTargetExists)
	}
	if _, statErr := os.Lstat(target); statErr == nil {
		return fmt.Errorf("%s: %w; rename one of them", filepath.Base(target), ErrTargetExists)
	}
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("putting the converted file in place: %w", err)
	}
	return nil
}

// removeLeftovers deletes half-written files from a conversion that was
// interrupted (a crash or a power cut).
func removeLeftovers(mediaDir string) error {
	var errs []error
	walkErr := filepath.WalkDir(mediaDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // an unreadable folder holds no leftovers we could remove
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), ".") &&
			(strings.HasSuffix(d.Name(), tmpSuffix) || strings.HasSuffix(d.Name(), ".srt.part")) {
			if err := os.Remove(p); err != nil {
				errs = append(errs, err)
			}
		}
		return nil
	})
	return errors.Join(append(errs, walkErr)...)
}

// checkWritable reports whether files can be created in dir.
func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".nexusplay-write-test-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

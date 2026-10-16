package convert

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AlexCristian/NexusPlay/internal/library"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

const (
	tmpSuffix = ".nexusplay-converting.mp4"
	// spaceMargin is kept free on top of the source file's size.
	spaceMargin = 256 << 20
	// noSpaceRetry is how long a file waits after a disk-space check fails.
	noSpaceRetry = 30 * time.Minute
)

// Converter works through the queue of files that need converting, at most
// Max at a time, newest first, and deletes each original once its MP4 is
// verified and in the library. Conversions run at low CPU priority so
// playback stays smooth.
type Converter struct {
	Store    *store.Store
	Scanner  *library.Scanner
	FFmpeg   string
	FFprobe  string
	MediaDir string
	Max      int
	Log      *slog.Logger

	wake      chan struct{}
	mu        sync.Mutex
	active    map[int64]*job
	waitUntil map[int64]time.Time
	disabled  string
	now       func() time.Time
}

type job struct {
	media    store.Media
	duration float64
	done     float64 // seconds converted so far
}

// JobStatus describes a conversion in progress.
type JobStatus struct {
	Media   store.Media
	Percent int
}

// Status is a snapshot for the control panel.
type Status struct {
	Disabled string // why conversions are off, if they are
	Active   []JobStatus
	Queued   int
}

func (c *Converter) init() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.wake == nil {
		c.wake = make(chan struct{}, 1)
		c.active = map[int64]*job{}
		c.waitUntil = map[int64]time.Time{}
	}
	if c.now == nil {
		c.now = time.Now
	}
	if c.Log == nil {
		c.Log = slog.Default()
	}
	if c.Max < 1 {
		c.Max = 1
	}
}

// Wake asks the converter to look at the queue now (after a scan found new files).
func (c *Converter) Wake() {
	c.init()
	select {
	case c.wake <- struct{}{}:
	default:
	}
}

// Run processes the queue until ctx ends.
func (c *Converter) Run(ctx context.Context) {
	c.init()
	if err := checkWritable(c.MediaDir); err != nil {
		c.mu.Lock()
		c.disabled = fmt.Sprintf("Conversion is off: the media folder is read-only or not writable by this "+
			"server's user (UID %d), so originals cannot be replaced. In Docker, mount it without :ro and make "+
			"sure that user owns it, or set user: in docker-compose.yml to the folder's owner.", os.Getuid())
		c.mu.Unlock()
		c.Log.Warn("automatic conversion disabled", "reason", err)
		return
	}
	c.removeLeftovers()

	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		c.fill(ctx, &wg)
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		case <-tick.C:
		}
	}
}

// fill starts conversions until Max are running or the queue is empty.
func (c *Converter) fill(ctx context.Context, wg *sync.WaitGroup) {
	c.mu.Lock()
	free := c.Max - len(c.active)
	c.mu.Unlock()
	if free <= 0 {
		return
	}
	queue, err := c.queue(ctx)
	if err != nil {
		if ctx.Err() == nil {
			c.Log.Error("reading the conversion queue", "err", err)
		}
		return
	}
	for _, m := range queue {
		if free == 0 {
			return
		}
		c.mu.Lock()
		j := &job{media: m, duration: m.Duration}
		c.active[m.ID] = j
		c.mu.Unlock()
		free--
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.runJob(ctx, j)
			c.mu.Lock()
			delete(c.active, j.media.ID)
			c.mu.Unlock()
			c.Wake()
		}()
	}
}

// queue lists files waiting for conversion, newest first, skipping running jobs.
func (c *Converter) queue(ctx context.Context) ([]store.Media, error) {
	cands, err := c.Store.ConversionCandidates(ctx)
	if err != nil {
		return nil, err
	}
	now := c.now()
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []store.Media
	for _, m := range cands {
		if c.active[m.ID] != nil || now.Before(c.waitUntil[m.ID]) {
			continue
		}
		if NeedsConversion(m.Path, m.VideoCodec, m.AudioCodec) {
			out = append(out, m)
		}
	}
	return out, nil
}

// Status reports what the converter is doing.
func (c *Converter) Status(ctx context.Context) (Status, error) {
	c.init()
	c.mu.Lock()
	st := Status{Disabled: c.disabled}
	for _, j := range c.active {
		pct := 0
		if j.duration > 0 {
			pct = int(math.Min(99, j.done/j.duration*100))
		}
		st.Active = append(st.Active, JobStatus{Media: j.media, Percent: pct})
	}
	c.mu.Unlock()
	if st.Disabled != "" {
		return st, nil
	}
	q, err := c.queue(ctx)
	st.Queued = len(q)
	return st, err
}

var errNoSpace = errors.New("not enough free disk space")

func (c *Converter) runJob(ctx context.Context, j *job) {
	m := j.media
	start := c.now()
	c.Log.Info("converting", "media", m.ID, "path", m.Path)
	newPath, err := c.convert(ctx, j)
	switch {
	case ctx.Err() != nil:
		c.Log.Info("conversion stopped by shutdown; it will restart", "path", m.Path)
	case errors.Is(err, errNoSpace):
		c.mu.Lock()
		c.waitUntil[m.ID] = c.now().Add(noSpaceRetry)
		c.mu.Unlock()
		c.Log.Warn("conversion postponed", "path", m.Path, "err", err)
	case err != nil:
		c.Log.Error("conversion failed", "path", m.Path, "err", err)
		if err := c.Store.SetConvertFailed(context.WithoutCancel(ctx), m.ID, err.Error()); err != nil {
			c.Log.Error("recording a failed conversion", "err", err)
		}
	default:
		c.Log.Info("converted", "media", m.ID, "from", m.Path, "to", newPath, "took", c.now().Sub(start).Round(time.Second))
		if c.Scanner != nil {
			c.Scanner.Trigger() // picks up extracted subtitles
		}
	}
}

// convert writes the MP4 next to the original, checks it, and swaps it in.
func (c *Converter) convert(ctx context.Context, j *job) (string, error) {
	m := j.media
	src := filepath.Join(c.MediaDir, filepath.FromSlash(m.Path))
	info, err := os.Stat(src)
	if err != nil {
		return "", fmt.Errorf("original file: %w", err)
	}
	dir := filepath.Dir(src)
	if free, ok := freeBytes(dir); ok && free < uint64(info.Size())+spaceMargin {
		return "", fmt.Errorf("%w: %d MB free, %d MB needed", errNoSpace, free>>20, (uint64(info.Size())+spaceMargin)>>20)
	}

	pctx, cancel := context.WithTimeout(ctx, time.Minute)
	source, err := ProbeSource(pctx, c.FFprobe, src)
	cancel()
	if err != nil {
		return "", err
	}
	if source.Duration > 0 {
		c.mu.Lock()
		j.duration = source.Duration
		c.mu.Unlock()
	}
	base := stripExt(filepath.Base(src))
	plan, err := MakePlan(source, base)
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, base+".mp4")
	if target != src {
		if _, err := os.Lstat(target); err == nil {
			return "", fmt.Errorf("%s already exists next to the original; rename one of them", filepath.Base(target))
		}
	}

	for _, sp := range plan.Subtitles {
		if err := c.extractSubtitle(ctx, src, dir, sp); err != nil {
			c.Log.Warn("could not extract a subtitle track", "path", m.Path, "file", sp.File, "err", err)
		}
	}
	if plan.DroppedSubs > 0 {
		c.Log.Info("image-based subtitles cannot be kept in MP4", "path", m.Path, "tracks", plan.DroppedSubs)
	}

	tmp := filepath.Join(dir, "."+base+tmpSuffix)
	defer os.Remove(tmp) // only matters if we stop before the rename
	if err := c.runFFmpeg(ctx, j, plan.ConvertArgs(src, tmp)); err != nil {
		return "", err
	}
	result, err := c.verify(ctx, tmp, source, plan)
	if err != nil {
		return "", fmt.Errorf("the converted file failed its checks: %w", err)
	}

	rel, err := filepath.Rel(c.MediaDir, target)
	if err != nil {
		return "", err
	}
	result.Path = filepath.ToSlash(rel)
	err = c.exclusive(func() error { return c.swap(ctx, m, src, tmp, target, result) })
	return result.Path, err
}

func (c *Converter) exclusive(fn func() error) error {
	if c.Scanner == nil {
		return fn()
	}
	return c.Scanner.Exclusive(fn)
}

// swap puts the converted file in place, updates the database and deletes
// the original. Until the database update succeeds, the original is untouched
// (or, for a same-name MP4, replaced only by an already verified file).
func (c *Converter) swap(ctx context.Context, m store.Media, src, tmp, target string, result store.ReplacedFile) error {
	ctx = context.WithoutCancel(ctx) // a half-finished swap is worse than a slow shutdown
	cur, err := c.Store.MediaByID(ctx, m.ID)
	if err != nil || cur.Path != m.Path {
		return fmt.Errorf("the file changed in the library while it was being converted")
	}
	// rename replaces an existing file atomically, so a same-name MP4 is
	// never missing, even for a moment.
	if err := os.Rename(tmp, target); err != nil {
		return fmt.Errorf("putting the converted file in place: %w", err)
	}
	if err := c.Store.ReplaceMediaFile(ctx, m.ID, result); err != nil {
		if target != src {
			os.Remove(target) // the original is still there; try again later
		}
		// For a same-name file the converted MP4 stays; the row keeps the old
		// codecs, so the file is checked again and simply remuxed.
		return fmt.Errorf("updating the library: %w", err)
	}
	if target != src {
		if err := os.Remove(src); err != nil {
			// The MP4 is in place and in the library; only the original lingers.
			c.Log.Error("could not delete the original; delete it by hand", "path", src, "err", err)
		}
	}
	return nil
}

func (c *Converter) runFFmpeg(ctx context.Context, j *job, args []string) error {
	cmd := exec.CommandContext(ctx, c.FFmpeg, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting ffmpeg: %w", err)
	}
	lowerPriority(cmd.Process.Pid)
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		// -progress reports out_time_us (out_time_ms is also microseconds, despite its name).
		key, val, ok := strings.Cut(sc.Text(), "=")
		if !ok || (key != "out_time_us" && key != "out_time_ms") {
			continue
		}
		if us, err := strconv.ParseInt(val, 10, 64); err == nil && us > 0 {
			c.mu.Lock()
			j.done = float64(us) / 1e6
			c.mu.Unlock()
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg: %v: %s", err, lastLine(stderr.String()))
	}
	return nil
}

func (c *Converter) extractSubtitle(ctx context.Context, src, dir string, sp SubtitlePlan) error {
	out := filepath.Join(dir, sp.File)
	if _, err := os.Lstat(out); err == nil {
		return nil // keep an existing file of the same name
	}
	tmp := filepath.Join(dir, "."+sp.File+".part")
	defer os.Remove(tmp)
	cmd := exec.CommandContext(ctx, c.FFmpeg, SubtitleArgs(src, sp, tmp)...)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%v: %s", err, lastLine(string(b)))
	}
	return os.Rename(tmp, out)
}

// verify checks the converted file before anything is deleted: H.264 video,
// every audio track present, and the same length as the original.
func (c *Converter) verify(ctx context.Context, file string, source Source, plan Plan) (store.ReplacedFile, error) {
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	got, err := library.Prober{Path: c.FFprobe}.Probe(ctx, file)
	if err != nil {
		return store.ReplacedFile{}, err
	}
	if got.VideoCodec != "h264" {
		return store.ReplacedFile{}, fmt.Errorf("video is %s, want h264", got.VideoCodec)
	}
	streams, err := ProbeSource(ctx, c.FFprobe, file)
	if err != nil {
		return store.ReplacedFile{}, err
	}
	audio := 0
	for _, s := range streams.Streams {
		if s.Type == "audio" {
			audio++
		}
	}
	if audio != len(plan.Audio) {
		return store.ReplacedFile{}, fmt.Errorf("%d audio tracks, want %d", audio, len(plan.Audio))
	}
	if source.Duration > 0 {
		tolerance := math.Max(2, source.Duration*0.01)
		if math.Abs(got.Duration-source.Duration) > tolerance {
			return store.ReplacedFile{}, fmt.Errorf("lasts %.1fs, the original %.1fs", got.Duration, source.Duration)
		}
	}
	info, err := os.Stat(file)
	if err != nil {
		return store.ReplacedFile{}, err
	}
	return store.ReplacedFile{Duration: got.Duration, Container: got.Container,
		VideoCodec: got.VideoCodec, AudioCodec: got.AudioCodec, Size: info.Size()}, nil
}

// removeLeftovers deletes half-written files from a conversion that was
// interrupted (a crash or a power cut).
func (c *Converter) removeLeftovers() {
	filepath.WalkDir(c.MediaDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasPrefix(d.Name(), ".") &&
			(strings.HasSuffix(d.Name(), tmpSuffix) || strings.HasSuffix(d.Name(), ".srt.part")) {
			os.Remove(p)
		}
		return nil
	})
}

func checkWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".nexusplay-write-test-*")
	if err != nil {
		return err
	}
	name := f.Name()
	f.Close()
	return os.Remove(name)
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func stripExt(name string) string { return name[:len(name)-len(filepath.Ext(name))] }

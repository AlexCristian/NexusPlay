package convert

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// fakeTools pretends to be ffprobe and ffmpeg. The original probes as an
// MKV with H.264 video and AC-3 audio; Run writes the output file and the
// converted file probes as `converted`.
type fakeTools struct {
	mu        sync.Mutex
	converted ffmpeg.Probe
	runErr    error
	block     bool // Run waits for cancellation
}

func (f *fakeTools) Probe(ctx context.Context, file string) (ffmpeg.Probe, error) {
	if strings.HasSuffix(file, tmpSuffix) {
		return f.converted, nil
	}
	return ffmpeg.Probe{Duration: 60, Container: "matroska,webm", Streams: []ffmpeg.Stream{
		{Index: 0, Type: "video", Codec: "h264", PixFmt: "yuv420p"},
		{Index: 1, Type: "audio", Codec: "ac3", Channels: 6},
	}}, nil
}

func (f *fakeTools) Run(ctx context.Context, args []string, opt ffmpeg.RunOptions) error {
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	if f.runErr != nil {
		return f.runErr
	}
	if opt.OnProgress != nil {
		opt.OnProgress(30)
	}
	return os.WriteFile(args[len(args)-1], []byte("mp4"), 0o644)
}

var goodMP4 = ffmpeg.Probe{Duration: 60, Container: "mov,mp4", Streams: []ffmpeg.Stream{
	{Type: "video", Codec: "h264", PixFmt: "yuv420p"}, {Type: "audio", Codec: "aac"},
}}

// fakeLibrary records how the converter uses the scanner.
type fakeLibrary struct {
	exclusive, triggers int
}

func (l *fakeLibrary) Exclusive(fn func() error) error { l.exclusive++; return fn() }
func (l *fakeLibrary) Trigger()                        { l.triggers++ }

type jobFixture struct {
	c     *Converter
	st    *store.Store
	lib   *fakeLibrary
	tools *fakeTools
	media store.Media
	dir   string
}

func newJobFixture(t *testing.T, tools *fakeTools, free uint64) jobFixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	st, err := store.Open(ctx, filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	mediaDir := filepath.Join(dir, "media")
	os.MkdirAll(mediaDir, 0o755)
	os.WriteFile(filepath.Join(mediaDir, "Movie.mkv"), []byte("original"), 0o644)
	id, _, err := st.InsertScannedMedia(ctx, store.ScannedMedia{Path: "Movie.mkv", Title: "Movie", VideoCodec: "h264", AudioCodec: "ac3"})
	if err != nil {
		t.Fatal(err)
	}
	m, _ := st.MediaByID(ctx, id)
	lib := &fakeLibrary{}
	c := New(Deps{Store: st, Tools: tools, Library: lib, Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		Config{MediaDir: mediaDir, FreeSpace: func(string) (uint64, error) { return free, nil }})
	return jobFixture{c: c, st: st, lib: lib, tools: tools, media: m, dir: mediaDir}
}

func (f jobFixture) files(t *testing.T) []string {
	t.Helper()
	entries, _ := os.ReadDir(f.dir)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func (f jobFixture) failures(t *testing.T) []store.ConversionFailure {
	t.Helper()
	got, err := f.st.ConversionFailures(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestJobReplacesOriginal(t *testing.T) {
	f := newJobFixture(t, &fakeTools{converted: goodMP4}, 1<<40)
	f.c.runJob(context.Background(), &job{media: f.media})
	if got := f.files(t); len(got) != 1 || got[0] != "Movie.mp4" {
		t.Errorf("files = %v, want only Movie.mp4", got)
	}
	if m, _ := f.st.MediaByID(context.Background(), f.media.ID); m.Path != "Movie.mp4" || m.AudioCodec != "aac" {
		t.Errorf("row = %+v", m)
	}
	if f.lib.exclusive != 1 || f.lib.triggers != 1 {
		t.Errorf("library calls: exclusive=%d triggers=%d, want 1 and 1", f.lib.exclusive, f.lib.triggers)
	}
	if len(f.failures(t)) != 0 {
		t.Error("a successful conversion was recorded as failed")
	}
}

// A full disk postpones the file and touches nothing.
func TestJobWithoutSpaceIsPostponed(t *testing.T) {
	f := newJobFixture(t, &fakeTools{converted: goodMP4}, 1<<20)
	f.c.runJob(context.Background(), &job{media: f.media})
	if len(f.failures(t)) != 0 {
		t.Error("a full disk must not be recorded as a failure")
	}
	st, err := f.c.Status(context.Background())
	if err != nil || st.Postponed != 1 || st.Queued != 0 {
		t.Errorf("status = %+v, %v; want 1 postponed", st, err)
	}
	if got := f.files(t); len(got) != 1 || got[0] != "Movie.mkv" {
		t.Errorf("files = %v", got)
	}
}

// A converted file that fails verification is thrown away; the original stays.
func TestJobVerificationFailureKeepsOriginal(t *testing.T) {
	lost := ffmpeg.Probe{Duration: 60, Streams: []ffmpeg.Stream{{Type: "video", Codec: "h264"}}} // audio lost
	f := newJobFixture(t, &fakeTools{converted: lost}, 1<<40)
	f.c.runJob(context.Background(), &job{media: f.media})
	failed := f.failures(t)
	if len(failed) != 1 || !strings.Contains(failed[0].Error, "audio tracks") {
		t.Errorf("failures = %+v", failed)
	}
	if got := f.files(t); len(got) != 1 || got[0] != "Movie.mkv" {
		t.Errorf("files = %v, want only the original", got)
	}
}

// An ffmpeg failure is recorded with its reason.
func TestJobFFmpegFailureIsRecorded(t *testing.T) {
	runErr := &ffmpeg.ExitError{Program: "ffmpeg", Err: errors.New("exit status 1"), LastLine: "Invalid data found"}
	f := newJobFixture(t, &fakeTools{runErr: runErr}, 1<<40)
	f.c.runJob(context.Background(), &job{media: f.media})
	if failed := f.failures(t); len(failed) != 1 || !strings.Contains(failed[0].Error, "Invalid data found") {
		t.Errorf("failures = %+v", failed)
	}
}

// A shutdown during the encode is not a failure and leaves no temp file.
func TestJobCancelledIsNotRecorded(t *testing.T) {
	f := newJobFixture(t, &fakeTools{block: true}, 1<<40)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { f.c.runJob(ctx, &job{media: f.media}); close(done) }()
	cancel()
	<-done
	if len(f.failures(t)) != 0 {
		t.Error("a shutdown was recorded as a failure")
	}
	if got := f.files(t); len(got) != 1 || got[0] != "Movie.mkv" {
		t.Errorf("files = %v", got)
	}
}

func TestDisabledStatus(t *testing.T) {
	st, err := Disabled{}.Status(context.Background())
	if err != nil || !st.Off {
		t.Errorf("Disabled status = %+v, %v", st, err)
	}
	Disabled{}.Wake() // must not panic or block
}

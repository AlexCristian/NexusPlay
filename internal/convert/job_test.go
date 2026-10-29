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
	"time"

	"github.com/AlexCristian/NexusPlay/internal/ffmpeg"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// fakeTools pretends to be ffprobe and ffmpeg. The original probes as an
// MKV with H.264 video and AC-3 audio; Run writes the output file and the
// converted file probes as `converted`.
type fakeTools struct {
	mutex     sync.Mutex
	converted ffmpeg.Probe
	runErr    error
	block     bool // Run waits for cancellation
}

func (fakeTools *fakeTools) Probe(ctx context.Context, file string) (ffmpeg.Probe, error) {
	if strings.HasSuffix(file, temporarySuffix) {
		return fakeTools.converted, nil
	}
	return ffmpeg.Probe{Duration: 60, Container: "matroska,webm", Streams: []ffmpeg.Stream{
		{Index: 0, Type: "video", Codec: "h264", PixelFormat: "yuv420p"},
		{Index: 1, Type: "audio", Codec: "ac3", Channels: 6},
	}}, nil
}

func (fakeTools *fakeTools) Run(ctx context.Context, arguments []string, options ffmpeg.RunOptions) error {
	if fakeTools.block {
		<-ctx.Done()
		return ctx.Err()
	}
	if fakeTools.runErr != nil {
		return fakeTools.runErr
	}
	if options.OnProgress != nil {
		options.OnProgress(30)
	}
	return os.WriteFile(arguments[len(arguments)-1], []byte("mp4"), 0o644)
}

var goodMP4 = ffmpeg.Probe{Duration: 60, Container: "mov,mp4", Streams: []ffmpeg.Stream{
	{Type: "video", Codec: "h264", PixelFormat: "yuv420p"}, {Type: "audio", Codec: "aac"},
}}

// fakeLibrary records how the converter uses the scanner.
type fakeLibrary struct {
	exclusive, triggers int
}

func (fakeLibrary *fakeLibrary) Exclusive(operation func() error) error {
	fakeLibrary.exclusive++
	return operation()
}
func (fakeLibrary *fakeLibrary) Trigger() { fakeLibrary.triggers++ }

type jobFixture struct {
	converter *Converter
	database  *store.Store
	library   *fakeLibrary
	tools     *fakeTools
	media     store.Media
	directory string
}

func newJobFixture(t *testing.T, tools *fakeTools, free uint64) jobFixture {
	t.Helper()
	ctx := context.Background()
	directory := t.TempDir()
	database, err := store.Open(ctx, filepath.Join(directory, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	mediaDirectory := filepath.Join(directory, "media")
	os.MkdirAll(mediaDirectory, 0o755)
	os.WriteFile(filepath.Join(mediaDirectory, "Movie.mkv"), []byte("original"), 0o644)
	id, _, err := database.InsertScannedMedia(ctx, store.ScannedMedia{Path: "Movie.mkv", Title: "Movie", VideoCodec: "h264", AudioCodec: "ac3"})
	if err != nil {
		t.Fatal(err)
	}
	media, _ := database.MediaByID(ctx, id)
	lib := &fakeLibrary{}
	converter := New(Dependencies{Store: database, Tools: tools, Library: lib, Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		Config{MediaDirectory: mediaDirectory, FreeSpace: func(string) (uint64, error) { return free, nil }})
	return jobFixture{converter: converter, database: database, library: lib, tools: tools, media: media, directory: mediaDirectory}
}

func (jobFixture jobFixture) files(t *testing.T) []string {
	t.Helper()
	entries, _ := os.ReadDir(jobFixture.directory)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func (jobFixture jobFixture) failures(t *testing.T) []store.ConversionFailure {
	t.Helper()
	got, err := jobFixture.database.ConversionFailures(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestJobReplacesOriginal(t *testing.T) {
	jobFixture := newJobFixture(t, &fakeTools{converted: goodMP4}, 1<<40)
	jobFixture.converter.runJob(context.Background(), &job{media: jobFixture.media})
	if got := jobFixture.files(t); len(got) != 1 || got[0] != "Movie.mp4" {
		t.Errorf("files = %v, want only Movie.mp4", got)
	}
	if media, _ := jobFixture.database.MediaByID(context.Background(), jobFixture.media.ID); media.Path != "Movie.mp4" || media.AudioCodec != "aac" {
		t.Errorf("row = %+v", media)
	}
	if jobFixture.library.exclusive != 1 || jobFixture.library.triggers != 1 {
		t.Errorf("library calls: exclusive=%d triggers=%d, want 1 and 1", jobFixture.library.exclusive, jobFixture.library.triggers)
	}
	if len(jobFixture.failures(t)) != 0 {
		t.Error("a successful conversion was recorded as failed")
	}
}

// A full disk postpones the file and touches nothing.
func TestJobWithoutSpaceIsPostponed(t *testing.T) {
	jobFixture := newJobFixture(t, &fakeTools{converted: goodMP4}, 1<<20)
	jobFixture.converter.runJob(context.Background(), &job{media: jobFixture.media})
	if len(jobFixture.failures(t)) != 0 {
		t.Error("a full disk must not be recorded as a failure")
	}
	status, err := jobFixture.converter.Status(context.Background())
	if err != nil || status.Postponed != 1 || status.Queued != 0 {
		t.Errorf("status = %+v, %v; want 1 postponed", status, err)
	}
	if got := jobFixture.files(t); len(got) != 1 || got[0] != "Movie.mkv" {
		t.Errorf("files = %v", got)
	}
}

// A converted file that fails verification is thrown away; the original stays.
func TestJobVerificationFailureKeepsOriginal(t *testing.T) {
	lost := ffmpeg.Probe{Duration: 60, Streams: []ffmpeg.Stream{{Type: "video", Codec: "h264"}}} // audio lost
	jobFixture := newJobFixture(t, &fakeTools{converted: lost}, 1<<40)
	jobFixture.converter.runJob(context.Background(), &job{media: jobFixture.media})
	failed := jobFixture.failures(t)
	if len(failed) != 1 || !strings.Contains(failed[0].Error, "audio tracks") {
		t.Errorf("failures = %+v", failed)
	}
	if got := jobFixture.files(t); len(got) != 1 || got[0] != "Movie.mkv" {
		t.Errorf("files = %v, want only the original", got)
	}
}

// An ffmpeg failure is recorded with its reason.
func TestJobFFmpegFailureIsRecorded(t *testing.T) {
	runErr := &ffmpeg.ExitError{Program: "ffmpeg", Err: errors.New("exit status 1"), LastLine: "Invalid data found"}
	jobFixture := newJobFixture(t, &fakeTools{runErr: runErr}, 1<<40)
	jobFixture.converter.runJob(context.Background(), &job{media: jobFixture.media})
	if failed := jobFixture.failures(t); len(failed) != 1 || !strings.Contains(failed[0].Error, "Invalid data found") {
		t.Errorf("failures = %+v", failed)
	}
}

// A shutdown during the encode is not a failure and leaves no temp file.
func TestJobCancelledIsNotRecorded(t *testing.T) {
	jobFixture := newJobFixture(t, &fakeTools{block: true}, 1<<40)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { jobFixture.converter.runJob(ctx, &job{media: jobFixture.media}); close(done) }()
	cancel()
	<-done
	if len(jobFixture.failures(t)) != 0 {
		t.Error("a shutdown was recorded as a failure")
	}
	if got := jobFixture.files(t); len(got) != 1 || got[0] != "Movie.mkv" {
		t.Errorf("files = %v", got)
	}
}

func TestDisabledStatus(t *testing.T) {
	status, err := Disabled{}.Status(context.Background())
	if err != nil || !status.Off {
		t.Errorf("Disabled status = %+v, %v", status, err)
	}
	Disabled{}.Wake() // must not panic or block
}

// Status counts files that need converting: queued, postponed for disk
// space, and not counting browser-ready files or failed ones.
func TestConversionCounts(t *testing.T) {
	ctx := context.Background()
	jobFixture := newJobFixture(t, &fakeTools{}, 1<<40) // Movie.mkv: H.264 + AC-3, needs converting
	add := func(scannedMedia store.ScannedMedia) int64 {
		t.Helper()
		id, _, err := jobFixture.database.InsertScannedMedia(ctx, scannedMedia)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	add(store.ScannedMedia{Path: "ready.mp4", Title: "Ready", VideoCodec: "h264", AudioCodec: "aac", PixelFormat: "yuv420p"})
	add(store.ScannedMedia{Path: "tenbit.mp4", Title: "10-bit", VideoCodec: "h264", AudioCodec: "aac", PixelFormat: "yuv420p10le"})
	waiting := add(store.ScannedMedia{Path: "big.avi", Title: "Big", VideoCodec: "mpeg4", AudioCodec: "mp3"})
	failed := add(store.ScannedMedia{Path: "broken.avi", Title: "Broken", VideoCodec: "mpeg4", AudioCodec: "mp3"})
	if err := jobFixture.database.SetConvertFailed(ctx, failed, "bad file"); err != nil {
		t.Fatal(err)
	}
	jobFixture.converter.waitUntil[waiting] = jobFixture.converter.now().Add(time.Hour)

	status, err := jobFixture.converter.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if status.Queued != 2 || status.Postponed != 1 || len(status.Active) != 0 {
		t.Errorf("status = %+v, want 2 queued (Movie.mkv, tenbit.mp4) and 1 postponed", status)
	}
}

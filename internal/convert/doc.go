// Package convert turns videos browsers cannot play into MP4 files in the
// background and replaces the originals with them.
//
// It owns the conversion queue, the ffmpeg arguments for a conversion
// (plan.go), verifying the result, and the swap that replaces the original
// (job.go). Which files need converting is media.NeedsConversion's decision,
// not this package's. Subtitle tracks are extracted to sidecar files rather
// than embedded, because corrections are applied to the database copy and
// must not require re-encoding the video.
//
// The swap is the one place NexusPlay deletes a user's file. It never
// overwrites an existing file, verifies the new file before deleting the
// old one, and holds the scanner's lock (Library.Exclusive) so a scan never
// sees the folder half-changed. See swap in job.go.
//
// app starts the Converter; web reads its Status through a small interface;
// Disabled stands in when NEXUSPLAY_CONVERT is off.
package convert

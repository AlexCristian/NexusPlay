// Package [convert] turns videos browsers cannot play into MP4 files in the
// background and replaces the originals with them.
//
// It owns the conversion queue, the ffmpeg arguments for a conversion
// (plan.go), checking the result, and the swap that replaces the original
// (converter_job.go). Which files need converting is
// [media.NeedsConversion]'s decision, not this package's. Subtitle tracks are
// extracted to sidecar files rather than embedded, because corrections are
// applied to the database copy and must not require re-encoding the video.
//
// The swap is the one place NexusPlay deletes a user's file. It never
// overwrites an existing file, checks the new file before deleting the old
// one, and holds the scanner's reconcile lock ([Library.Exclusive]), so no
// scan adds or removes rows between moving the file and updating its row; a
// walk may see both files, but the scan re-checks the disk under the lock
// before acting. See [Converter.swap].
//
// Package app starts the [Converter]; package web reads its [Status] through
// a small interface; [Disabled] stands in when NEXUSPLAY_CONVERT is off.
package convert

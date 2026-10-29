// Package [library] finds media files and keeps the database in step with
// the media folder.
//
// The [Scanner] walks the folder, probes new videos with ffprobe, adds them
// as unpublished media, imports sidecar subtitles and removes rows whose
// files are gone. [ParsePath] guesses a title, year, show, season and episode
// from a file's path; the control panel shows that guess for review.
//
// The scanner never publishes anything and never deletes files. It removes
// database rows only for files it could look for and did not find: rows
// under an unreadable folder, or every row when the folder has no videos at
// all (an unmounted drive), are kept.
//
// The scan lock ([Scanner.Exclusive]) keeps scans from overlapping the
// converter's file swap.
package library

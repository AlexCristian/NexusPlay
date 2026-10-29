package library

// [Result] summarizes one scan.
type Result struct {
	// [Added] counts the new files added as unpublished media.
	Added int
	// [Removed] counts the media removed because their files were gone.
	Removed int
	// [Subtitles] counts the sidecar subtitle files imported.
	Subtitles int
	// [Waiting] counts the new files skipped because they were modified less
	// than [Config.Settle] ago and may still be being written.
	Waiting int
}

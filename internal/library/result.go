package library

import (
	_ "embed"
)

// Result summarizes one scan.
type Result struct {
	Added     int
	Removed   int
	Subtitles int // sidecar subtitle files imported
	Waiting   int // new files skipped because they are still being written
}

package library

import (
	"slices"
	"testing"
)

func TestMissingMediaKeepsUnreadableFolders(t *testing.T) {
	known := map[string]int64{
		"Movies/A.mp4":            1,
		"Movies/B.mp4":            2, // deleted from disk
		"Shows/X/S01E01.mp4":      3, // inside a folder that failed to list
		"Shows/X/Season 2/E1.mp4": 4, // deeper inside it
		"Shows/XY/E1.mp4":         5, // a sibling whose name merely starts the same: deleted
		"Broken.mp4":              6, // a file whose details could not be read
	}
	onDisk := map[string]bool{"Movies/A.mp4": true}
	unreadable := []string{"Shows/X", "Broken.mp4"}

	got := missingMedia(known, onDisk, unreadable)
	slices.Sort(got)
	if want := []int64{2, 5}; !slices.Equal(got, want) {
		t.Errorf("missingMedia = %v, want %v", got, want)
	}
}

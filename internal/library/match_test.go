package library

import (
	"testing"

	"github.com/AlexCristian/NexusPlay/internal/metadata"
)

// TMDB's first season of Star Trek: The Next Generation, which numbers the
// two-part pilot as one episode.
var tngS1 = []metadata.Episode{
	{Number: 1, Info: metadata.Info{Title: "Encounter at Farpoint"}},
	{Number: 2, Info: metadata.Info{Title: "The Naked Now"}},
	{Number: 3, Info: metadata.Info{Title: "Code of Honor"}},
	{Number: 4, Info: metadata.Info{Title: "The Last Outpost"}},
}

func TestEpisodeByTitle(t *testing.T) {
	for _, tc := range []struct {
		title  string
		number int
		want   int // 0: no match
	}{
		{"Encounter at Farpoint Part 1 and 2", 1, 1},
		{"The Naked Now", 3, 2}, // the file counts the pilot as two episodes
		{"Code Of Honor", 4, 3},
		{"code.of.honor", 4, 3},
		{"Last Outpost", 5, 4},
		{"Something Else Entirely", 2, 0},
		{"", 2, 0},
	} {
		got, ok := episodeByTitle(tngS1, tc.title, tc.number)
		if (tc.want == 0) == ok || (ok && got.Number != tc.want) {
			t.Errorf("episodeByTitle(%q, %d) = %d, %v; want %d", tc.title, tc.number, got.Number, ok, tc.want)
		}
	}
}

func TestFileEpisodeTitle(t *testing.T) {
	if got := fileEpisodeTitle(ParsePath("Show/S01E03.mkv")); got != "" {
		t.Errorf("made-up title %q counted as the file's", got)
	}
	if got := fileEpisodeTitle(ParsePath("Show/Show S01E03 The Naked Now.mkv")); got != "The Naked Now" {
		t.Errorf("title = %q", got)
	}
}

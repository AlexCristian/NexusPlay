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
	for _, testCase := range []struct {
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
		got, found := episodeByTitle(tngS1, testCase.title, testCase.number)
		if (testCase.want == 0) == found || (found && got.Number != testCase.want) {
			t.Errorf("episodeByTitle(%q, %d) = %d, %v; want %d", testCase.title, testCase.number, got.Number, found, testCase.want)
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

func TestIgnoredWordsLoaded(t *testing.T) {
	for _, word := range []string{"the", "and", "part"} {
		if !ignoredWords.Contains(word) {
			t.Errorf("ignored_words.txt does not have %q", word)
		}
	}
}

func TestParseWordList(t *testing.T) {
	got := parseWordList("# a comment\n\n  The \nPT\r\n")
	if got.Size() != 2 || !got.Contains("the") || !got.Contains("pt") {
		t.Errorf("parseWordList kept %d words; want the and pt", got.Size())
	}
}

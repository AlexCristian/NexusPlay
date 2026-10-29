package library

import "testing"

func TestParsePath(t *testing.T) {
	tests := []struct {
		path string
		want Guess
	}{
		{"Movies/The Matrix (1999).mp4", Guess{Title: "The Matrix", Year: "1999"}},
		{"The.Matrix.1999.1080p.BluRay.x264.mkv", Guess{Title: "The Matrix", Year: "1999"}},
		{"2001 A Space Odyssey (1968).mkv", Guess{Title: "2001 A Space Odyssey", Year: "1968"}},
		{"Blade Runner 2049.mp4", Guess{Title: "Blade Runner 2049"}}, // a future year is part of the title
		{"Blade Runner 2049 (2017).mkv", Guess{Title: "Blade Runner 2049", Year: "2017"}},
		{"Cosmos.mp4", Guess{Title: "Cosmos"}},
		{"Doctor Who/Season 2/Doctor.Who.S02E03.School.Reunion.720p.mkv",
			Guess{Show: "Doctor Who", Title: "School Reunion", Season: 2, Episode: 3}},
		{"Shows/Star Trek The Next Generation/Season 3/S03E01.mkv",
			Guess{Show: "Star Trek The Next Generation", Title: "Episode 1", Season: 3, Episode: 1}},
		{"House of Cards (2013)/House of Cards - 2x05.mp4",
			Guess{Show: "House of Cards", Year: "2013", Title: "Episode 5", Season: 2, Episode: 5}},
		{"Cosmos (2014)/Season 1/Cosmos S01E01E02 Standing Up in the Milky Way.mp4",
			Guess{Show: "Cosmos", Year: "2014", Title: "Standing Up in the Milky Way", Season: 1, Episode: 1, EpisodeEnd: 2}},
		{"TNG/Season 1/TNG.S01E01-E02.Encounter.at.Farpoint.mkv",
			Guess{Show: "TNG", Title: "Encounter at Farpoint", Season: 1, Episode: 1, EpisodeEnd: 2}},
		{"TNG/Season 1/TNG.S01E01-02.Encounter.at.Farpoint.mkv",
			Guess{Show: "TNG", Title: "Encounter at Farpoint", Season: 1, Episode: 1, EpisodeEnd: 2}},
		{"Show/Season 1/Show.S01E05-720p.mkv",
			Guess{Show: "Show", Title: "Episode 5", Season: 1, Episode: 5}},
		{"Season 1/s01e04.mp4", Guess{Show: "Unknown show", Title: "Episode 4", Season: 1, Episode: 4}},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := ParsePath(tt.path)
			if got != tt.want {
				t.Errorf("ParsePath(%q)\n got  %+v\n want %+v", tt.path, got, tt.want)
			}
		})
	}
}

func TestMatchEpisode(t *testing.T) {
	for name, want := range map[string]episodeMatch{
		"Doctor.Who.S02E03.School.Reunion.720p": {show: "Doctor Who", title: "School Reunion", season: 2, episode: 3},
		"House of Cards - 2x05":                 {show: "House of Cards", season: 2, episode: 5},
		"s01e04":                                {season: 1, episode: 4},
		"Cosmos S01E01E02 Standing Up":          {show: "Cosmos", title: "Standing Up", season: 1, episode: 1, end: 2},
	} {
		got, ok := matchEpisode(name)
		if !ok || got != want {
			t.Errorf("matchEpisode(%q) = %+v, %v; want %+v", name, got, ok, want)
		}
	}
	if _, ok := matchEpisode("The Matrix (1999)"); ok {
		t.Error("a film matched as an episode")
	}
}

func TestShowFromDirs(t *testing.T) {
	for dir, want := range map[string]struct {
		show   string
		season int
	}{
		"Doctor Who/Season 2/":         {"Doctor Who", 2},
		"Shows/Cosmos (2014)/S01/":     {"Cosmos (2014)", 1},
		"Series 3/":                    {"", 3},
		"":                             {"", 0},
		"TV/The.Expanse/Specials/":     {"Specials", 0}, // not a season folder
		"Shows/House of Cards (2013)/": {"House of Cards (2013)", 0},
	} {
		show, season := showFromDirectories(dir)
		if show != want.show || season != want.season {
			t.Errorf("showFromDirs(%q) = %q, %d; want %q, %d", dir, show, season, want.show, want.season)
		}
	}
}

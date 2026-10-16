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
		{"Blade Runner 2049.mp4", Guess{Title: "Blade Runner", Year: "2049"}},
		{"Cosmos.mp4", Guess{Title: "Cosmos"}},
		{"Doctor Who/Season 2/Doctor.Who.S02E03.School.Reunion.720p.mkv",
			Guess{Show: "Doctor Who", Title: "School Reunion", Season: 2, Episode: 3}},
		{"Shows/Star Trek The Next Generation/Season 3/S03E01.mkv",
			Guess{Show: "Star Trek The Next Generation", Title: "Episode 1", Season: 3, Episode: 1}},
		{"House of Cards (2013)/House of Cards - 2x05.mp4",
			Guess{Show: "House of Cards", Year: "2013", Title: "Episode 5", Season: 2, Episode: 5}},
		{"Cosmos (2014)/Season 1/Cosmos S01E01E02 Standing Up in the Milky Way.mp4",
			Guess{Show: "Cosmos", Year: "2014", Title: "Standing Up in the Milky Way", Season: 1, Episode: 1}},
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

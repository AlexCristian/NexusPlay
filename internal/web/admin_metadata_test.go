package web

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/AlexCristian/NexusPlay/internal/metadata"
	"github.com/AlexCristian/NexusPlay/internal/store"
)

// fakeLookup records which TMDB searches ran. Names in shows and films are
// found by those searches; everything else is ErrNoMatch.
type fakeLookup struct {
	shows, collections, films map[string]bool
	calls                     []string
}

func (f *fakeLookup) Enabled() bool { return true }

func (f *fakeLookup) find(kind string, known map[string]bool, name string) (metadata.Info, error) {
	f.calls = append(f.calls, kind)
	if !known[name] {
		return metadata.Info{}, metadata.ErrNoMatch
	}
	return metadata.Info{Title: name, Year: "1993", Poster: kind + ".jpg"}, nil
}

func (f *fakeLookup) Show(_ context.Context, name, _ string) (metadata.Info, error) {
	return f.find("show", f.shows, name)
}
func (f *fakeLookup) Collection(_ context.Context, name string) (metadata.Info, error) {
	return f.find("collection", f.collections, name)
}
func (f *fakeLookup) Movie(_ context.Context, name, _ string) (metadata.Info, error) {
	return f.find("movie", f.films, name)
}

func TestLookupCollection(t *testing.T) {
	f := &fakeLookup{
		shows:       map[string]bool{"Deep Space Nine": true},
		collections: map[string]bool{"Star Trek": true},
		films:       map[string]bool{"Alien": true},
	}
	h := &adminHandlers{responder: responder{log: slog.New(slog.NewTextHandler(io.Discard, nil))}, meta: f}
	for _, tc := range []struct {
		name, category, wantPoster, wantYear string
		wantCalls                            string
	}{
		{"Star Trek", "Movies", "collection.jpg", "1993", "[collection]"},
		{"Alien", "movies", "movie.jpg", "", "[collection movie]"}, // a film's year is not the collection's
		{"Deep Space Nine", "TV Shows", "show.jpg", "1993", "[show]"},
		{"Star Trek", "TV Shows", "collection.jpg", "1993", "[show collection]"},
	} {
		f.calls = nil
		c := store.Collection{Name: tc.name, Category: tc.category}
		h.fillFromTMDB(context.Background(), &c)
		if c.Poster != tc.wantPoster || c.Year != tc.wantYear || fmtCalls(f.calls) != tc.wantCalls {
			t.Errorf("%s (%s): poster %q, year %q, searches %v; want %q, %q, %s",
				tc.name, tc.category, c.Poster, c.Year, f.calls, tc.wantPoster, tc.wantYear, tc.wantCalls)
		}
	}
}

func fmtCalls(calls []string) string {
	s := "["
	for i, c := range calls {
		if i > 0 {
			s += " "
		}
		s += c
	}
	return s + "]"
}

// A new collection never takes an episode's air year; only the show's
// folder year.
func TestShowYearGuess(t *testing.T) {
	for _, tc := range []struct {
		path, collection, want string
	}{
		{"Star Trek Deep Space Nine (1993)/Season 5/Star Trek Deep Space Nine S05E01.mkv", "Star Trek Deep Space Nine", "1993"},
		{"Star Trek Deep Space Nine (1993)/Season 5/S05E01.mkv", "Star Trek Deep Space Nine", "1993"},
		{"Deep Space Nine/Season 5/Deep Space Nine S05E01.mkv", "Deep Space Nine", ""},
		{"Star Trek Deep Space Nine (1993)/Season 5/DS9 S05E01.mkv", "Something Else", ""},
		{"Movies/Alien (1979).mp4", "Alien Films", ""},
	} {
		m := store.Media{Path: tc.path, Year: "1997"}
		if got := showYearGuess(m, tc.collection); got != tc.want {
			t.Errorf("showYearGuess(%q, %q) = %q, want %q", tc.path, tc.collection, got, tc.want)
		}
	}
}

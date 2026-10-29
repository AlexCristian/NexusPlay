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

func (fakeLookup *fakeLookup) Enabled() bool { return true }

func (fakeLookup *fakeLookup) find(kind string, known map[string]bool, name string) (metadata.Info, error) {
	fakeLookup.calls = append(fakeLookup.calls, kind)
	if !known[name] {
		return metadata.Info{}, metadata.ErrNoMatch
	}
	return metadata.Info{Title: name, Year: "1993", Poster: kind + ".jpg"}, nil
}

func (fakeLookup *fakeLookup) Show(_ context.Context, name, _ string) (metadata.Info, error) {
	return fakeLookup.find("show", fakeLookup.shows, name)
}
func (fakeLookup *fakeLookup) Collection(_ context.Context, name string) (metadata.Info, error) {
	return fakeLookup.find("collection", fakeLookup.collections, name)
}
func (fakeLookup *fakeLookup) Movie(_ context.Context, name, _ string) (metadata.Info, error) {
	return fakeLookup.find("movie", fakeLookup.films, name)
}

func TestLookupCollection(t *testing.T) {
	lookup := &fakeLookup{
		shows:       map[string]bool{"Deep Space Nine": true},
		collections: map[string]bool{"Star Trek": true},
		films:       map[string]bool{"Alien": true},
	}
	handlers := &adminHandlers{responder: responder{log: slog.New(slog.NewTextHandler(io.Discard, nil))}, metadata: lookup}
	for _, testCase := range []struct {
		name, category, wantPoster, wantYear string
		wantCalls                            string
	}{
		{"Star Trek", "Movies", "collection.jpg", "1993", "[collection]"},
		{"Alien", "movies", "movie.jpg", "", "[collection movie]"}, // a film's year is not the collection's
		{"Deep Space Nine", "TV Shows", "show.jpg", "1993", "[show]"},
		{"Star Trek", "TV Shows", "collection.jpg", "1993", "[show collection]"},
	} {
		lookup.calls = nil
		collection := store.Collection{Name: testCase.name, Category: testCase.category}
		handlers.fillFromTMDB(context.Background(), &collection)
		if collection.Poster != testCase.wantPoster || collection.Year != testCase.wantYear || formatCalls(lookup.calls) != testCase.wantCalls {
			t.Errorf("%s (%s): poster %q, year %q, searches %v; want %q, %q, %s",
				testCase.name, testCase.category, collection.Poster, collection.Year, lookup.calls, testCase.wantPoster, testCase.wantYear, testCase.wantCalls)
		}
	}
}

func formatCalls(calls []string) string {
	text := "["
	for i, call := range calls {
		if i > 0 {
			text += " "
		}
		text += call
	}
	return text + "]"
}

// A new collection never takes an episode's air year; only the show's
// folder year.
func TestShowYearGuess(t *testing.T) {
	for _, testCase := range []struct {
		path, collection, want string
	}{
		{"Star Trek Deep Space Nine (1993)/Season 5/Star Trek Deep Space Nine S05E01.mkv", "Star Trek Deep Space Nine", "1993"},
		{"Star Trek Deep Space Nine (1993)/Season 5/S05E01.mkv", "Star Trek Deep Space Nine", "1993"},
		{"Deep Space Nine/Season 5/Deep Space Nine S05E01.mkv", "Deep Space Nine", ""},
		{"Star Trek Deep Space Nine (1993)/Season 5/DS9 S05E01.mkv", "Something Else", ""},
		{"Movies/Alien (1979).mp4", "Alien Films", ""},
	} {
		media := store.Media{Path: testCase.path, Year: "1997"}
		if got := showYearGuess(media, testCase.collection); got != testCase.want {
			t.Errorf("showYearGuess(%q, %q) = %q, want %q", testCase.path, testCase.collection, got, testCase.want)
		}
	}
}

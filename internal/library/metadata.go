package library

import (
	"context"

	"github.com/AlexCristian/NexusPlay/internal/metadata"
)

// [Metadata] looks titles up online; [metadata.Client] is the real one. Its
// [metadata.ErrDisabled] and [metadata.ErrNoMatch] are expected and not
// logged as failures.
type Metadata interface {
	// [Movie] looks up a film.
	//
	// Parameters:
	//   - [ctx]: cancels the lookup.
	//   - [title]: the film's title.
	//   - [year]: the release year, or "" for any.
	//
	// Returns:
	//   - [metadata.Info]: the film's details.
	//   - [error]: as [metadata.Client.Movie] describes.
	Movie(ctx context.Context, title, year string) (metadata.Info, error)
	// [Show] looks up a TV show.
	//
	// Parameters:
	//   - [ctx]: cancels the lookup.
	//   - [name]: the show's name.
	//   - [year]: the year it first aired, or "" for any.
	//
	// Returns:
	//   - [metadata.Info]: the show's details.
	//   - [error]: as [metadata.Client.Show] describes.
	Show(ctx context.Context, name, year string) (metadata.Info, error)
	// [Episode] looks up one episode of a show.
	//
	// Parameters:
	//   - [ctx]: cancels the lookup.
	//   - [showID]: the show's TMDB ID.
	//   - [season]: the season number.
	//   - [episode]: the episode number.
	//
	// Returns:
	//   - [metadata.Info]: the episode's details.
	//   - [error]: as [metadata.Client.Episode] describes.
	Episode(ctx context.Context, showID, season, episode int) (metadata.Info, error)
	// [Season] lists every episode of one season.
	//
	// Parameters:
	//   - [ctx]: cancels the lookup.
	//   - [showID]: the show's TMDB ID.
	//   - [season]: the season number.
	//
	// Returns:
	//   - [[]metadata.Episode]: the season's episodes.
	//   - [error]: as [metadata.Client.Season] describes.
	Season(ctx context.Context, showID, season int) ([]metadata.Episode, error)
}

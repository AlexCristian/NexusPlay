package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// [Media] is one playable file and what the library knows about it.
type Media struct {
	// [ID] identifies the media item. It stays the same when a conversion
	// replaces the file.
	ID int64
	// [Path] is the file's path relative to the media folder, always with
	// forward slashes, such as "Movies/The Matrix (1999).mp4".
	Path string
	// [Title] is the title shown in the library.
	Title string
	// [Year] is the release year, such as "1999", or "" if unknown.
	Year string
	// [Overview] is the plot summary, or "" if there is none.
	Overview string
	// [Category] is the home-page row the item appears in when it is not in a
	// collection, such as "Movies", or "" (shown as "Other").
	Category string
	// [CollectionID] is the collection the item belongs to, or 0 when it stands
	// alone.
	CollectionID int64
	// [Season] is the season number of an episode, or 0.
	Season int
	// [Episode] is the episode number within its season, or 0.
	Episode int
	// [EpisodeEnd] is the last episode of a file that holds several, such as 2
	// for "S01E01E02", or 0 for a file with one episode.
	EpisodeEnd int
	// [Poster] is the poster's file name in the poster folder: the item's own,
	// else its collection's, else "".
	Poster string
	// [OwnPoster] is [true] when [Media.Poster] is the item's own, not its
	// collection's.
	OwnPoster bool
	// [Duration] is the length in seconds, or 0 if unknown.
	Duration float64
	// [Container] is the container format names ffprobe matched, as one
	// comma-separated string, such as
	// "mov,mp4,m4a,3gp,3g2,mj2".
	Container string
	// [VideoCodec] is the video codec as ffprobe names it, such as "h264".
	VideoCodec string
	// [AudioCodec] is the first audio track's codec, such as "aac", or "" if
	// the file has no audio.
	AudioCodec string
	// [PixelFormat] is the video's pixel format, such as "yuv420p", or "" if it
	// has not been probed yet.
	PixelFormat string
	// [Size] is the file's size in bytes.
	Size int64
	// [Published] is [true] once an administrator has published the item; until
	// then only administrators can see it.
	Published bool
	// [Views] counts how many times the item's player page was opened while
	// it was published; every page load counts, reloads included.
	Views int
	// [AddedAt] is when the scanner first found the file.
	AddedAt time.Time
	// [PublishedAt] is when the item was published, or the zero time if it is
	// not.
	PublishedAt time.Time

	// [CollectionName] is the name of the item's collection, or "" when it has
	// none.
	CollectionName string
}

// [mediaColumns] are the columns every media query selects, in the order
// [scanMediaWith] reads them. The poster falls back to the collection's.
const mediaColumns = `m.id, m.path, m.title, m.year, m.overview, m.category, COALESCE(m.collection_id, 0),
	m.season, m.episode, CASE WHEN m.poster <> '' THEN m.poster ELSE COALESCE(c.poster, '') END,
	m.duration, m.container, m.video_codec, m.audio_codec, m.size, m.published, m.views,
	m.added_at, m.published_at, COALESCE(c.name, ''), m.pix_fmt, m.poster <> '', m.episode_end`

// [mediaFrom] is the FROM clause that goes with [mediaColumns]: media "m"
// joined to its collection "c", if any.
const mediaFrom = ` FROM media m LEFT JOIN collections c ON c.id = m.collection_id `

// [canonicalCategory] tidies a category name and, if a category with the same
// name in different letter case already exists, uses that spelling, so
// "movies" joins "Movies" instead of starting a second home-page row.
//
// Parameters:
//   - [ctx]: cancels the query.
//   - [transaction]: the transaction of the write that sets the category.
//   - [name]: the category as typed.
//
// Returns:
//   - [string]: the name with runs of spaces made single and spaces around
//     it trimmed, spelled like an existing category that matches it
//     regardless of ASCII letter case: the spelling whose first media item
//     or collection has the lowest ID. The IDs of media and of collections are
//     compared as one sequence, so this is not necessarily the oldest
//     spelling. "" for a blank name.
//   - [error]: [nil] on success, or the database error.
func canonicalCategory(ctx context.Context, transaction *sql.Tx, name string) (string, error) {
	name = strings.Join(strings.Fields(name), " ")
	if name == "" {
		return "", nil
	}
	var existing string
	err := transaction.QueryRowContext(ctx, `
		SELECT category FROM (
			SELECT category, MIN(id) AS first FROM media WHERE category = ? COLLATE NOCASE GROUP BY category
			UNION ALL
			SELECT category, MIN(id) FROM collections WHERE category = ? COLLATE NOCASE GROUP BY category
		) ORDER BY first LIMIT 1`, name, name).Scan(&existing)
	if errors.Is(err, sql.ErrNoRows) {
		return name, nil
	}
	return existing, err
}

// [scanString] reads a row with one text column, for [queryAll].
//
// Parameters:
//   - [row]: the row to read.
//
// Returns:
//   - [string]: the column's value, or "" if [error] is not [nil].
//   - [error]: [nil] on success, or the scan error.
func scanString(row rowScanner) (string, error) {
	var value string
	err := row.Scan(&value)
	return value, err
}

// [likeEscape] makes text safe to put in a LIKE pattern used with
// "ESCAPE '\'", so its "%" and "_" match themselves.
//
// Parameters:
//   - [text]: the text to search for.
//
// Returns:
//   - [string]: [text] with "\", "%" and "_" each preceded by "\".
func likeEscape(text string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(text)
}

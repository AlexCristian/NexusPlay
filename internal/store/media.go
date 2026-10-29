package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// Media is one playable file.
type Media struct {
	ID           int64
	Path         string // relative to the media directory, always with forward slashes
	Title        string
	Year         string
	Overview     string
	Category     string
	CollectionID int64 // 0 when standalone
	Season       int
	Episode      int
	EpisodeEnd   int    // the last episode of a multi-episode file; 0 for one episode
	Poster       string // file name in the poster directory: its own, else its collection's
	OwnPoster    bool   // Poster is the media's own, not its collection's
	Duration     float64
	Container    string
	VideoCodec   string
	AudioCodec   string
	PixFmt       string // video pixel format, e.g. "yuv420p"; "" if not probed yet
	Size         int64
	Published    bool
	Views        int
	AddedAt      time.Time
	PublishedAt  time.Time

	// Filled by queries that join collections.
	CollectionName string
}

const mediaCols = `m.id, m.path, m.title, m.year, m.overview, m.category, COALESCE(m.collection_id, 0),
	m.season, m.episode, CASE WHEN m.poster <> '' THEN m.poster ELSE COALESCE(c.poster, '') END,
	m.duration, m.container, m.video_codec, m.audio_codec, m.size, m.published, m.views,
	m.added_at, m.published_at, COALESCE(c.name, ''), m.pix_fmt, m.poster <> '', m.episode_end`

const mediaFrom = ` FROM media m LEFT JOIN collections c ON c.id = m.collection_id `

// canonicalCategory trims a category name and, if a category with the same
// name in different letter case already exists, returns that spelling, so
// "movies" joins "Movies" instead of starting a second home-page tab.
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

func scanString(row rowScanner) (string, error) {
	var value string
	err := row.Scan(&value)
	return value, err
}

func likeEscape(text string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(text)
}

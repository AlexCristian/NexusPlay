Nexus Play
==========

A small, self-hosted web media center. Point it at a folder of videos and stream them to any
browser, on any device.

This is a rewrite in Go of the original 2013 Java EE version (still in this repository's
history). It keeps the same features and look, and ships as a single binary or a
Docker image.

Features
--------

- Watches a media folder and picks up new videos automatically (MP4, MKV, WebM, MOV, AVI, …).
- Looks up posters, titles and descriptions on TMDB (optional, needs a free API key).
- Organizes TV shows into collections by season and episode, guessing both from file names
  such as `Show/Season 2/Show.S02E05.mkv`.
- Plays files directly when the browser supports them, and otherwise converts them on the fly
  to HLS with ffmpeg.
- Remembers where each user stopped watching and resumes from there.
- Subscriptions: get notified when new episodes are added to a show.
- Subtitles (SRT or WebVTT), either uploaded or picked up from files next to the video
  (`Movie.en.srt`).
- Community subtitle corrections: viewers propose fixes to a line and vote; a fix is applied
  once it gets enough votes.
- New files wait in a control panel until the administrator reviews and publishes them.

Quick start with Docker
-----------------------

```sh
mkdir media            # put your videos here
docker compose up -d   # builds the image and starts the server
```

Open http://localhost:8080 and register. **The first account becomes the administrator.**
Then open the **Control panel** to publish the videos the scanner found.

Without Compose:

```sh
docker build -t nexusplay .
docker run -d --name nexusplay -p 8080:8080 \
  -v /path/to/videos:/media:ro \
  -v nexusplay-data:/data \
  nexusplay
```

The media folder is mounted read-only: Nexus Play never changes your files. Everything it
stores (database, posters, transcode scratch space) lives in `/data`. The container runs as
UID 1000, so the media folder must be readable by that user.

Running without Docker
----------------------

You need Go 1.27+ and ffmpeg (which includes ffprobe).

```sh
go build -o nexusplay ./cmd/nexusplay
NEXUSPLAY_MEDIA_DIR=/path/to/videos ./nexusplay
```

The pure-Go SQLite driver needs no C compiler, so cross-compiling is simple, for example
`GOOS=linux GOARCH=arm64 go build ./cmd/nexusplay` for a Raspberry Pi.

Configuration
-------------

All settings are environment variables.

| Variable | Default | Meaning |
|---|---|---|
| `NEXUSPLAY_ADDR` | `:8080` | Address to listen on |
| `NEXUSPLAY_MEDIA_DIR` | `./media` (`/media` in Docker) | Folder with your videos |
| `NEXUSPLAY_DATA_DIR` | `./data` (`/data` in Docker) | Database, posters and transcode cache |
| `NEXUSPLAY_TMDB_API_KEY` | (empty) | TMDB v3 API key or v4 read access token; enables metadata lookups |
| `NEXUSPLAY_SCAN_INTERVAL` | `5m` | How often to rescan in full (changes are also picked up within seconds) |
| `NEXUSPLAY_VOTES_REQUIRED` | `4` | Votes needed to apply a subtitle correction |
| `NEXUSPLAY_MAX_TRANSCODES` | `2` | Videos that may be converted at the same time |
| `NEXUSPLAY_REQUIRE_LOGIN` | `false` | Hide the library from visitors who are not signed in |
| `NEXUSPLAY_SECURE_COOKIES` | `false` | Set to `true` when serving over HTTPS |
| `NEXUSPLAY_FFMPEG`, `NEXUSPLAY_FFPROBE` | `ffmpeg`, `ffprobe` | Paths to the ffmpeg tools |
| `NEXUSPLAY_DEBUG` | (empty) | Set to anything to log every request |

If you expose Nexus Play to the internet, put it behind a reverse proxy that terminates HTTPS
(Caddy, nginx, Traefik), and set `NEXUSPLAY_SECURE_COOKIES=true`. Consider
`NEXUSPLAY_REQUIRE_LOGIN=true` too.

Naming your files
-----------------

The scanner guesses from paths; you can correct anything before publishing.

```
media/
├── Movies/
│   ├── The Matrix (1999).mp4
│   └── The Matrix (1999).en.srt     ← subtitles, imported automatically
└── Doctor Who (2005)/
    └── Season 2/
        └── Doctor.Who.S02E03.School.Reunion.mkv
```

Development
-----------

The UI is server-rendered with [templ](https://templ.guide) and [htmx](https://htmx.org).
After editing a `.templ` file, regenerate the Go code:

```sh
go run github.com/a-h/templ/cmd/templ@v0.3.1070 generate
go test ./...
```

Project layout:

| Path | Contents |
|---|---|
| `cmd/nexusplay` | Entry point |
| `internal/config` | Settings from environment variables |
| `internal/store` | SQLite schema, migrations and queries |
| `internal/library` | Media folder scanner, file-name parsing, ffprobe |
| `internal/metadata` | TMDB client |
| `internal/stream` | Direct-play decisions and HLS transcoding |
| `internal/subtitle` | SRT/WebVTT parsing and writing |
| `internal/auth` | Password hashing (argon2id) and session tokens |
| `internal/web` | HTTP handlers, templates, CSS and JavaScript |

Third-party assets bundled in `internal/web/static`: htmx (0BSD), hls.js (Apache 2.0),
Open Sans (SIL Open Font License, see `fonts/OFL.txt`) and icons from Lucide (ISC).

License
-------

GNU Affero General Public License v3.0. See [LICENSE.md](LICENSE.md).

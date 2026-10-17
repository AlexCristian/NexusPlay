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
- Keeps the library as browser-ready MP4s: any other file (MKV, AVI, WebM, HEVC, …) is
  converted in the background as soon as it is found, then **the original is deleted**. All
  audio tracks and chapters are kept; built-in text subtitles are saved as `.srt` files next to
  the video. Until a file is converted it still plays, through on-the-fly HLS.
- Remembers where each user stopped watching and resumes from there.
- Subscriptions: get notified when new episodes are added to a show.
- Subtitles (SRT or WebVTT), either uploaded or picked up from files next to the video
  (`Movie.en.srt`).
- Community subtitle corrections: viewers propose fixes to a line and vote; a fix is applied
  once it gets enough votes.
- New files wait in a control panel until the administrator reviews and publishes them.

Quick start with Docker
-----------------------

Prebuilt images for `linux/amd64` and `linux/arm64` are published to
`ghcr.io/alexcristian/nexusplay`. Next to `docker-compose.yml`:

```sh
mkdir media                                # put your videos here
docker compose pull && docker compose up -d
```

Open http://localhost:8080 and register. **The first account becomes the administrator.**
Then open the **Control panel** to publish the videos the scanner found.

Without Compose:

```sh
docker run -d --name nexusplay -p 8080:8080 \
  -v /path/to/videos:/media \
  -v nexusplay-data:/data \
  ghcr.io/alexcristian/nexusplay:go-rewrite
```

To build the image yourself instead, uncomment `build: .` in `docker-compose.yml` and run
`docker compose up -d --build`.

| Image tag | What it is |
|---|---|
| `go-rewrite` | Latest build of the `go-rewrite` branch |
| `master` | Latest build of `master` |
| `latest` | Latest build of the default branch (`master`) |
| `1.2.3`, `1.2` | A release, from a `v1.2.3` git tag |
| `sha-1abb108` | One exact commit |

**Nexus Play rewrites your media folder.** Files that are not browser-ready MP4s are converted
and the originals deleted, so the folder is mounted writable and must be writable by the
container's user (UID 1000). To keep your originals untouched, set `NEXUSPLAY_CONVERT=false` or
mount the folder read-only (`:ro`); conversion then switches itself off and those files play
through on-the-fly HLS instead. Everything else Nexus Play stores (database, posters,
transcode scratch space) lives in `/data`.

Install on a NAS
----------------

Any NAS that runs Docker with Compose works (Synology Container Manager, QNAP Container
Station, Unraid, TrueNAS, or plain Docker over SSH). The commands below run in an SSH session
on the NAS.

**1. Check the CPU type.** Images exist for 64-bit Intel/AMD and 64-bit ARM:

```sh
uname -m
```

| Output | Supported |
|---|---|
| `x86_64` | Yes (`linux/amd64`) |
| `aarch64` or `arm64` | Yes (`linux/arm64`) |
| `armv7l` or anything else | No prebuilt image; older 32-bit ARM models are not covered |

**2. Make a folder and get the compose file.** For example, on a Synology:

```sh
mkdir -p /volume1/docker/nexusplay && cd /volume1/docker/nexusplay
curl -fsSLO https://raw.githubusercontent.com/AlexCristian/NexusPlay/go-rewrite/docker-compose.yml
```

Point the media volume at your videos by editing the left side of `./media:/media`, for example
`/volume1/video:/media`.

**3. Set the user the container runs as.** Nexus Play must be able to write to the media
folder, because it converts files to MP4 and deletes the originals. By default the container
runs as UID 1000, which on most NAS systems owns nothing. Find the owner of your media folder:

```sh
ls -ln /volume1/video    # third and fourth columns: UID and GID
id your-nas-username     # or look up your own account
```

Then uncomment the `user:` line in `docker-compose.yml` with those numbers. On a Synology the
first admin account is usually `1026:100`. When you set `user:`, also replace the
`nexusplay-data:/data` volume with a folder that user owns, such as `./data:/data`, and create
it first with `mkdir data`. The named volume is created owned by UID 1000 and would not be
writable. If you prefer Nexus Play never to change your media folder, mount it with `:ro`
instead; conversion then switches itself off and the control panel says so.

**4. Start it, and later update it, with the same command:**

```sh
docker compose pull && docker compose up -d
```

`pull` downloads the newest image for the tag in the compose file; `up -d` recreates the
container only if the image changed. Your database and settings live in the data volume and
survive updates. To remove old images afterwards: `docker image prune`.

**5. If `pull` says "denied" or "unauthorized".** Packages on ghcr.io start out private. Either:

- make the package public: on GitHub, open your profile's **Packages** tab, then
  **nexusplay → Package settings → Change visibility → Public**; or
- keep it private and log in on the NAS once with a personal access token (classic) that has
  only the `read:packages` scope:

  ```sh
  docker login ghcr.io -u AlexCristian    # paste the token as the password
  ```

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
| `NEXUSPLAY_CONVERT` | `true` | Convert files that are not browser-ready MP4s and delete the originals |
| `NEXUSPLAY_MAX_CONVERSIONS` | `1` | Files converted to MP4 at the same time |
| `NEXUSPLAY_MAX_TRANSCODES` | `2` | Videos streamed through on-the-fly HLS at the same time (only files not yet converted) |
| `NEXUSPLAY_REQUIRE_LOGIN` | `false` | Hide the library from visitors who are not signed in |
| `NEXUSPLAY_SECURE_COOKIES` | `false` | Set to `true` when serving over HTTPS |
| `NEXUSPLAY_FFMPEG`, `NEXUSPLAY_FFPROBE` | `ffmpeg`, `ffprobe` | Paths to the ffmpeg tools |
| `NEXUSPLAY_DEBUG` | `false` | `true` logs every request |

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

[ARCHITECTURE.md](ARCHITECTURE.md) maps the code: the packages, the life of a file from
scan to playback, locking and shutdown, and the safety rules the converter follows.

The UI is server-rendered with [templ](https://templ.guide) and [htmx](https://htmx.org).
After editing a `.templ` file, regenerate the Go code:

```sh
go run github.com/a-h/templ/cmd/templ@v0.3.1070 generate
go test ./...
```

### Integration and screenshot tests

The `integration` package runs the real app end to end, with ffmpeg, SQLite and a pinned
headless Chrome (driven from Go with chromedp, no Node needed):

- **HTTP tests**: the scanner follows the media folder, transcodes produce H.264/AAC, the
  transcode limit, progress, notifications and subtitle upload.
- **Conversion tests**: originals are replaced by MP4s without anyone opening them, with every
  audio track kept and built-in subtitles extracted; failures leave the original alone and can
  be retried; a read-only media folder is left untouched.
- **Browser tests**: first-run setup, publishing, browsing, playback (direct and HLS), the
  subtitle correction vote, resume, search and login. Console errors and Content Security
  Policy violations fail the test.
- **Screenshot tests**: key screens are compared pixel by pixel with the reference images in
  `integration/testdata/screenshots`.

```sh
./scripts/install-chrome.sh                      # pinned Chrome for Testing, into .cache/
go test -tags integration ./integration/
```

When a screen changes on purpose, regenerate its reference and review the new PNGs in the diff:

```sh
go test -tags integration ./integration/ -run TestScreens -update
```

A failing screen writes `<name>.actual.png` and `<name>.diff.png` (changed pixels in red) to
`integration/testdata/screenshots/failures/`; in CI they are uploaded as an artifact.
Screenshots are only comparable when rendered by the same Chrome build, which is why the
version is pinned in `scripts/install-chrome.sh`. Change it there, then regenerate every reference.

Project layout:

| Path | Contents |
|---|---|
| `cmd/nexusplay` | Entry point |
| `internal/config` | Settings from environment variables |
| `internal/store` | SQLite schema, migrations and queries |
| `internal/library` | Media folder scanner, file-name parsing, ffprobe |
| `internal/metadata` | TMDB client |
| `internal/stream` | Direct-play decisions and HLS transcoding |
| `internal/convert` | Background conversion to MP4 that replaces originals |
| `internal/subtitle` | SRT/WebVTT parsing and writing |
| `internal/auth` | Password hashing (argon2id) and session tokens |
| `internal/web` | HTTP handlers, templates, CSS and JavaScript |
| `internal/app` | Wires everything together (used by the command and the integration tests) |
| `integration` | End-to-end, browser and screenshot tests |

Third-party assets bundled in `internal/web/static`: htmx (0BSD), hls.js (Apache 2.0),
Open Sans (SIL Open Font License, see `fonts/OFL.txt`) and icons from Lucide (ISC).

License
-------

GNU Affero General Public License v3.0. See [LICENSE.md](LICENSE.md).

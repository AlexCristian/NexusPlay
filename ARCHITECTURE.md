# NexusPlay architecture

NexusPlay is one Go binary. It watches a folder of videos, keeps a SQLite
database in step with it, converts files browsers cannot play into MP4, and
serves a server-rendered web app (templ + htmx) for browsing, watching and
correcting subtitles. This document maps the code; the package docs
(`go doc ./internal/...`) give the details.

## Packages

```
cmd/nexusplay      main: load config, build the app, serve, stop on SIGTERM
internal/app       composition root: builds every component, runs and stops them
internal/config    NEXUSPLAY_* environment variables, defaults, validation

internal/web       routes, middleware, handlers (five groups), templates
internal/auth      password hashing, sessions, registration and login rules
internal/library   the scanner and the file-name parser
internal/convert   the background MP4 converter
internal/stream    on-demand HLS transcodes for unconverted files
internal/metadata  TMDB lookups (optional)
internal/posters   deletes poster files nothing uses any more
internal/store     SQLite: migrations and every query

internal/media     the playback policy (pure functions)
internal/ffmpeg    the only code that runs ffmpeg and ffprobe
internal/subtitle  SRT/WebVTT parsing and writing (pure functions)
```

Dependencies point down this list. `app` is the only package that imports
everything. Each consumer declares the small interface it needs
(`library.Store`, `convert.Library`, `web`'s `playbackStore`, and so on), so
a package depends on what it uses rather than on whole components.
`app` asserts at compile time that the real types satisfy them.

## The life of a file

1. **Scan.** `library.Scanner.Run` scans at start, every
   `NEXUSPLAY_SCAN_INTERVAL` (5 min), on "Scan now", and a few seconds after
   the file watcher sees a change. A scan walks the media folder and lists
   videos, sidecar subtitles and folders it could not read.
2. **Settle.** A new file is imported only once it has gone 30 s without
   being modified, so a file still being copied in is not read half-written.
3. **Probe.** ffprobe reads the container, codecs, pixel format and length.
   A file that fails to probe is logged and tried again next scan.
4. **Insert.** The file becomes an *unpublished* media row. `ParsePath`
   guesses the title, year, show, season and episode from its path.
5. **Enrich.** With a TMDB key, the title, year, overview and poster are
   looked up. Only unpublished media is updated, and only with what the
   lookup found, so an administrator's edits are never overwritten.
6. **Sidecars.** `Movie.en.srt` next to `Movie.mkv` is imported as a
   subtitle track, once (tracks are keyed by their source file).
7. **Convert.** If `media.NeedsConversion` says so, the converter remuxes
   or re-encodes to MP4, extracts text subtitles to `.srt` files, and
   checks the result with ffprobe (streams, duration).
8. **Swap.** The MP4 replaces the original (see "Safety invariants"). The
   media row keeps its ID, so progress, subtitles, votes and metadata carry
   over. The converter then triggers a scan, which imports the extracted
   subtitles.
9. **Publish.** The file waits in the control panel until an administrator
   reviews it and publishes it. Publishing notifies everyone subscribed to
   its collection, in the same transaction.
10. **Watch.** The player plays the file directly or, before conversion,
    through on-demand HLS (see "Playback").
11. **Progress.** player.js saves the position every 15 s while playing, on
    pause, and when the page is hidden. A video counts as watched when it
    ends or passes 95%.

Removal: a scan deletes the rows of files it looked for and did not find.
It never does so under a folder it could not read, or when the folder holds
no videos at all, which usually means an unmounted drive.

## Goroutines, locks and shutdown

`App.Serve` runs the HTTP server and five workers:

| Worker | Runs | Stops |
|---|---|---|
| `library.Scanner` | scans (one at a time) | between scans |
| `convert.Converter` | up to `NEXUSPLAY_MAX_CONVERSIONS` jobs | cancels ffmpeg; a swap in progress finishes |
| `stream.Manager` | evicts idle transcodes every 30 s | stops every transcode and removes its files |
| `auth.Sessions` | deletes expired sessions hourly | at once |
| `posters.Sweeper` | deletes unused poster files hourly | at once |

The scanner has two mutexes. `scan` serialises whole scans. `reconcile` is
held only while a scan changes the database, never while it probes files or
calls TMDB. The converter takes `reconcile` through `Scanner.Exclusive`
while it swaps a file and its row, so a scan never sees a file renamed but
not yet updated in the database.

On SIGTERM, `Serve` shuts down in this order:

1. Stop accepting requests, and give the ones in flight 10 s to finish.
2. Cancel the workers and wait up to 30 s.
3. Close the database.

If a worker is still running after 30 s, the database is left open and the
process exits anyway. SQLite's write-ahead log makes exiting safe; closing
the database under a file swap would not be.

## Data model

All tables are in `internal/store/migrations`. Things that are not obvious
from the schema:

- **Media IDs survive conversion.** `ReplaceMediaFile` updates the path and
  codecs of the existing row, so everything that refers to the media stays
  attached.
- **Case-insensitive names.** Emails, nicknames and collection names are
  `UNIQUE COLLATE NOCASE`: "Doctor Who" and "doctor who" are one
  collection, and a user cannot register `Alex@x` next to `alex@x`.
  Category names are not columns of their own. Writes reuse an existing
  spelling, and the home page groups them without regard to case.
- **What a delete removes.**
  - Deleting a user removes their sessions, progress, watched marks,
    subscriptions, notifications and votes. Their proposals stay, without
    an author.
  - Deleting media removes its progress, watched marks, notifications and
    subtitle tracks, and with each track its lines, proposals and votes.
  - Deleting a collection leaves its media in the library as standalone
    items.
- **Subtitle corrections.** A proposal is a row in `subtitle_changes`;
  proposing counts as the proposer's vote. Each user gets one vote per line.
  When a proposal reaches `NEXUSPLAY_VOTES_REQUIRED` votes, one transaction
  writes it into `subtitle_cues` and deletes every proposal for that line.
  Tracks are served from the database (`GET /subtitles/{id}`), so a
  correction shows the next time the video loads, without touching any
  file.
- **Posters.** `Media.Poster` falls back to the collection's poster;
  `OwnPoster` tells them apart. Several rows can share a file, so nothing
  deletes a poster when it is replaced or removed. Instead, the poster
  sweeper deletes files no row has used for an hour.

## HTTP layer

`web.Server.Handler` builds the route table and the middleware chain.
Requests pass through the middleware outermost first:

1. log
2. recover from panics
3. security headers (CSP: own scripts only; `blob:` for hls.js)
4. session: the viewer goes into the request context
5. CSRF

**Guards** wrap routes:

- `browse`: public unless `NEXUSPLAY_REQUIRE_LOGIN`.
- `member`: needs an account.
- `admin`: needs the administrator; anyone else gets 403.

When a guarded request has no account, htmx requests get 401 with
`HX-Redirect`, page loads are redirected to `/login?next=…`, and other
requests (media, subtitles, beacons) get a plain 401.

| Group | Routes | Guard |
|---|---|---|
| library | `GET /`, `/search`, `/collections/{id}` | browse |
| | `GET /me/resume`, `/me/notifications`, `POST /collections/{id}/subscribe` | member |
| playback | `GET /watch/{id}`, `/media/{id}/file`, `/media/{id}/hls/…`, `/posters/{name}` | browse |
| | `POST /me/progress` | member |
| subtitles | `GET /subtitles/{id}` | browse |
| | `GET /media/{id}/subtitle-report`, `POST /cues/{id}/changes`, `POST /changes/{id}/vote` | member |
| accounts | `GET/POST /login`, `/register`, `POST /logout` | none |
| admin | `/admin/...` | admin |

**Errors.** Handlers return an `error`, and `respondError` maps it in one
place:

| Error | Response |
|---|---|
| `store.ErrNotFound` | 404 |
| `store.ErrDuplicate` | 409 |
| `stream.ErrBusy` | 503 with `Retry-After: 30` |
| `*http.MaxBytesError` | 413 |
| `*statusError` | its own status and message |
| the client went away | nothing |
| anything else | logged, 500 |

Unpublished media is a 404 for everyone but the administrator, never a 403,
so its existence does not leak. For htmx requests, errors render as an
alert inside the dialog that asked; otherwise as a full page.

**Fragments.** `respond` writes the fragment alone for htmx and wraps it in
a page otherwise, so every dialog is also a working URL. Control-panel forms
are decoded and validated by `decodeMediaForm`, `decodeCollectionForm` and
`decodeSubtitleUpload` before anything is stored. A `*formError` is shown in
the form again.

**CSRF.** Every unsafe request must come from this site: the Origin or
Referer must match the Host, or, without either, `Sec-Fetch-Site` must be
absent or `same-origin`. Requests from a signed-in viewer must also carry
the session's token, in `X-CSRF-Token` or the `csrf_token` field.
`<body hx-headers>` puts the token on every htmx request, and player.js
reads it from there for `sendBeacon`, which cannot set headers. The body
size limit is applied before the token is read, so an oversize upload is a
413, not a CSRF error.

## Playback

`media.Deliver` decides how a file plays:

- **Directly** (`/media/{id}/file`, with range requests) when the browser
  can play it: MP4/M4V with H.264 (8-bit 4:2:0 only), VP9 or AV1 video and
  AAC, MP3, Opus or FLAC audio; or WebM with VP8, VP9 or AV1 and Opus or
  Vorbis.
- **Otherwise through HLS**, made on demand by `stream.Manager`:
  - Browser-safe H.264 video is copied (`-c:v copy`), so an MKV only needs
    a cheap remux. Anything else is encoded to H.264, with a keyframe every
    6 s so segments split cleanly.
  - AAC audio is copied. Other audio becomes stereo AAC.
  - The playlist is an HLS *event* playlist. ffmpeg appends segments as it
    goes, so playback starts after the first segment. Players treat an
    open event playlist like live TV and would join at its end, so
    player.js sets hls.js `startPosition` (0 or the resume point).
  - At most `NEXUSPLAY_MAX_TRANSCODES` run at once; beyond that the answer
    is 503. A transcode nobody fetches from for 3 minutes is stopped, and
    a finished one is kept for 6 hours.

`media.NeedsConversion` is stricter than `Deliver`: the converter's target
is MP4 with browser-safe H.264 and AAC or MP3 audio, which every browser
plays.

## Safety invariants

- **No file is lost by conversion.** The swap never overwrites another file:
  `Movie.mkv` becomes `Movie.mp4` through a hard link, which fails if
  `Movie.mp4` exists. Only an MP4 being fixed in place is replaced, by an
  atomic rename. It updates the row next, and deletes the original only
  after both have succeeded. If the row update fails, only the file this
  job created is removed. The swap runs under the scan lock and ignores
  cancellation. See `convert.Converter.swap`.
- **Paths stay inside their folders.**
  - HLS segment names are checked against a pattern.
  - Media paths are checked against the media folder.
  - Posters are served only by plain file name.
  - Uploaded posters are checked by content and created with `O_EXCL`.
  - Language tags from a video's subtitle streams are sanitised before the
    converter uses them in `.srt` file names.
  - `safeNext` redirects only to local paths after login.
- **The scanner never deletes files.** It deletes rows only for files it
  could look for and did not find.
- **Passwords** are hashed with argon2id. Session tokens are stored as
  SHA-256 hashes, so a leaked database holds no live sessions.

## Conventions

- **Errors.**
  - A missing row is `store.ErrNotFound`, and every lookup's doc says so.
  - Rule violations are named sentinels (`ErrDuplicate`, `ErrAlreadyVoted`,
    `ErrUnchanged`) or typed errors (`auth.ValidationError`).
  - Functions return `(T, error)` or `(T, bool)`, never a magic `""` or
    `0`.
  - Wrap errors with `%w` and context. Log an error once, where it is
    handled.
- **Construction.** A constructor takes `Deps` (collaborators) and `Config`
  (values) and does not start anything; `Run(ctx)` does. Nothing
  initialises itself lazily.
- **Comments** explain why. The tricky parts have a full explanation next
  to the code: the swap, the CSRF model, HLS event playlists, the scan lock,
  and the two "watched" rules.

## Testing

- `go test -race ./...`: unit tests. They use fakes for ffmpeg, TMDB and
  the converter's collaborators. Where a test needs the database, it uses
  a real SQLite file in a temporary folder.
- `go test -tags integration ./integration/`: starts the whole app with
  real ffmpeg and drives it with a pinned Chrome (`scripts/install-chrome.sh`).
  - End-to-end: playback, conversion, 10-bit video.
  - Screenshots of every screen, compared with
    `integration/testdata/screenshots`. To accept an intended UI change,
    add `-run TestScreens/<name>$ -update` after the package.

## Adding a feature

1. **Data:** a migration (`internal/store/migrations/000N_*.sql`) and the
   store methods, with their error contracts.
2. **Rules** that span rows go in one store transaction. Rules about
   accounts go in `auth`, and playback policy in `media`.
3. **The handler** goes in the group it belongs to:
   - Add the store methods it needs to that group's interface.
   - Decode forms with a `decode…` function.
   - Return errors rather than writing them.
4. **Route:** add it to the table in `Server.Handler`, with the right guard.
5. **Template:** markup in a `.templ` file, and any logic it needs in
   `views.go`. Run `templ generate`. Update the screenshots if the UI
   changed.

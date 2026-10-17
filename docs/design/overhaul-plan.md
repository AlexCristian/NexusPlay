# NexusPlay readability and architecture overhaul: implementation plan

Status: accepted for implementation · Branch: `go-rewrite` · October 2026

This is the single plan the team implements. It merges two inputs:

- **Our design** (`docs/design/overhaul-design.md`, written at `dca1cd0`). It came from three reviews: Google Go style research, an adversarial review that measured the code and reproduced bugs, and a collaborative review of what to keep.
- **An external plan** supplied by the repository owner. It was written against `539523f`, before MP4 conversion existed, so it has no converter items and its line numbers are stale.

Every external proposal was checked against the current code and the research before it was taken. Section 0 records each decision. Section 7 is the source of truth for what to do and in what order. When this document and `overhaul-design.md` disagree, this one wins.

Progress is tracked in the step list in section 7 ("done" with the commit).

---

## 0. How this plan was merged

Each external item was checked against the code at `1397d1d`, not against the external line numbers. "Adopted" means taken as proposed. "Modified" means the goal is taken but the shape changed. "Rejected" means not done.

| # | External item | Decision | Reason |
|---|---|---|---|
| 1 | `downloadPoster` returns `(string, error)` | Adopted | Same as our step 18; it has six silent failure paths today. |
| 2 | `Touch` returns `(dir string, ok bool)` | Adopted | Same as our step 19; the comma-ok idiom fits a cache lookup. |
| 3 | `ClearPoster bool` on `MediaUpdate` and collection updates | Modified | The need is real (an admin cannot remove a wrong poster, and an episode cannot fall back to its collection's poster), so it ships as a "Remove poster" checkbox on both edit pages, with a decoder that rejects "upload and remove" together (step 28). |
| 4 | Split `Scanner.Scan` into `walkMedia`, `syncDiscoveredMedia`, `pruneRemovedMedia` | Modified | We keep our four helpers, because the split also has to shrink the scan lock to the reconcile and keep the unreadable-folder guard (D4). |
| 5 | `ParsePath`: `matchEpisode`, `extractAncestorShow` | Modified | Adopted with two changes: `matchEpisode` returns a small struct instead of five results, and the folder helper also returns the season from a "Season 2" folder, which the proposed `(show, year)` signature drops. |
| 6 | `saveMedia`: `parseMediaForm`, `savePosterUpload` | Modified | Our `decodeMediaForm` + `applyMediaForm` also validates everything before the poster is written, which the external split does not. |
| 7 | `Manager.start`: extract `launchFFmpeg` | Modified | Process launch moves into `ffmpeg.Start`, shared with the converter, and runs outside the manager lock. |
| 8 | `subtitle.Parse` takes `io.Reader` and streams | Rejected | Encoding detection (`utf8.Valid`) needs the whole input, both callers already hold a `[]byte`, and subtitle files are small (uploads are capped at 20 MB), so streaming saves nothing. |
| 9 | Split `Server.Handler` into `registerPublicRoutes`, `registerAuthRoutes`, `registerAdminRoutes` | Rejected | The table is already grouped with comments, and one list in reading order is the best map of the app. |
| 10 | `register`: extract `validateRegistration` | Modified | Validation and creation move to `auth.Accounts.Register`, so the handler only decodes and renders. |
| 11 | `collection`: extract `groupEpisodesBySeason` | Adopted | Named `groupBySeason`; pure and table-tested. |
| 12 | `uploadSubtitle`: extract `readUploadedSubtitle` | Adopted | Named `decodeSubtitleUpload`, returning a `*ValidationError`. |
| 13 | `Scanner.Run`: run scans off the event loop with a coalescing trigger | Modified | Scans must stay serialised and the trigger channel already coalesces, so scans stay in the loop; we add the one real gap, a rescan when fsnotify reports `ErrEventOverflow`. |
| 14 | `hls`: separate playlist from segment polling | Adopted | Segment waiting moves into `stream`. |
| 15 | `config.LoadFrom(lookup)` and `Load()` as `LoadFrom(os.Getenv)` | Adopted | Same as our step 23, plus `Validate`. |
| 16 | Document status codes, query parameters, CSRF and htmx headers on every handler | Modified | Every handler gets one line (route, who, returns); status codes and `HX-*` headers are listed only where they are not the defaults set by the error mapper. |
| 17 | Comment `0001_init.sql` (`COLLATE NOCASE`, cascades, voting) | Modified | The notes go in `ARCHITECTURE.md` under the data model, because applied migrations are not edited. |
| 18 | Document the HLS process model and why H.264 is copied | Adopted | Added to the HLS why-comment and `ARCHITECTURE.md`. |
| 19 | `library` interfaces `MediaProber`, `MediaStore`, `MetadataFetcher`, set as exported fields | Modified | Named `Prober`, `Store`, `Metadata` to avoid stutter (`library.Store`), and passed through `NewScanner(Deps, Config)` so a missing one is a compile error. |
| 20 | `TestScan` without ffprobe, using a fake prober | Adopted | Step 20; one ffprobe-backed test stays. |
| 21 | `web.Transcoder` interface `{Playlist, Touch}` | Adopted | It becomes the playback group's consumer interface (step 27). |
| 22 | Hand-written composition root in `app.New` | Adopted | Matches our principle 2.5; we take the external `ensureDirectories`/`checkBinaries` helpers to shorten `New`. |
| 23 | `db.SetMaxOpenConns(1)` | Modified | We bound the pool at 8 instead of 1: with one connection every read waits behind every write, and any query issued while a `rows` iterator is open on the same goroutine deadlocks. |
| 24 | `_txlock=immediate` in the DSN | Adopted | Confirmed in `modernc.org/sqlite@v1.60.1` (`sqlite.go:395`, accepts deferred, immediate, exclusive); every `s.tx` caller writes. |
| 25 | Batch subtitle cue inserts, 100 per statement | Rejected | Inserts already run in one transaction with a prepared statement; measured on v1.60.1, 2,500 cues take 3.0 ms looped and 2.5 ms batched. |
| 26 | `errgroup`, `App.Start` + `App.Stop(ctx)` | Modified | One blocking `App.Run(ctx) error` instead of Start/Stop, but we take the bounded shutdown wait from `Stop(ctx)`: if workers miss the deadline, the store is left open rather than closed under them. |
| 27 | `stopAll` removes transcode directories | Adopted | Today they stay on disk until the next start; `stopAll` also waits for each ffmpeg to exit. |
| 28 | Replace `time.After` in `waitForFile` (timer "leak") | Rejected | Since Go 1.23 unreferenced timers are collected immediately, and this module requires Go 1.27; the function moves into `stream` anyway. |
| 29 | `%v` to `%w` in `transcode.go` | Modified | All three hand-formatted ffmpeg errors (`transcode.go:183`, two in `converter.go`) become `*ffmpeg.ExitError`, which unwraps. |
| 30 | Subtitle import counter counts duplicates | Adopted | `AddSubtitle` returns `(id, inserted, err)` and the scanner counts only inserts; today this is latent, because the source map is checked first. |
| 31 | Functional options `WithBaseURL`, `WithHTTPClient`, `WithLogger`; `tmdb_test.go` with `httptest` | Modified | Options for the first two only; the logger is required, so it is a normal parameter. |
| 32 | ADRs: no `Result<T>`/`*string`, no central ports package, no DI framework | Adopted | Identical to our sections 2.1, 2.4 and 2.5. |

Totals: 14 adopted, 14 modified, 4 rejected.

Our design items that the external plan lacks stay unchanged: defects D1–D15, the `ffmpeg` and `media` packages, the converter split and its safety tests, the error-returning handlers and central mapper, `auth.Accounts`/`Sessions`, `singleflight`, `queryAll`, and the must-not-break list.

## 1. What is actually wrong

The complaint was: long functions, almost no documentation, unclear modules, no dependency injection, and functions that return `""` on failure. Measured against the code:

| Complaint | Measured | Verdict |
|---|---|---|
| Functions far too long | 235 production functions; median 12 lines; 13 over 40; longest `Scanner.Scan` at 98 | **Partly true.** Few functions are long, but the long ones do 4–9 jobs each, and they are the most important ones (scan, convert, transcode, admin save). |
| Almost no documentation | 149/151 exported identifiers documented; 11/11 package docs; **29%** of unexported code documented; **0** of 12 store lookups say they return `ErrNotFound`; nothing describes how the pieces fit together | **True where it matters.** Nothing explains how the scanner, converter, transcoder and web layer interact, or the contracts: errors, concurrency, lifecycle. Several comments are wrong. |
| Unclear modules | `web` imports 8 of 9 sibling packages and has 63 methods on one `Server`; `convert` and `library` call each other at runtime through concrete types; the "can a browser play this" rule exists in 3 versions that disagree | **True.** |
| No dependency injection | 0 named interfaces; 5 direct `exec.CommandContext` calls; TMDB URL hard-coded; zero-value structs with lazy `init()` | **True.** Coverage is `convert` 19.9%, `stream` 13.4%, `metadata` 0%. |
| Returning `""` on failure | 18 places encode failure or absence in a zero value or swallow an error, including `downloadPoster` with 6 silent failure paths. `MediaUpdate.Poster` and `Collection.Poster` use `""` to mean "keep the current poster", so a poster can never be removed. | **True.** |

Defects, fixed first (phase 1 and 2):

| # | Defect | Severity | Status |
|---|---|---|---|
| D1 | The converter's final rename overwrites a `Movie.mp4` that appeared during the encode; the cleanup then **deletes that file**. | Critical | Fixed in `48dab8d` |
| D2 | Open redirect after login: `next=/%09/evil.example` passes `safeNext`. | High | Fixed in `e685ddd` |
| D3 | A subtitle language tag inside a media file (`../../../../tmp/pwned`) makes the converter write `.srt` files outside the media folder. | High | Fixed in `a1eb7fc` |
| D4 | One unreadable subfolder makes the scanner delete every row under it. | High | Fixed in `1397d1d` |
| D5 | Data race between `Scanner.Trigger` and `Run`'s lazy init. A scan triggered before `Run` starts is dropped. | High | Step 20 |
| D6 | Shutdown doesn't wait for background workers. The database can be closed under a running file swap. | High | Step 26 |
| D7 | The transcode janitor can delete the output folder of a newer transcode of the same video. | Medium | Step 19 |
| D8 | The CSRF check parses the whole upload before handler size limits. Oversize uploads get "invalid CSRF token". | Medium | Fixed in `8d8f88f` |
| D9 | About 10 handlers turn any database error into "Not found", unlogged. | Medium | Step 27 |
| D10 | A 10-bit H.264 `.mp4` plays in no browser, but is never converted and is stream-copied to HLS. | Medium | Step 17 |
| D11 | Two queries re-list the media columns by hand and leave `AddedAt`/`PublishedAt` zero. | Medium (latent) | Fixed in `f2e8d5c` |
| D12 | Home-page categories split by letter case. | Medium | Step 10 |
| D13 | Duplicate users and collections are detected by searching error text for "UNIQUE". | Medium | Fixed in `f2e8d5c` |
| D14 | Progress and votes don't check that the video is published; `NaN`/`Inf` positions are accepted. | Low-medium | Fixed in `e5d98f5` |
| D15 | Non-constant-time CSRF compare (fixed in `8d8f88f`); byte slicing that can split UTF-8; a tautological `HashToken` test; "Blade Runner 2049" parsed as year 2049; N+1 queries on the home page; the 3 s conversion poll loads every media row. | Low | Steps 5, 8, 13, 29 |
| D16 | Shutdown cancels transcodes without waiting for ffmpeg, and leaves their segments on disk until the next start. (From the external plan.) | Low | Step 19 |

## 2. Principles

### 2.1 Errors: what to return instead of `""`

A zero value meaning "it failed" is an **in-band error**, which Google's Go style guide says not to use ([decisions › In-band errors](https://google.github.io/styleguide/go/decisions#in-band-errors)). Go already has the shapes Kotlin's types provide:

| Kotlin | Go | Use for |
|---|---|---|
| `Result<T>` | `(T, error)` | Anything that can fail. On error, `T` is the zero value and callers never look at it. |
| `T?` | `(T, bool)` ("comma ok") | Absent, but not an error: cache and map lookups, such as `Touch(id) (dir string, ok bool)`. |
| — | `(T, inserted bool, error)` | Two successful outcomes: "inserted" or "already there". |
| — | `(T, error)` with a sentinel such as `store.ErrNotFound` | Absent, where callers usually propagate it. |
| — | An explicit field, such as `RemovePoster bool` | An update that must tell "leave unchanged" apart from "set to empty". |

A generic `Result[T]` is rejected. Go has no sum types, so it becomes a struct with methods that panic. It defeats `errcheck` and vet, moves `errors.Is`/`errors.As` one hop away, and every standard-library boundary still returns `(T, error)` ([decisions › Generics](https://google.github.io/styleguide/go/decisions#generics)). Returning `nil` from a `*string` only to signal failure is rejected for the same reason. Pointers stay where a value is genuinely optional data (`Viewer.User`).

Rules:

1. **Never encode failure in a zero value.** Never discard an error with `_ =` without a comment saying why that is safe.
2. **Make errors checkable:** sentinels (`store.ErrNotFound`) or types (`*ffmpeg.ExitError`), checked with `errors.Is`/`errors.As`. Wrap with `%w` and context: `fmt.Errorf("probe %s: %w", path, err)`.
3. **Handle or return, never both.** Library code returns errors. Only the owner of a unit of work logs them, once: the HTTP error mapper, the scan loop, the converter job, the transcode waiter, and `main`. A component may downgrade an error to a warning only when the operation is optional, and says so in a comment (poster download, sidecar import, deleting an original after a successful swap).
4. **Document the error contract** on every function that returns a sentinel.

### 2.2 Functions

Google sets no line limit; the rule is clarity ([best practices › Function length](https://google.github.io/styleguide/go/best-practices)).

- A function does one job, named by its name. Long functions are split along their real seams: decode → validate → act → render in handlers; walk → import → enrich → prune in the scanner.
- Errors and edge cases are handled first, with guard clauses. Nesting stays at 3 levels or fewer.
- More than 4 parameters or results, or several adjacent ones of the same type, means a struct.

### 2.3 Documentation

- Every package doc says what the package owns, what it must not do, and who uses it.
- Every exported identifier is documented, and so is every unexported function that is not obvious ([Go doc comments](https://go.dev/doc/comment)). Every HTTP handler gets one line: route, who can call it, what it returns. Status codes and `HX-*` headers are listed when they differ from the error mapper's defaults.
- Comments explain **why**. The tricky invariants (converter commit, CSRF model, HLS event playlists, the scan lock) get a full explanation next to the code.
- Contracts are written down: errors returned, concurrency safety, call order, cleanup.
- `ARCHITECTURE.md` gives the map (section 6).

### 2.4 Packages and interfaces

- **Consumer-defined, small interfaces** ([decisions › Interfaces](https://google.github.io/styleguide/go/decisions#interfaces)). The package that uses a dependency declares the 2–6 methods it calls. Producers return concrete types. No central `interfaces` or `ports` package, and no interface "for later". Names don't repeat the package (`library.Store`, not `library.MediaStore`).
- Interfaces only at real seams: ffmpeg/ffprobe, TMDB, and cross-component calls (the scanner as seen by the converter; the converter and transcoder as seen by the web layer).
- **Not** interfaces: the clock (a `func() time.Time`), the filesystem (the converter depends on real atomic-rename and link semantics; tests use `t.TempDir()`), the logger, config, and pure functions.
- Dependencies point one way. One package, one responsibility. No `util` packages.

### 2.5 Dependency injection

- **Constructors with explicit dependencies**, wired by hand in `internal/app`. Required collaborators go in a `Deps` struct, tunables in a `Config` struct. A missing dependency is a compile error, not a nil-pointer panic at the first scan.
- **No zero-value structs with lazy `init()`.** Constructors create channels and maps. This removes D5 by construction.
- **Functional options only for test overrides of good defaults:** `metadata.WithBaseURL`, `metadata.WithHTTPClient`. Required things (the logger) are parameters. Google prefers option structs in general ([best practices › Option structure](https://google.github.io/styleguide/go/best-practices)).
- **No DI framework.** About 10 components; a 60-line composition root is clearer. Wire was archived in 2025.
- Conformance is asserted in the composition root: `var _ convert.Store = (*store.Store)(nil)`.

### 2.6 Concurrency and lifecycle

- Every background component exposes a blocking `Run(ctx context.Context) error`. `App.Run` runs them in an `errgroup`, waits for all of them on shutdown with a deadline, and only then closes the database ([decisions › Goroutine lifetimes](https://google.github.io/styleguide/go/decisions#goroutine-lifetimes)).
- Each type documents its concurrency safety and which lock protects what.
- Hold locks around state, not around I/O. No filesystem work or process launch under a manager-wide mutex.
- `time.After` in a `select` loop is fine on Go 1.23+. Change it only for readability.

### 2.7 Tests

- Table-driven tests with `t.Helper()` and `cmp.Diff`; no assertion libraries.
- Real SQLite in `t.TempDir()` for store-backed tests. Hand-written fakes only at the seams in 2.4, plus database *failures* the converter must survive.
- Fakes live next to their consumer in `_test.go` files. No generated mocks.
- Integration tests keep proving what fakes cannot: real ffmpeg arguments, browser behaviour, screenshots.

## 3. Target package layout

```
cmd/nexusplay ──► internal/app            composition root; the only place that knows every concrete type
                     │
     ┌─────────┬─────┴──────┬───────────┬────────────┬──────────┐
     ▼         ▼            ▼           ▼            ▼          ▼
    web     library      convert      stream      metadata   config
     │       │   │        │  │          │  │
     │       │   └──┬─────┘  │          │  │
     │       │      ▼        ▼          │  ▼
     │       │    ffmpeg ◄───────────────┘ media ◄── (library, convert, stream, web)
     │       │
     │       ▼
     │     store ──► subtitle
     ▼
   auth ──► store
```

**Leaves** (import nothing internal): `ffmpeg`, `media`, `subtitle`, `metadata`, `config`.

New packages:

| Package | Owns | Replaces |
|---|---|---|
| `internal/ffmpeg` | The only code that runs ffmpeg or ffprobe. `Tools{FFmpeg, FFprobe}` with `Probe(ctx, file) (Probe, error)`, `Run(ctx, args, RunOptions) error` (progress callback, low priority), `Start(ctx, args) (*Process, error)` (long-running HLS). Failures are `*ExitError{Err, LastLine}` with `Unwrap`. | Two ffprobe parsers, the double probe in `verify`, two process wrappers, two copies of `lastLine`, three `%v`-formatted ffmpeg errors. |
| `internal/media` | The browser-compatibility policy as pure functions: `IsVideo`, `MIMEType`, `Deliver(Format) Delivery` (`DirectPlay` or `HLS`), `NeedsConversion(Format) bool`, `BrowserSafeH264(pixFmt) bool`. | `stream.DirectPlayable`, `convert.NeedsConversion`, `library.SupportedExtensions`, `web.videoTypes`, and the 10-bit rule hidden in `MakePlan`. |

Changed packages:

| Package | Change |
|---|---|
| `store` | Stays **one `Store`**, split by file per entity. Publishing plus notifications, votes plus cue edits, and first-user-is-admin are single-transaction rules that per-entity repositories would break. Additions: `queryAll` and `scanMediaWith(row, extra...)` (D11); unique violations by SQLite error code; `inserted bool` results; a typed `ConvertStatus`; `ErrNotFound` documented on every lookup; a DSN built with `url.URL`, `_txlock=immediate` and a bounded pool; explicit poster removal in updates. |
| `library` | `NewScanner(Deps, Config)`. Declares `Store`, `Prober` and `Metadata`. `Scan` splits into `walk`, `importNew`, `importSidecars` and `removeMissing` (the last already exists as `missingMedia`). The scan lock covers only the reconcile. `ParsePath` splits into `matchEpisode`, `parseMovie`, `showFromDirs`. |
| `convert` | `New(Deps, Config)`. Declares `Store`, `Tools` and `Library` (`Exclusive`, `Trigger`), so it no longer imports `library`. A `Disabled` null object replaces nil checks. The job splits into `prepare → encode → verify → commit`. `CheckConverted` is pure. |
| `stream` | `NewManager(Starter, Config, log)`. `Touch` returns `(string, bool)`. `Janitor` becomes `Run`. Per-job directories (D7). `stopAll` waits for each job and removes its directory (D16). File and process work happens outside the lock. `WaitForSegment` moves here from `web`. |
| `metadata` | `New(Config, log, ...Option)`. `fetchPoster` returns `(string, error)`; the caller logs and keeps the rest of the info. Shows go through `singleflight`. Tested against `httptest`. |
| `auth` | `Viewer` moves here from `web`. `Accounts` (register, login, timing equaliser) and `Sessions` (create, look up, end, expire in `Run`). |
| `config` | `LoadFrom(getenv func(string) string) (Config, error)`, table-driven, with `Validate()` covering every field. `Load()` calls `LoadFrom(os.Getenv)`. |
| `app` | `New` built from `ensureDirectories`, `checkBinaries`, `openStore` and the constructors. `App.Run(ctx) error` runs the workers in an `errgroup`, serves HTTP, and shuts down in order (step 26). The healthcheck reuses config defaults. |
| `web` | See 3.1. |

### 3.1 The web layer

- **Handlers return `error`.** One `Handle(fn)` adapter maps errors to responses: `ErrNotFound` → 404, `*ValidationError` → 400 with the message, `ErrDuplicate` → 409, `stream.ErrBusy` → 503 + `Retry-After`, `*http.MaxBytesError` → 413, client gone → nothing, anything else → logged 500. This fixes D9 everywhere and removes 48 copies of `serverError` and 28 of `notFound`.
- **One `Respond` helper** decides between htmx fragment and full page, so every dialog still works as a standalone URL. It replaces 13 hand-written branches.
- **Form decoders** (`decodeMediaForm(r) (mediaForm, error)` and similar) read and validate input, and are table-tested with `httptest.NewRequest`.
- **Feature handler groups**, each with a constructor that takes only what it uses, declared as consumer interfaces: `library` (browse, search, collections, subscriptions), `playback` (watch, file, HLS, progress; its transcoder interface is `{Playlist, Touch}`), `subtitles` (VTT, reports, proposals, votes), `accounts` (login, register, logout), `admin` (control panel, media, collections, subtitles, conversions).
- **One route table and one middleware chain**, in reading order, with a `chain(...)` helper. The route table stays in one function, grouped by comments as it is today.
- **View models** move out of `.templ` files into Go files: `Page`, `Playback`, `Season`, `MediaForm`, `ConversionView` and the URL helpers.
- **Body limits** apply before CSRF; CSRF prefers the header token and compares in constant time (D8, in progress).

Feature groups are done **inside `package web` first**. Moving them into subpackages is the last, optional step.

## 4. Patterns: adopted and rejected

| Pattern | Decision | Why |
|---|---|---|
| One ffmpeg runner shared by transcoder and converter | **Adopt** | Lifecycle, stderr capture, progress and "cancelled vs failed" are written and tested once. |
| Delivery policy as an enum plus `switch` (`media.Deliver`) | **Adopt** | One place answers "how does this file play". |
| Null object for disabled conversion (`convert.Disabled`) | **Adopt** | Removes nil checks and avoids the typed-nil-interface trap. |
| Named conversion states with explicit transitions | **Adopt**, no FSM library | `''` means both "queued" and "nothing to do" today. |
| Error-returning handlers plus a central mapper | **Adopt** | See 3.1. |
| Form decoders separate from handlers | **Adopt** | Handlers read as five steps; decoding is unit-testable. |
| Generic `queryAll[T]` row helper | **Adopt** | Removes real boilerplate that has already caused a bug. |
| `singleflight` for TMDB show lookups | **Adopt** | Concurrent scans and admin saves don't query the same show twice. |
| Explicit "remove" flags in update structs | **Adopt** | "Unchanged" and "empty" are different intents. |
| `Result[T]` / `Option[T]` types | Reject | Section 2.1. |
| Per-entity repositories plus unit of work | Reject | Breaks working single-transaction rules. |
| DI container (wire, dig, fx) | Reject | Section 2.5. |
| Scanner as a pipeline of `Step` interfaces | Reject | Steps have different inputs and failure policies. |
| Scans on a separate goroutine from the watcher loop | Reject | Scans must be serialised anyway; see section 9. |
| Generic job framework for transcoder and converter | Reject | They share only "run ffmpeg", which the runner covers. |
| Event bus between scanner and converter | Reject | `scanner.OnAdded(conv.Wake)` is one grep-able line. |
| Filesystem abstraction (afero) | Reject | Hides the rename, link and `O_EXCL` semantics the converter relies on. |
| A separate `domain` package | Reject | The store types are the domain types. |
| Multi-row batched `INSERT` for subtitle cues | Reject | Measured gain is 0.5 ms per 2,500 cues. |
| Route registration split into several methods | Reject | One table is the best map of the app. |

## 5. Function decomposition

Every production function over 40 lines, and its split:

| Function | Lines | Split into |
|---|---|---|
| `library.Scanner.Scan` | 98 | `walk(ctx) (tree, error)` (videos, sidecars, unreadable paths); `importNew` (settle time, probe, insert, enrich); `importSidecars`; `removeMissing`, wrapping the existing `missingMedia` and the empty-folder guard. The lock is held only for reconcile. |
| `library.ParsePath` | 70 | `matchEpisode(name) (episodeMatch, bool)`, one loop over both episode patterns, where `episodeMatch` holds season, episode, show and title; `parseMovie(name) Guess`; `showFromDirs(dir) (folder string, season int)`. |
| `convert.Converter.convert` | 62 | `prepare`, `extractSubtitles`, `encode`, `verify` (via `CheckConverted`), `commit`. |
| `web.saveMedia` | 62 | `decodeMediaForm`, `applyMediaForm` (collection and poster, validated before any side effect), then save and redirect. |
| `stream.Manager.start` | 60 | `reuse`, `reserve` (capacity under the lock), `launch` (via `ffmpeg.Start`, outside the lock), `finish`. |
| `subtitle.Parse` | 60 | A small `parser` struct with `line` and `flush` methods; `parseTiming` stays. Signature unchanged. |
| `config.Load` | 54 | Table-driven `LoadFrom` plus `Validate`. |
| `web.register` | 49 | `auth.Accounts.Register`; the handler only decodes and renders. |
| `web.collection` | 47 | `groupBySeason(eps) []Season`, plus one `collectionView` query helper. |
| `web.uploadSubtitle` | 47 | `decodeSubtitleUpload(r) (lang string, cues []subtitle.Cue, err error)`. |
| `library.Scanner.Run` | 46 | The loop delegates to `onEvent`, `onWatchError` (overflow → rescan) and `rescanSoon`; no lazy `init`. |
| `web.hls` | 45 | `playlist` and `segment` handlers; segment waiting moves into `stream`. |
| `web.Server.Handler` | 58 | Stays one route table, with the middleware chain extracted. |

## 6. Documentation deliverables

1. `ARCHITECTURE.md`:
   - purpose and shape; the package map from section 3
   - the life of a file: scan → settle → probe → insert → enrich → sidecars → convert → swap → publish → watch → progress
   - goroutines and locks; shutdown order
   - the data model: why media IDs survive conversion, why emails, nicknames and collection names use `COLLATE NOCASE`, what each `ON DELETE CASCADE` removes, how vote thresholds replace a line
   - the HTTP layer: routes, guards, middleware order, error mapping, CSRF
   - playback policy, HLS event playlists, and why H.264 video is copied (`-c:v copy`) while audio is always re-encoded
   - safety invariants; error conventions; testing; how to add a feature
2. Package docs rewritten to say what each owns and what it must not do.
3. Full "why" comments on: the converter commit, the CSRF model (done in the D8 change), HLS event playlists, the scan lock, the two "finished" rules (the player on `ended`, the server at 95%), and the `hx-headers` ↔ `player.js` CSRF coupling.
4. Error contracts on every function that returns a sentinel.
5. One line on every handler, and docs for every non-obvious unexported helper.
6. Fix the comments that are wrong: swap comments that promise a retry that doesn't happen; `cacheForever` caches for one day; `Media.Poster` falls back to the collection's poster; the conversions panel implies conversion is opt-in, but it defaults to on.

## 7. Implementation plan

Each step compiles, keeps `go vet`, `go test -race ./...` and the integration suite (including screenshots) green, and is one commit. Tests named in a step are written in that step, before the code change where the step fixes a bug. Risk rises down the list.

**Phase 1: data-safety and security fixes.**

1. **D1 (done, `48dab8d`):** no-overwrite link-and-remove for the final rename; `commit` re-checks under the scan lock; on DB failure only a file this job created is removed. Tests: `TestSwapNeverOverwritesAnExistingTarget`, `TestSwapDatabaseFailureRemovesOnlyItsOwnFile`, `TestSwapReplacesAndDeletesOriginal`, `TestSwapSameNameReplacesInPlace`, `TestSwapRefusesWhenTheRowChanged`.
2. **D3 (done, `a1eb7fc`):** language tags sanitised to `[a-z0-9-]{1,12}`, else `und`; output paths stay in the video's folder. Test: `TestSubtitleNamesIgnoreHostileLanguageTags`.
3. **D2 (done, `e685ddd`):** `safeNext` parses the URL and rebuilds it. Test: `TestSafeNext` (tab, `///`, backslash, scheme cases).
4. **D4 (done, `1397d1d`):** unreadable paths recorded during the walk; `missingMedia` skips rows under them. Tests: `TestMissingMediaKeepsUnreadableFolders`, `TestScanKeepsMediaInUnreadableFolders`.
5. **D8 (done, `8d8f88f`):** per-route body limit before CSRF, 413 for oversize, constant-time token compare. Tests: `TestOversizeUploadIsRejectedAsTooLarge`, `TestFormBodyLimit`, existing `TestCSRF`.
6. **D14 (done, `e5d98f5`):** progress and votes check that the media is visible to the user (one `visibleTo` rule); non-finite positions are rejected. Test: `TestUnpublishedMediaRejectsProgressAndVotes`.

**Phase 2: correctness fixes that need no restructuring.**

7. **(done, `f2e8d5c`)** `queryAll` and `scanMediaWith`, fixing D11. Test: `TestListQueriesKeepEveryMediaColumn`.
8. **(done, `f2e8d5c`)** Unique violations by SQLite error code (D13), exposed as `store.ErrDuplicate`; `errors.Is(err, sql.ErrNoRows)`; rune-safe truncation (D15); checked `RowsAffected`/`LastInsertId`. Tests: the existing duplicate cases in `TestFirstUserIsAdmin`, `TestTruncateRunes`.
9. **(done, `f266f86`)** `AddSubtitle` returns `(id int64, inserted bool, err error)`; `importSidecars` counts only inserts; the upload handler ignores `inserted`. (External item 30.) Tests: `TestAddSubtitleSameSourceTwice`, `TestImportSidecarsCountsOnlyInserted`.
10. **(done, `dd02bba`)** Category normalisation on write and case-insensitive grouping (D12). Test: `TestTilesGroupCategoriesIgnoringCase`.
11. **(done, `97f6a82`)** Store connection setup: build the DSN with `url.URL` (a data directory with `?` or `#` works); add `_txlock=immediate` (done in `f2e8d5c`, with `TestConcurrentWriteTransactions`, which fails with "database is locked" without it); `db.SetMaxOpenConns(8)`. Comment why it is not 1. Tests: `TestOpenPathWithQueryCharacters`, `TestConcurrentReadThenWriteTransactions` (eight goroutines running `ProposeChange` and `VoteChange` at once; none sees `SQLITE_BUSY`).
12. **(done, `d740eca`)** Log every swallowed error: rehash, logout, `watcher.Add`, `Rel`/`Info` in the walk, leftover cleanup. No new tests; reviewed by `errcheck`.
13. **(done, `6eb2b84`)** Fix the test bugs: the tautological `HashToken` assertion; Blade Runner (a trailing number is the year only if no later than next year). Tests: `TestHashTokenDiffersPerToken`, new `TestParsePath` rows.

**Phase 3: new leaf packages (behaviour-preserving).**

14. **(done, `83ae07d`)** `internal/ffmpeg` with `Probe`, `Run`, `Start` and `*ExitError` (implements `Unwrap`). Tests use the helper-process pattern instead of real ffmpeg: `TestRunReportsLastLine`, `TestRunCancelledIsNotExitError`, `TestExitErrorUnwraps`, `TestProbeParsesStreams`.
15. **(done, `38b3272`)** Switch `library` and `convert` probing to `ffmpeg.Probe`, with one probe in `verify`; switch both process wrappers. This removes the three `%v`-formatted ffmpeg errors (external item 29). Existing integration tests cover it.
16. **(done, `0f9098f`)** `internal/media`; `stream`, `convert`, `library` and `web` use it; the old functions are deleted. Test: `TestDeliver` (one table: container × video codec × audio codec × pixel format).
17. **(done, `ea47b2c`)** *(Behaviour change, isolated.)* Store `pix_fmt` (migration `0003`). `Deliver`, `NeedsConversion` and the HLS copy decision use `BrowserSafeH264` (D10). Tests: `TestNeedsConversionTenBit`, integration `TestTenBitH264IsConverted`.

**Phase 4: constructors and seams, one package per step.**

18. **(done, `b98638d`)** `metadata.New(Config, log, ...Option)` with `WithBaseURL` and `WithHTTPClient`; `fetchPoster` returns `(string, error)`; `singleflight` for shows. Tests against `httptest.Server`: `TestMovieLookup`, `TestFetchPosterReportsHTTPError`, `TestFetchPosterKeepsExistingFile`, `TestShowLookupsCoalesce`.
19. **(done, `86e422a`)** `stream.NewManager(Starter, Config, log)`; `Touch` returns `(string, bool)`; per-job directories (D7); `Janitor` becomes `Run`; on shutdown `stopAll` cancels every job, waits for its `done`, and removes its directory (D16); `waitForFile` moves here as `WaitForSegment`. Tests with a fake `Starter`: `TestTouchUnknownJob`, `TestJanitorKeepsNewerJobDir`, `TestStopRemovesJobDirs`, `TestStartWhenFullIsBusy`.
20. **(done, `ee57e93`)** `library.NewScanner(Deps, Config)` with `Store`, `Prober`, `Metadata`; the `Scan` split and lock scope; no lazy init (D5); `Run` returns `error`; a watcher `ErrEventOverflow` schedules a rescan. Tests: `TestScanWithFakeProber` (no ffprobe needed; the ffprobe-backed `TestScan` stays), `TestTriggerBeforeRunIsKept`, `TestScanLockReleasedWhileProbing`, `TestWatchOverflowSchedulesRescan`.
21. **(done, `2afe6ca`)** `ParsePath` split into `matchEpisode`, `parseMovie`, `showFromDirs`. Tests: the existing `TestParsePath` table unchanged, plus `TestMatchEpisode` and `TestShowFromDirs` (including a "Season 2" folder supplying the season).
22. **(done, `5b61657`)** `convert.New(Deps, Config)` with consumer interfaces, a `FreeSpace` func, `Disabled`, the `prepare/encode/verify/commit` split, `CheckConverted` and named states. Tests: `TestCheckConverted`, and swap tests for no space, verification failure, original delete fails and cancellation (the D1 tests from step 1 move over unchanged).
23. **(done, `91e22eb`)** `config.LoadFrom` and `Validate`. Tests: `TestLoadFrom` (table of env maps), `TestValidate`.
24. **(done, `bd2fb47`)** `auth.Accounts`, `auth.Sessions`, `Viewer` in `auth`. Tests: `TestRegisterFirstUserIsAdmin`, `TestLoginUnknownEmailStillHashes`, `TestLoginRehashesWeakHash`, `TestSessionsExpire`.
25. **(done, `de514c2`)** `app.New` split into `ensureDirectories`, `checkBinaries`, `openStore` and wiring; compile-time interface assertions; a healthcheck that uses config. Test: `TestNewFailsWithoutFFmpeg`.
26. **(done, `de514c2`)** `App.Run(ctx) error` (adds `golang.org/x/sync`, already in `go.sum` at v0.23.0). Workers run in an `errgroup`. On shutdown: stop the HTTP server, cancel workers, wait up to `ShutdownTimeout` (30 s; a swap ignores cancellation), then close the store. If the wait times out, log which workers are still running and return without closing the store; SQLite's WAL makes process exit safe, closing under a swap is not (D6). `main` calls `Run` and nothing else. Remove the 50 ms sleep in the integration harness. Tests: `TestRunClosesStoreAfterWorkers`, `TestRunShutdownTimeoutLeavesStoreOpen`.

**Phase 5: the web layer, one file per step.**

27. **(done, `091c031`, `05a3a36`, `c2ac868`, `d3b19e0`)** `Handle`/error mapper, `Respond`, `PathID` returning an error. Convert `handlers_library.go`, then `handlers_watch.go`, `handlers_account.go`, `handlers_admin.go`, one commit each (fixes D9 file by file). Tests: `TestHandleMapsErrors` (one row per mapped error), `TestRespondFragmentOrPage`.
28. **(done, `bcd9db0`)** Poster removal (external item 3). `MediaUpdate` and the collection update gain `RemovePoster bool`; SQL becomes `poster = CASE WHEN ? THEN '' WHEN ? <> '' THEN ? ELSE poster END`. Both edit pages show a "Remove poster" checkbox next to the current image, only when there is one. The episode page keeps its note that an episode without a poster shows its collection's. The decoder rejects an upload together with "remove" (400). Poster files stay on disk, as they do when replaced today. `SetScannedMetadata` keeps `""` = "lookup found nothing", which is a fill-only rule, and says so. Tests: `TestUpdateMediaRemovesPoster`, `TestUpdateCollectionRemovesPoster`, `TestDecodeMediaFormRejectsUploadAndRemove`; update the edit-page screenshots.
29. **(done, `31229f5`, `67f490c`, `6aab480`, `041e604`)** Feature handler structs with consumer interfaces; view models out of `.templ`; form decoders; the remaining long handlers split (section 5). Tests: `TestDecodeMediaForm`, `TestDecodeSubtitleUpload`, `TestGroupBySeason`.
30. **(done, `e9c98f0`, `75969e3`)** Small fixes: `RecentTiles` as one query; conversion status counted in SQL (D15). Test: `TestConversionCounts`.

**Where the implementation differs from the steps above:**

- Step 26: the entry point is `App.Serve(ctx, ln)`, which takes a listener so tests can bind port 0. Workers run on a `sync.WaitGroup` rather than an `errgroup`: a failed worker is logged and does not stop the others, so nothing needs the group's first error. `golang.org/x/sync` is used only for `singleflight`.
- Step 29: the media and collection forms decode into `mediaInput` and `collectionInput`. The poster's type is sniffed while decoding, so an unsupported image is refused before a collection is created. `NEXUSPLAY_DEBUG` moved into `config` (it now takes `true`/`false`).
- Step 30: conversion status is not counted in SQL. That would copy `media.NeedsConversion` into SQL, so there would be two policies to keep in step. Instead, the poll reads only the five columns the decision needs, and jobs load their full row when they start.

**Phase 6: documentation.**

31. `ARCHITECTURE.md`, package docs, why-comments, error contracts and handler lines (section 6). Written last, so it describes the final code. Each earlier step still documents what it adds.

**Phase 7 (optional):** move the web feature groups into subpackages once their boundaries have held for a while.

## 8. Behaviours that must not change

Each gets a test before the code around it moves.

1. **Converter commit order:** verify the MP4 (H.264, every planned audio track, duration within max(2 s, 1%)); put it in place without overwriting anything it did not create; point the media row at it; only then delete the original. Same-name files are replaced atomically. The swap runs under the scan lock and ignores cancellation. The media ID is preserved, so progress, subtitles, votes and metadata carry over.
2. **Converter bookkeeping:** hidden temp names (`.x.nexusplay-converting.mp4`, `.x.srt.part`), cleaned at start only after the write check passes; a shutdown is never recorded as a failure; no disk space postpones a file for 30 minutes and is not a failure; existing sidecar files are never overwritten; sidecars stay in the video's folder.
3. **Scanner:** 30 s settle time; empty-folder guard; unreadable folders keep their rows; hidden files skipped; 5 s debounce with `Chmod`-only events ignored; `OnAdded` fires only when something was added; scanned metadata never overwrites admin edits.
4. **CSRF:** GET, HEAD and OPTIONS exempt; every other method passes the same-origin check (Origin, then Referer, then `Sec-Fetch-Site`); signed-in requests also need the token, in the header or form field; `/login` and `/register` skip the token but not the origin check; session middleware runs before CSRF; oversize bodies get 413.
5. **Visibility and login redirects:** unpublished media returns 404, not 403, to non-admins; `/media/*` and `/subtitles/*` get a bare 401 instead of a login redirect; htmx requests get `HX-Redirect`.
6. **Accounts:** unknown-email logins still run argon2; weak hashes are re-hashed on login; session tokens are stored hashed; the first user becomes admin inside the insert transaction.
7. **Voting:** one vote per user per line; the proposer's vote counts; reaching the threshold replaces the line and clears every competing proposal.
8. **Progress:** 95% or `finished=1` marks the item watched and clears the position.
9. **Path safety:** the HLS filename whitelist; the media path escape check; poster names, with content-sniffed uploads opened with `O_EXCL`; `safeNext` only redirects to local paths.
10. **htmx error handling:** `responseHandling` swaps 4xx/5xx HTML, which `app.js` relies on.
11. **Transcodes:** 503 with `Retry-After: 30` when busy; 60 s wait for the first segment (the player's timeout is 70 s); 3 min idle stop and 6 h cache; the transcode folder is emptied at startup.
12. **Migrations** stay numbered and transactional and are never edited once released; existing databases are at version 2.
13. **Shutdown** never closes the database while a worker is still running.

## 9. Decisions where the inputs disagreed

| Question | Options | Decision |
|---|---|---|
| Split `web` into subpackages now? | Adversarial: yes. Collaborative: feature structs first. | **Inside the package first** (phase 5); subpackages optional (phase 7). Subpackages force exporting every shared templ component. |
| One `Store` or repositories per entity? | Adversarial: narrow services. Collaborative: one `Store`. | **One `Store`, consumer interfaces.** Narrow visible dependencies without breaking single-transaction rules. |
| Separate domain types from `store` types? | Adversarial: yes. Collaborative: no. | **No domain package.** View models only where a template needs a different shape. |
| SQLite connection pool | External: `SetMaxOpenConns(1)`. Our design: unbounded pool. Research: bound the pool; 1 is safe but makes reads wait behind writes. | **Bounded pool of 8 with `_txlock=immediate`** (step 11). Writers queue on `busy_timeout`; readers keep WAL concurrency. Revisit only if the concurrency test or production shows `SQLITE_BUSY`. |
| `time.After` in polling loops | External: a leak, use a ticker. | **Not a leak since Go 1.23**; this module requires 1.27, which also removed `asynctimerchan`. |
| Shutdown API | External: `Start` + `Stop(ctx)`. Our design: `Run(ctx)`, unbounded wait. | **`Run(ctx) error` with a bounded wait** (step 26). One blocking call has a clear lifetime; the deadline keeps a stuck worker from hanging shutdown, and the store stays open if it expires. |
| Scanner event loop | External: run scans off the loop. | **Keep scans in the loop.** Scans are serialised by the scan lock, `Trigger` already coalesces, and after step 20 the lock no longer covers probing. Events that arrive during a scan wait in the fsnotify queue; if it overflows, the scanner rescans (step 20) and the periodic scan remains the backstop. |
| Route registration | External: three `register*Routes` methods. | **One table.** It is a list, already grouped by comments. |
| Removing a poster | Collaborative: keep `""` = "keep current". External: add `ClearPoster`. | **Add `RemovePoster`** with a checkbox (step 28). `""` as "unchanged" is the in-band pattern section 2.1 bans, and it blocks a real admin action. |
| Conversion of originals with image subtitles (PGS) or attachments | The adversarial review calls deleting them destructive. | **Open question** (section 10). Current behaviour stays until decided. |

## 10. Open questions for the owner

1. **Originals with image-based subtitles.** PGS and VobSub tracks can't go into MP4 or become `.srt`, so deleting the original loses them. Options: (a) delete anyway, as today; (b) keep the original when it has image subtitles; (c) a setting.
2. **Open registration.** Anyone who reaches the server can register, and the first visitor becomes admin. A setting such as `NEXUSPLAY_REGISTRATION=open|invite|closed` would close that for internet-facing installs.
3. **Renamed files** count as delete-plus-add, so progress, subtitles and votes are lost. Matching renames by size and duration is possible but adds complexity.
4. **Unused poster files.** Replacing or removing a poster leaves the old file in the poster folder. A sweep could delete files no row references; TMDB posters are shared by name, so it must check every row. Worth doing?

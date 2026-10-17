# NexusPlay readability and architecture overhaul

Status: proposed · Branch: `go-rewrite` at `dca1cd0` · October 2026

This document proposes how to make the Go rewrite easy to read and reason about. It builds on three reviews done for it:

- **Research**: Google's Go Style Guide (guide, decisions, best practices), Effective Go, Go Code Review Comments, and the Go release notes. Every rule below comes from there unless marked otherwise.
- **Adversarial review**: an exhaustive, evidence-based hunt for defects and readability problems. It measured the code with a `go/ast` tool, ran `go vet`, staticcheck, errcheck, race and coverage, ran the code-review skill, and reproduced the worst bugs on a scratch copy.
- **Collaborative review**: what to keep, a target package layout, and a migration path.

Where the reviews disagreed, this document makes the call and says why (section 9).

---

## 1. What is actually wrong

The complaint was: long functions, almost no documentation, unclear modules, no dependency injection, and functions that return `""` on failure. Measured against the code:

| Complaint | Measured | Verdict |
|---|---|---|
| Functions far too long | 235 production functions; median 12 lines; 13 over 40; longest `Scanner.Scan` at 98 | **Partly true.** Few functions are long, but the long ones do 4–9 jobs each, and they are the most important ones (scan, convert, transcode, admin save). |
| Almost no documentation | 149/151 exported identifiers documented; 11/11 package docs; **29%** of unexported code documented; **0** of 12 store lookups say they return `ErrNotFound`; no description of how the pieces fit together | **True where it matters.** The API reference is fine. Nothing explains how the scanner, converter, transcoder and web layer interact, or the contracts: errors, concurrency, lifecycle. Several comments are wrong. |
| Unclear modules | `web` imports 8 of 9 sibling packages and has 63 methods on one `Server`; `convert` and `library` call each other at runtime through concrete types; the "can a browser play this" rule exists in 3 versions that disagree | **True.** |
| No dependency injection | 0 named interfaces; 5 direct `exec.CommandContext` calls; TMDB URL hard-coded; zero-value structs with lazy `init()` | **True**, and it has consequences: the code that deletes your originals (`Converter.swap`) has no unit test. Coverage is `convert` 19.9%, `stream` 13.4%, `metadata` 0%. |
| Returning `""` on failure | 18 places encode failure or absence in a zero value or swallow an error, including `downloadPoster` with 6 silent failure paths | **True.** |

The reviews also found **real defects**, several of which can lose data or are security holes. These are fixed first, before any restructuring (section 7, phase 1).

| # | Defect | Severity | Proof |
|---|---|---|---|
| D1 | The converter checks for an existing `Movie.mp4` before an encode that can take hours. The later `rename` overwrites whatever is there. If you added your own `Movie.mp4` meanwhile, the DB update fails on the unique path and the cleanup **deletes your file**. | Critical | Reproduced |
| D2 | Open redirect after login: `next=/%09/evil.example` passes `safeNext`. Browsers strip the tab and land on `//evil.example`. | High | Reproduced (header) |
| D3 | A subtitle language tag inside a media file (`../../../../tmp/pwned`) makes the converter write `.srt` files outside the media folder. | High | Reproduced |
| D4 | One unreadable subfolder (a flaky NAS share) makes the scanner delete every row under it, with progress, subtitles, votes and metadata. The empty-folder guard only covers a completely empty library. | High | Read |
| D5 | Data race between `Scanner.Trigger` and `Run`'s lazy init. A scan triggered before `Run` starts is silently dropped. | High | Race detector |
| D6 | Shutdown doesn't wait for background workers. The database can be closed under a running file swap. | High | Read |
| D7 | The transcode janitor can delete the output folder of a newer transcode of the same video. | Medium | Read |
| D8 | The CSRF check parses the whole upload before the handlers apply their size limits. Oversize uploads get "invalid CSRF token" and the handler limits are no-ops. | Medium | Reproduced |
| D9 | About 10 handlers turn any database error into "Not found", unlogged. | Medium | Read |
| D10 | A 10-bit H.264 `.mp4` plays in no browser, but is never converted and is stream-copied when transcoded to HLS. | Medium | Read |
| D11 | Two queries re-list the media columns by hand and leave `AddedAt`/`PublishedAt` zero. | Medium (latent) | Reproduced |
| D12 | Home-page categories split by letter case ("Movies" and "movies" become two tabs). | Medium | Reproduced |
| D13 | Duplicate users and collections are detected by searching error text for "UNIQUE". | Medium | Read |
| D14 | Progress saving and subtitle votes don't check that the video is published (members can act on unpublished items by guessing IDs). `NaN`/`Inf` positions are accepted. | Low-medium | Read |
| D15 | Smaller items: CSRF token compared with `!=` (not constant-time); `q[:100]` and `reason[:2000]` can split a UTF-8 character; a test asserts `HashToken(a) != HashToken(a)` (can never fail); a test locks in "Blade Runner 2049" being parsed as title "Blade Runner", year 2049; N+1 queries on the home page; the converter status (polled every 3 s) loads every media row. | Low | Mixed |

## 2. Principles

These are the rules the overhaul applies everywhere. Each cites its source.

### 2.1 Errors: what to return instead of `""`

The instinct behind "return null, or a `Result<T>` like Kotlin" is right: a zero value meaning "it failed" is ambiguous and easy to ignore. Google's Go style guide calls this an **in-band error** and says not to use them ([decisions › In-band errors](https://google.github.io/styleguide/go/decisions#in-band-errors)). But Go already has the shapes Kotlin's types provide:

| Kotlin | Go | Use for |
|---|---|---|
| `Result<T>` | `(T, error)` | Anything that can fail. On error, `T` is the zero value and callers never look at it. |
| `T?` / `Optional<T>` | `(T, bool)` ("comma ok") | Absent, but not an error: cache and map lookups. Example: `Touch(id) (dir string, ok bool)`. |
| — | `(T, inserted bool, error)` | Two successful outcomes: "inserted" vs "already there". |
| — | `(T, error)` with a sentinel such as `store.ErrNotFound` | Absent, where callers usually propagate it. |

A generic `Result[T]` type is rejected. Go has no sum types or pattern matching, so `Result[T]` is a struct plus methods that panic. It defeats `errcheck` and the vet checks, puts `errors.Is`/`errors.As` one hop further away, and every standard-library boundary would still return `(T, error)`. Google's guide warns against error-handling frameworks built on generics ([decisions › Generics](https://google.github.io/styleguide/go/decisions#generics)), and the Go team stopped pursuing new error syntax in 2025. Returning `nil` from a `*string` only to signal failure is rejected for the same reason. Pointers stay where a value is genuinely optional data (for example `Viewer.User`).

Rules:

1. **Never encode failure in a zero value.** Never discard an error with `_ =` without a comment saying why that is safe.
2. **Make errors checkable:** sentinel errors (`store.ErrNotFound`) or error types (`*ffmpeg.ExitError`), checked with `errors.Is` / `errors.As`. Wrap with `%w` and context: `fmt.Errorf("probe %s: %w", path, err)`.
3. **Handle or return, never both.** Library code returns errors. Only the owner of a unit of work logs them, once: the HTTP error mapper (per request), the scan loop (per scan), the converter (per job), the transcode waiter (per transcode), and `main` (fatal startup). A component may downgrade an error to a warning only when the operation is optional, and it says so in a comment. Examples: poster download, sidecar import, deleting an original after a successful swap.
4. **Document the error contract** on every function that returns a sentinel: "returns ErrNotFound if …".

### 2.2 Functions

Google sets no line limit; the rule is clarity ([best practices › Function length](https://google.github.io/styleguide/go/best-practices)). In practice:

- A function does one job, named by its name. Long functions are split along their real seams: decode → validate → act → render in handlers; walk → import → enrich → prune in the scanner.
- Errors and edge cases are handled first, with guard clauses. Nesting stays at 3 levels or fewer.
- More than 4 parameters, or several adjacent parameters of the same type (`title, year, overview, poster string`), means a struct.

### 2.3 Documentation

- Every package doc says what the package owns, what it must not do, and who uses it.
- Every exported identifier is documented, and so is every unexported function that is not obvious ([Go doc comments](https://go.dev/doc/comment)). Every HTTP handler gets one line: route, who can call it, what it returns.
- Comments explain **why**, not what. The tricky invariants (converter swap order, CSRF model, HLS event playlists, the scan lock) get a full explanation where the code is.
- Contracts are written down: errors returned, concurrency safety, who must call what first, cleanup.
- A new `ARCHITECTURE.md` gives the map: packages, the life of a file, goroutines and locks, the data model, the HTTP layer, safety invariants.

### 2.4 Packages and interfaces

- **Consumer-defined, small interfaces** ([decisions › Interfaces](https://google.github.io/styleguide/go/decisions#interfaces)). The package that *uses* a dependency declares the 2–6 methods it calls. Producers return concrete types. No central `interfaces` or `ports` package, and no interface "for later".
- Interfaces only at real seams: external processes (ffmpeg/ffprobe), the network (TMDB), and cross-component calls (the scanner as seen by the converter; the converter and transcoder as seen by the web layer).
- **Not** interfaces: the clock (a `func() time.Time`), the filesystem (the code depends on real atomic-rename semantics; tests use `t.TempDir()`), the logger, config, and pure functions.
- Dependencies point one way; no runtime cycles through concrete types.
- One package, one responsibility. No `util` packages.

### 2.5 Dependency injection

- **Constructors with explicit dependencies**, wired by hand in one composition root (`internal/app`). Required collaborators go in a `Deps` struct, tunables in a `Config` struct. A missing dependency is a compile error, not a nil-pointer panic at the first scan.
- **No zero-value structs with lazy `init()`.** Constructors create channels and maps before anyone can use them. This removes the D5 race by construction.
- **Functional options only for test-only overrides of good defaults:** `metadata.WithBaseURL`, `metadata.WithHTTPClient`. Google prefers option structs in general ([best practices › Option structure](https://google.github.io/styleguide/go/best-practices)).
- **No DI framework** (wire, dig, fx). The graph has about 10 components; a 60-line composition root is clearer. Wire was archived in 2025.
- Conformance is asserted at compile time in the composition root: `var _ convert.Store = (*store.Store)(nil)`.

### 2.6 Concurrency and lifecycle

- Every background component exposes a blocking `Run(ctx context.Context) error`. The app runs them in an `errgroup`, waits for all of them on shutdown, and only then closes the database ([decisions › Goroutine lifetimes](https://google.github.io/styleguide/go/decisions#goroutine-lifetimes)).
- Each type documents its concurrency safety and which lock protects what.
- Hold locks around state, not around I/O. No filesystem work or process launch under a manager-wide mutex.

### 2.7 Tests

- Table-driven tests with `t.Helper()` and `cmp.Diff`; no assertion libraries.
- Real SQLite in a temp directory for store-backed tests. It is fast and exercises real SQL. Hand-written fakes only at the seams in 2.4: ffmpeg, TMDB, the scanner lock, and database *failures* the converter must survive.
- Fakes live next to their consumer in `_test.go` files. No generated mocks and no `mocks/` package.
- Integration tests keep proving what fakes cannot: that real ffmpeg accepts the arguments, the browser behaviour, and the screenshots.

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
| `internal/ffmpeg` | The only code that runs ffmpeg or ffprobe. `Tools{FFmpeg, FFprobe}` with `Probe(ctx, file) (Probe, error)` (one full stream list per call), `Run(ctx, args, RunOptions) error` (progress callback, low priority), `Start(ctx, args) (*Process, error)` (long-running HLS). Failures are `*ExitError{Err, LastLine}`. | Two ffprobe parsers (`library.Prober`, `convert.ProbeSource`), the double probe in `verify`, two process wrappers, two copies of `lastLine`. |
| `internal/media` | The browser-compatibility policy as pure functions: `IsVideo`, `MIMEType`, `Deliver(Format) Delivery` (`DirectPlay` or `HLS`), `NeedsConversion(Format) bool`, `BrowserSafeH264(pixFmt) bool`. | `stream.DirectPlayable`, `convert.NeedsConversion`, `library.SupportedExtensions`, `web.videoTypes`, and the 10-bit rule hidden in `MakePlan`. One table, one test, and D10 is fixed in one place. |

Changed packages:

| Package | Change |
|---|---|
| `store` | Stays **one `Store`**, split by file per entity. No per-entity repositories: publishing plus notifications, votes plus cue edits, and first-user-is-admin are single-transaction rules that repositories would have to break or re-plumb with a unit-of-work. Additions: a generic `queryAll` row helper and `scanMediaWith(row, extra...)`, which together fix D11 and remove about 10 copies of the `rows.Next` loop; unique-violation detection by SQLite error code; `inserted bool` results; a typed `ConvertStatus` with named transitions; every lookup documents `ErrNotFound`. |
| `library` | `NewScanner(Deps, Config)`. Declares its own `Store`, `Prober` and `Metadata` interfaces. `Scan` is split into `walk`, `importNew`, `importSidecars` and `removeMissing`. The scan lock covers only the database reconcile, not ffprobe or TMDB calls. Walk errors make `removeMissing` skip that subtree (D4). |
| `convert` | `New(Deps, Config)`. Declares `Store`, `Tools` and `Library` interfaces (`Exclusive`, `Trigger`), so it no longer imports `library`. A `Disabled` null object replaces the nil checks. The job is split into `prepare → encode → verify → commit`. `commit` re-checks the target under the lock and never overwrites a file it did not create (D1). Language tags are sanitised (D3). `CheckConverted` is a pure, unit-tested function. |
| `stream` | `NewManager(Starter, Config, log)`. `Touch` returns `(string, bool)`. `Janitor` becomes `Run`. Each job gets its own directory (D7). Filesystem and process work happens outside the lock. |
| `metadata` | `New(Config, log, ...Option)`. `fetchPoster` returns `(string, error)`, and the caller logs and keeps the rest of the info. Tested against `httptest`. Shows are looked up through `singleflight`. |
| `auth` | Grows from password hashing into identity. `Viewer` moves here from `web`. `Accounts` (register and login rules, the timing equalizer created at construction) and `Sessions` (create, look up, end, and expire in `Run`) move out of the handlers. |
| `config` | `LoadFrom(getenv func(string) string)`, table-driven, with `Validate()` covering every field (including `ScanInterval`). `Load()` calls `LoadFrom(os.Getenv)`. Tested. |
| `app` | Builds everything with constructors. `App.Run(ctx) error` runs the workers in an `errgroup`, serves HTTP, and on shutdown stops the server, waits for every worker, then closes the store (D6). The healthcheck reuses config defaults. |
| `web` | See 3.1. |

### 3.1 The web layer

`web` today is one flat package where every handler sees every dependency, and each handler mixes decoding, validation, business rules, error mapping and htmx-vs-page rendering.

Target:

- **Handlers return `error`.** One `Handle(fn)` adapter maps errors to responses in one place: `ErrNotFound` → 404, `*ValidationError` → 400 with the message, `ErrDuplicate` → 409, `stream.ErrBusy` → 503 + `Retry-After`, `*http.MaxBytesError` → 413, client gone → nothing, anything else → logged 500. This fixes D9 everywhere at once and removes 48 copies of `serverError` and 28 of `notFound`.
- **One `Respond` helper** decides between htmx fragment and full page, so every dialog keeps working as a standalone URL (progressive enhancement). It replaces 13 hand-written branches.
- **Form decoders** (`decodeMediaForm(r) (mediaForm, error)` and similar) read and validate input, and are table-tested with `httptest.NewRequest`.
- **Feature handler groups**, each with a constructor that takes only what it uses, declared as small consumer interfaces: `library` (browse, search, collections, subscriptions), `playback` (watch, file, HLS, progress), `subtitles` (VTT, reports, proposals, votes), `accounts` (login, register, logout), `admin` (control panel, media, collections, subtitles, conversions).
- **One route table and one middleware chain**, written in reading order with a `chain(...)` helper. The route table is the single best description of what the app does, so it stays in one place.
- **View models** move out of `.templ` files into Go files: `Page`, `Playback`, `Season`, `MediaForm`, `ConversionView` and the URL helpers. Go tooling can then see them.
- **Body limits** apply per route before CSRF. CSRF prefers the header token, maps an oversize body to 413 (D8), and compares tokens in constant time.

Feature groups are done **inside `package web` first**, as separate files and structs. Moving them into subpackages (`web/ui`, `web/httpx`, `web/<feature>`) is the last, optional step: it requires exporting every shared templ component, and the boundaries should settle before they are frozen into package names.

## 4. Patterns: adopted and rejected

| Pattern | Decision | Why |
|---|---|---|
| One ffmpeg runner shared by the transcoder and converter | **Adopt** | Process lifecycle, stderr capture, progress parsing and "cancelled vs failed" are written and tested once. |
| Delivery policy as an enum plus `switch` (`media.Deliver`) | **Adopt** | One place answers "how does this file play". A strategy interface would spread a two-case switch across three types. |
| Null object for disabled conversion (`convert.Disabled`) | **Adopt** | Removes nil checks, and avoids Go's typed-nil-interface trap once the field becomes an interface. |
| Named conversion states with explicit transitions | **Adopt**, no FSM library | `''` currently means both "queued" and "nothing to do", and "running"/"postponed" exist only in memory. One `stateOf` function answers what the control panel shows. |
| Error-returning handlers plus a central mapper | **Adopt** | See 3.1. |
| Form decoders separate from handlers | **Adopt** | Handlers read as five steps; decoding is unit-testable. |
| Generic `queryAll[T]` row helper | **Adopt** | Generics that remove real boilerplate, and the copy-paste has already caused a bug. |
| `singleflight` for TMDB show lookups | **Adopt** | Concurrent scans and admin saves no longer query the same show twice. |
| `Result[T]` / `Option[T]` types | Reject | Section 2.1. |
| Per-entity repositories plus unit of work | Reject | Breaks working single-transaction rules. |
| DI container (wire, dig, fx) | Reject | Section 2.5. |
| Scanner as a pipeline of `Step` interfaces | Reject | The steps have different inputs and different failure policies. Named sequential helpers read top to bottom. |
| Generic job framework shared by transcoder and converter | Reject | An on-demand cache with eviction and a DB-driven queue with retries share only "run ffmpeg", which the runner covers. |
| Event bus between scanner and converter | Reject | `scanner.OnAdded(conv.Wake)` is one grep-able line. |
| Filesystem abstraction (afero) | Reject | It would hide the atomic-rename and `O_EXCL` semantics the converter relies on. |
| A separate `domain` package mirroring `store` types | Reject | The store types are the domain types; copying them doubles field lists that already drift. Templates get view models only where a template needs a different shape. |

## 5. Function decomposition

Every production function over 40 lines, and its split:

| Function | Lines | Split into |
|---|---|---|
| `library.Scanner.Scan` | 98 | `walk(ctx) (tree, error)`, collecting videos, sidecars and failed directories; `importNew`, which applies the settle time, probes, inserts and enriches; `importSidecars`; `removeMissing(known, tree)`, which applies both the empty-folder and failed-subtree guards. The lock is held only for reconcile. |
| `library.ParsePath` | 70 | `parseEpisode(name)` (one loop over both episode patterns), `parseMovie(name)`, `showFromDirs(dir)`. |
| `convert.Converter.convert` | 62 | `prepare` (space check, probe, plan, target), `extractSubtitles`, `encode`, `verify` (via the pure `CheckConverted`), `commit`. |
| `web.saveMedia` | 62 | `decodeMediaForm`, `applyMediaForm` (collection and poster, with validation before any side effect), then save and redirect. |
| `stream.Manager.start` | 60 | `reuse`, `reserve` (capacity under the lock), `launch` (outside the lock), `finish`. |
| `subtitle.Parse` | 60 | A small `parser` struct with `line` and `flush` methods, plus `parseTiming`. |
| `config.Load` | 54 | Table-driven `LoadFrom` plus `Validate`. |
| `web.register` | 49 | `auth.Accounts.Register` (validation and creation), with the handler only decoding and rendering. |
| `web.collection` | 47 | `groupBySeason(eps) []Season`, plus a single `collectionView` query helper. |
| `web.uploadSubtitle` | 47 | `decodeSubtitleUpload(r) (lang string, cues []subtitle.Cue, err error)`. |
| `library.Scanner.Run` | 46 | The event loop delegates to `onEvent` and `rescanSoon`; there is no lazy `init`. |
| `web.hls` | 45 | `playlist` and `segment` handlers; segment waiting moves into `stream`. |
| `web.Server.Handler` | 58 | Stays one route table (acceptable as a list), with the middleware chain extracted. |

## 6. Documentation deliverables

1. `ARCHITECTURE.md`:
   - purpose and shape
   - the package map from section 3
   - the life of a file: scan → settle → probe → insert → enrich → sidecars → convert → swap → publish → watch → progress
   - goroutines and locks
   - the data model, and why media IDs survive conversion
   - the HTTP layer: routes, guards, middleware order, error mapping, CSRF
   - playback policy and HLS event playlists
   - safety invariants
   - error conventions
   - testing
   - how to add a feature
2. Package docs rewritten to say what each owns and what it must not do.
3. Full "why" comments on: the converter commit protocol, the CSRF model, HLS event playlists, the scan lock, the two "finished" rules (the player on `ended`, the server at 95%), and the `hx-headers` ↔ `player.js` CSRF coupling.
4. Error contracts on every function that returns a sentinel.
5. One line on every handler, and docs for every non-obvious unexported helper.
6. Fix the comments that are wrong:
   - The swap comments promise an automatic retry that doesn't happen.
   - `cacheForever` caches for one day.
   - `Media.Poster` falls back to the collection's poster.
   - The conversions panel implies conversion is opt-in, but it defaults to on.

## 7. Implementation plan

Each step compiles, keeps `go vet`, `go test -race ./...` and the integration suite (including screenshots) green, and is committed on its own. Risk rises down the list.

**Phase 1: data-safety and security fixes, each with a regression test written first.**

1. **D1:** the converter never overwrites an existing target.
   - When the target name differs from the source, the rename becomes a no-overwrite link-and-remove: `os.Link(tmp, target)` then `os.Remove(tmp)`. This fails with "exists" instead of replacing the file.
   - `commit` re-checks the target under the scan lock.
   - On a DB failure, only a file this job created is removed.
2. **D3:** language tags are sanitised to `[a-z0-9-]{1,12}`, else `und`. Every output path is checked to stay inside the video's folder.
3. **D2:** `safeNext` parses the URL. It rejects any scheme, host, backslash or control character, and rebuilds the path from parsed parts.
4. **D4:** walk errors are recorded per directory, and `removeMissing` skips rows under failed directories.
5. **D8:** the body limit runs before CSRF; oversize gets 413; the token is compared in constant time.
6. **D14:** progress and votes check that the media is visible to the user; non-finite positions are rejected.

**Phase 2: correctness fixes that need no restructuring.**

7. `queryAll` and `scanMediaWith`, fixing D11.
8. Unique violations detected by SQLite error code (D13); `errors.Is(sql.ErrNoRows)`; rune-safe truncation (D15); checked `RowsAffected`/`LastInsertId`.
9. Category normalisation on write, with case-insensitive grouping (D12).
10. `_txlock=immediate` for write transactions (decision in section 9).
11. Log every currently swallowed error: rehash, logout, `watcher.Add`, `Rel`/`Info` in the walk, and leftover cleanup.
12. Fix the test bugs: the tautological `HashToken` assertion; the Blade Runner case. A trailing number becomes the year only if it is no later than next year.

**Phase 3: new leaf packages (behaviour-preserving).**

13. `internal/ffmpeg`. Switch `library` and `convert` probing to it, with one probe in `verify`; switch both process wrappers.
14. `internal/media`. `stream`, `convert`, `library` and `web` use it; the old functions are deleted.
15. *(Behaviour change, isolated.)* Store `pix_fmt` (migration `0003`). `media.Deliver`, `NeedsConversion` and the HLS copy decision use `BrowserSafeH264`. Integration test with a 10-bit fixture (D10).

**Phase 4: constructors and seams, one package at a time, each with new unit tests.**

16. `metadata.New(Config, log, ...Option)`, `fetchPoster` returning `(string, error)`, `singleflight`. Tests against `httptest`.
17. `stream.NewManager(Starter, Config, log)`, `Touch` returning `(string, bool)`, per-job directories (D7), `Run`. Unit tests with a fake `Starter`.
18. `library.NewScanner(Deps, Config)` with consumer interfaces and `inserted bool` results; the `Scan`/`Run` split; no lazy init (D5). `TestScan` runs without ffprobe using a fake prober; an ffprobe-backed variant stays.
19. `convert.New(Deps, Config)` with consumer interfaces, a `FreeSpace` func, `Disabled`, the `prepare/encode/verify/commit` split, `CheckConverted` and named states. Swap unit tests:
    - DB failure
    - target exists
    - same-name file
    - original delete fails
    - no space
    - verification failure
    - cancellation
20. `config.LoadFrom` and `Validate`, with tests.
21. `auth.Accounts`, `auth.Sessions` and `Viewer` in `auth`.
22. `app`: workers with `Run(ctx) error` under an `errgroup`, ordered shutdown (D6), compile-time interface assertions, and a healthcheck that uses config.

**Phase 5: the web layer, one file per step.**

23. `Handle`/error mapper, `Respond`, and `PathID` returning an error. Convert `handlers_library.go`, then `handlers_watch.go`, `handlers_account.go` and `handlers_admin.go` (fixes D9 file by file).
24. Feature handler structs with consumer interfaces; view models out of `.templ`; form decoders; the remaining long handlers split.
25. Small fixes on the way: `RecentTiles` as one query; conversion status counted in SQL.

**Phase 6: documentation.**

26. `ARCHITECTURE.md`, package docs, why-comments, error contracts and handler lines (section 6). Written last, so it describes the final code. Each earlier step still documents what it adds.

**Phase 7 (optional):** move the web feature groups into subpackages once their boundaries have held for a while.

## 8. Behaviours that must not change

The adversarial review lists 36. These are the ones most likely to be broken by restructuring. Each gets a test before the code around it moves.

1. **Converter commit order:**
   1. Verify the MP4 (H.264, every planned audio track present, duration within max(2 s, 1%)).
   2. Put it in place.
   3. Point the media row at it.
   4. Only then delete the original.

   Same-name files are replaced atomically. The swap runs under the scan lock and ignores cancellation. The media ID is preserved, so progress, subtitles, votes and metadata carry over.
2. **Converter bookkeeping:**
   - Hidden temp names (`.x.nexusplay-converting.mp4`, `.x.srt.part`), cleaned at start only after the write check passes.
   - A shutdown is never recorded as a failure.
   - No disk space postpones a file for 30 minutes, and is not a failure.
   - Existing sidecar files are never overwritten.
3. **Scanner:**
   - 30 s settle time.
   - Empty-folder guard.
   - Hidden files skipped.
   - 5 s debounce, with `Chmod`-only events ignored.
   - `OnAdded` fires only when something was added.
   - Scanned metadata never overwrites admin edits.
4. **CSRF:**
   - GET, HEAD and OPTIONS are exempt.
   - Every other method passes the same-origin check (Origin, then Referer, then `Sec-Fetch-Site`).
   - Signed-in requests also need the token, in the header or the form field.
   - `/login` and `/register` skip the token check but not the origin check.
   - Session middleware runs before CSRF.
5. **Visibility and login redirects:**
   - Unpublished media returns 404, not 403, to non-admins.
   - `/media/*` and `/subtitles/*` get a bare 401 instead of a login redirect.
   - htmx requests get `HX-Redirect`.
6. **Accounts:**
   - Unknown-email logins still run argon2.
   - Weak hashes are re-hashed on login.
   - Session tokens are stored hashed.
   - The first user becomes admin, inside the insert transaction.
7. **Voting:**
   - One vote per user per subtitle line.
   - The proposer's vote counts.
   - Reaching the threshold replaces the line and clears every competing proposal.
8. **Progress:** 95% or `finished=1` marks the item watched and clears the position.
9. **Path safety:**
   - The HLS filename whitelist.
   - Media path escape check.
   - Poster names, with content-sniffed uploads opened with `O_EXCL`.
10. **htmx error handling:** `responseHandling` swaps 4xx/5xx HTML, which `app.js` relies on.
11. **Transcodes:**
    - 503 with `Retry-After: 30` when busy.
    - 60 s wait for the first segment (the player's timeout is 70 s).
    - 3 min idle stop and 6 h cache.
12. **Migrations** stay numbered and transactional; existing databases are at version 2.

## 9. Decisions where the inputs disagreed

| Question | Options | Decision |
|---|---|---|
| Split `web` into subpackages now? | Adversarial: yes, per feature. Collaborative: feature structs inside `web` first, subpackages last. | **Inside the package first** (phase 5); subpackages optional (phase 7). The handler boundaries are not proven yet, and subpackages force exporting every shared templ component. |
| One `Store` or repositories per entity? | Adversarial: narrow service interfaces. Collaborative: one `Store`, consumer interfaces. | **One `Store`, consumer interfaces.** Both reviews' goals, narrow visible dependencies, are met without breaking single-transaction rules. |
| Separate domain types from `store` types? | Adversarial: yes. Collaborative: no. | **No domain package.** Move view models out of templates, and build them in handlers only where a template needs a different shape. |
| SQLite connection pool | Common advice: one connection. Research: one connection is safe but makes reads queue behind writes. | **Keep the pool; use `_txlock=immediate`** so read-then-write transactions take the write lock up front and wait on `busy_timeout` instead of failing. Revisit with a concurrent-writers test if `SQLITE_BUSY` ever appears. |
| `time.After` in polling loops | Often described as a leak. | **Not a leak since Go 1.23**, and this module requires Go 1.27. Loops change only for readability. |
| Conversion when image subtitles (PGS) or attachments would be lost | The adversarial review flags deleting such originals as destructive. | **Open question for the owner** (section 10). Current behaviour stays until decided. |

## 10. Open questions for the owner

1. **Originals with image-based subtitles.** Blu-ray/DVD subtitle tracks (PGS, VobSub) can't go into MP4 or be turned into `.srt`, so deleting the original loses them for good. Options:
   - (a) delete anyway, as today
   - (b) keep the original when it has image subtitles
   - (c) a setting
2. **Open registration.** Anyone who reaches the server can register, and the very first visitor becomes admin. A setting such as `NEXUSPLAY_REGISTRATION=open|invite|closed` would close that, especially for internet-facing installs.
3. **Renamed files** currently count as delete-plus-add, so progress, subtitles and votes are lost. Matching renames by size and duration is possible but adds complexity.

# Style casebook

Every correction the owner has made to code in this repository, with what
was wrong, what replaced it, and the rule it became. Examples show taste
better than rules: before writing or reviewing Go here, read these, and ask
of every name and comment whether the owner would have flagged it the same
way.

Each case is written down the first time the owner raises it. The rule it
became is in `CLAUDE.md`; where a program can catch it, the check is in
`tools/housestyle`.

---

## 1. A doc comment that makes the reader guess

**Before**

```go
// episodeByNumber returns episode number of eps.
func episodeByNumber(eps []metadata.Episode, number int) (metadata.Episode, bool)
```

**Owner:** "what is 'eps', is it too hard to type 'episodes'? also i have to
kinda infer that 'number' and 'eps' are params?" Wanted something close to
Javadoc's `@param` / `@return`.

**What was wrong:** an abbreviation; the parameters are not identified as
parameters; nothing says what the `bool` means or what comes back when there
is no match.

**After**

```go
// [episodeByNumber] finds the episode with a given number in a season's
// episode list. It is the fallback when the file name has no episode title
// to match on, or [episodeByTitle] found no match for it.
//
// Parameters:
//   - [episodes]: every episode of one season, as returned by
//     [metadata.Client.Season], in any order.
//   - [number]: the episode number parsed from the file name, starting at 1.
//
// Returns:
//   - [metadata.Episode]: the first episode whose [metadata.Episode.Number]
//     equals [number], or a zero [metadata.Episode] if there is none.
//   - [bool]: [true] if an episode was found, [false] otherwise.
func episodeByNumber(episodes []metadata.Episode, number int) (metadata.Episode, bool)
```

**Rule:** every function lists every parameter and every return value.

## 2. Single-letter names

**Before:** `for _, e := range eps`

**Owner:** "'e'??? will it kill you to say 'episode'?"

**After:** `for _, episode := range episodes`

**Rule:** names are full words. Only `err`, `ctx`, `i` (a plain loop index),
`id`, and `w`/`r` in HTTP handlers stay short.

## 3. Types nobody can find

**Before:** `metadata.Episode` declared in `internal/metadata/tmdb.go`, next to
eight other declarations.

**Owner:** "where is `metadata.Episode`? why isn't it in
structs/metadata/episode, or some findable shit?"

**After:** `internal/metadata/episode.go`.

**Rule:** one file per exported type, named after the type.

## 4. Code that looks like English

**Owner:** "can you use some markup when referencing primitives or otherwise
referring to code and not English? like [nil] or {@code nil}"

**After:** every reference to code in a comment is in brackets: `[nil]`,
`[episodes]`, `[metadata.Episode]`.

## 5. Return values without types

**Owner:** "i would like returns to clarify the type being returned, too"

**After:** every `Returns:` entry starts with its type: `[bool]: …`,
`[int64] (start): …`.

## 6. Short receivers

**Before:** `func (h *adminHandlers) …`, `func (s *Store) …`

**Owner:** "i'm super confused by short receivers, i gotta stay on the
lookout every time for what letter that class has assigned?" Considered
`self`, then chose: "let's just do full names".

**After:** `func (handlers *adminHandlers) …`, `func (store *Store) …`

## 7. Data hardcoded in code

**Before**

```go
var ignoredWords = map[string]bool{"the": true, "a": true, "an": true, "and": true, "part": true, "pt": true, "of": true}
```

**Owner:** "you're hardcoding literals into code now? why isn't this some
allowlist being read from a file?" Then: "can't you just get the compiler to
figure it out? break at compile-time if the file is missing not on startup".

**After:** `internal/library/ignored_words.txt`, built in with `//go:embed`,
plus a test that the list is not empty.

## 8. A silent exception to a rule

**Before:** `// log records poster downloads that failed.` The leading name
was left out of brackets in every comment, following a Go convention the
owner was never told about.

**Owner:** "we talked about using [log], why was this missed"

**Rule:** no silent exceptions. If a rule has an exception, say so before
applying it. When one instance is flagged, find and fix every instance of
the same kind, and add a check if a program can catch it.

## 9. A name that lies, a comment that describes the mechanics

**Before**

```go
// [wordSet] reads a word list with one word per line.
//
// Parameters:
//   - [list]: the list's text. Blank lines and lines starting with "#" are
//     skipped, and spaces around each word are trimmed.
//
// Returns:
//   - [map[string]bool]: every word in [list], lowercased, mapped to [true].
func wordSet(list string) map[string]bool
```

**Owner:** "why use the word set? we're literally not returning a set. we're
returning a map. … tf is 'reads a word list with one word per line'? doesn't
tell you anything about what's being done with the 'read'. … tf are we
mapping these things to 'true' for? this feels like it should be a HashSet."

**What was wrong:** the name promises a set and returns a map (*Clean Code*
calls this disinformation). The comment describes the input format, not
what the caller gets or can do with it (*Effective Java* Item 56: describe
the contract). `map[string]bool` with `true` values is a Go idiom that only
works if the reader knows the trick.

**After**

```go
// [parseWordList] turns the text of a word list file, such as
// ignored_words.txt, into a [collections.Set] of its words, so callers can
// check whether a word is on the list with [collections.Set.Contains].
// …
func parseWordList(text string) collections.Set[string]
```

**Rule:** sets are `collections.Set`. A name says what the thing is; a
comment says what the caller gets and what it is for.

## 10. Standards that conflict without anyone saying so

**Owner:** "i asked you to apply google coding expectations and you kinda
failed miserably. what went wrong?"

**What was wrong:** Google's Go guide recommends short names, short
receivers and prose comments, the opposite of most of the owner's requests.
That conflict was never pointed out; requests were followed one at a time.

**Rule:** the reference standards are the Java-world ones listed in
`CLAUDE.md`. When a request conflicts with them, or with Go's usual style,
say so before doing it.

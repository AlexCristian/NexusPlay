# NexusPlay house style

**Write Go for a reader who knows Java and Kotlin well and Go's idioms
little.** For every name, comment and construct, ask: would that reader have
to look something up, guess, or know a Go trick to understand it? If so,
make it explicit.

Read the casebook before writing or reviewing code. It holds every
correction the owner has made, and shows the taste behind the rules better
than the rules do:

@docs/style-casebook.md

## Reference standards

Where this file is silent, follow these, in this order:

1. **This file and the casebook.**
2. ***Clean Code*, chapter 2, "Meaningful Names"** (Robert C. Martin), for
   naming: intention-revealing names, no mental mapping, no disinformation,
   searchable names, one word per concept.
3. ***Effective Java*, Item 56** (Joshua Bloch, 3rd edition), and Oracle's
   **"How to Write Doc Comments for the Javadoc Tool"**, for doc comments:
   describe the contract (what, not how), preconditions, postconditions,
   side effects, and add information beyond the name. Never restate it.
4. **Google Java Style Guide**, for file organization: one top-level type
   per file, named after it.
5. **Google Go Style Guide**, only for Go-specific matters the above do not
   cover: error handling, concurrency, package design, testing.

*Clean Code*'s advice to avoid comments does not apply here: doc comments
are required.

### Java to Go

| Java / Kotlin | Here |
|---|---|
| `HashSet<T>` | `collections.Set[T]` (`internal/collections`), never `map[T]bool` or `map[T]struct{}` |
| Resource files (`getResourceAsStream`) | A text file next to the code, built in with `//go:embed` |
| `@param name …` | A `Parameters:` list entry, `- [name]: …` |
| `@return …` | A `Returns:` list entry, `- [Type]: …` |
| `{@code nil}`, `{@link Foo}` | `[nil]`, `[Foo]` |
| One public class per file | One exported type per file, `snake_case.go` |
| `this` | The receiver, named after its type: `client`, `store`, `handlers` |

## Names

- Spell names out as full words: `episode`, `episodes`, `request`,
  `response`, `query`, `result`, `temporaryFile`, `posterDirectory`. Never
  abbreviate (`eps`, `e`, `res`, `q`, `p`, `tmp`, `temp`, `cfg`, `opts`,
  `dir`). `tools/housestyle/abbreviations.txt` lists the banned
  abbreviations.
- Only these stay short: `err` (an `error`), `ctx` (a `context.Context`),
  `i` (an `int` loop index), `id`, and `w`/`r` (an `http.ResponseWriter` and
  `*http.Request`).
- Method receivers use the type's name as a full word: `client` for
  `*Client`, `handlers` for `*adminHandlers`, `store` for `*Store`. Never one
  letter, `self` or `this`. If the name would shadow a type or package used
  in the method, use a more specific one (`tmdbClient`).
- A name says what the thing is. A function named `wordSet` returns a set; a
  `bool` named `found` is true when something was found.
- Test code follows the same naming rules.

## Files

- One file per exported type, named after it in snake_case:
  `metadata.Episode` lives in `internal/metadata/episode.go`, with its
  methods and constructor.
- Small unexported helper types used by only one type stay in that type's
  file.
- Sentinel errors go in `errors.go`; the package comment goes in `doc.go`.
- Test files are named after the file they test (`client_test.go`).

## Data

- Fixed data lists (word lists, allowlists, lookup tables of strings) go in
  a text file next to the code, built in with `//go:embed`, never as
  literals in code. A missing file then fails the build. Add a test that the
  embedded list loaded.

## Doc comments

Every function, method, type, struct field, constant and variable gets a doc
comment, exported or not. Test functions are exempt. Comments are full
sentences in plain words.

- **Describe the contract:** what the caller gets and what it is for, not
  the steps the code takes. State preconditions, side effects (arguments
  modified, files written, caches filled), concurrency safety, and limits.
- **Every claim must be true of the code.** Readers rely on comments instead
  of reading the code, so a wrong comment is worse than none. Check each
  claim against the code before writing it.
- **Brackets around every reference to code:** types, functions, methods,
  fields, parameters, local variables, builtins and predeclared values,
  including the name a comment starts with: `[episodeByNumber] finds…`,
  `[nil]`, `[true]`, `[error]`, `[Info.Poster]`, `[episodes]`. A package
  comment starts `Package [name]`; a command's starts `Command [name]`.
- **Quote string literals** (`""`, `"TV Shows"`). Numbers stay plain.
- **Functions list every parameter and every return value** in this format.
  Leave out `Parameters:` only when there are none:

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
```

- Each entry adds information beyond the name and type. Never write
  `[name]: the name`.
- Every return entry starts with its type. When two returns share a type,
  say which is which: `[int64] (start)`, `[int64] (end)`.
- Error entries name every sentinel a caller can match with `errors.Is`
  (`[store.ErrNotFound]`), and say what "otherwise" covers.

## Checks

Run all of these for every package you touch, and fix what they report:

```sh
gofmt -l .
go vet ./...
go run ./tools/housestyle/cmd/housestyle ./...
go test ./...
```

`tools/housestyle` checks the rules a program can check. Passing it does not
make a comment good: it cannot tell whether a comment is true or useful.

## Working with the owner

- **Say so first** when a request conflicts with this file, the reference
  standards, or Go's usual style.
- **No silent exceptions.** If a rule has an exception, state it.
- **When the owner flags a problem:** name the class of problem, find every
  instance in the codebase, fix them all, report the count, add a check to
  `tools/housestyle` if a program can catch it, and add the case to
  `docs/style-casebook.md`.
- **Split commits by how much review they need:** mechanical changes
  (renames, file moves; the compiler and tests prove them), documentation,
  and behavior changes. Behavior changes always go in their own commits.
- **Do renames with a type-checked tool** (`gopls rename`), not text
  substitution.

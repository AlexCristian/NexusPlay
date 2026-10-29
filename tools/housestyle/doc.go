// Package [housestyle] is a Go analyzer that enforces the NexusPlay house
// style written down in CLAUDE.md at the repository root. It checks the
// rules no published linter covers:
//
//   - Names are full words: no abbreviations from abbreviations.txt, and
//     nothing shorter than three letters except the names in short_names.txt.
//   - Every function, type, struct field, interface method, constant and
//     variable has a doc comment that starts with its name in brackets, such
//     as "[Client] talks to…".
//   - A function's or interface method's doc comment lists every parameter
//     under "Parameters:" and every return value under "Returns:", each
//     return starting with its type in brackets.
//   - A function that returns a package-level error variable, such as
//     [store.ErrNotFound], names it in brackets in its doc comment.
//   - Comments write exported and camelCase names, and the words in
//     code_words.txt such as "nil", in brackets.
//   - Every exported type, and its methods, live in a file named after it.
//   - Fixed lists of strings are not written as literals in code.
//   - Sets use [collections.Set], not a map to [bool] or "struct{}".
//
// Generated files are not checked. Test files get only the naming check.
package housestyle

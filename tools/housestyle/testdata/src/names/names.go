// Package [names] holds declarations for the naming check.
package names

import (
	"context"
	"go/ast"
	"go/token"
	"net/http"
	"sync"
)

// [holder] has fields with good and bad names.
type holder struct {
	// [Mutex] is embedded, so its name comes from its type.
	sync.Mutex
	// [counter] is embedded from this package.
	counter
	// [Expr] is embedded; its type's name has an abbreviation, but the name
	// is not chosen here.
	ast.Expr
	// [Pos] is embedded behind a pointer.
	*token.Pos
	// [posterDir] abbreviates "directory".
	posterDir string // want "name .posterDir. uses the abbreviation .dir.: write .directory. instead"
	// [ID] is an allowed short name of any type.
	ID int64
	// [URLs] is a plural acronym, one word.
	URLs []string
	// [showIDsByName] has a plural acronym inside it.
	showIDsByName map[string]int64
}

// [counter] counts.
type counter int

// [Increment] has a one-letter receiver.
func (c *counter) Increment() { // want "name .c. is too short: spell it out as a full word"
	*c++
}

// [Reset] has a receiver named after its type.
func (counter *counter) Reset() {
	*counter = 0
}

// [Handle] uses the allowed HTTP names with the right types.
//
// Parameters:
//   - [w]: where the response is written.
//   - [r]: the request.
func Handle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_ = ctx
	for i := range 3 {
		_ = i
	}
	for i, value := range []string{"one"} {
		_, _ = i, value
	}
	w.WriteHeader(http.StatusOK)
}

// [WrongTypes] uses the allowed short names with the wrong types.
//
// Parameters:
//   - [w]: not a response writer.
//   - [r]: not a request.
//   - [ctx]: not a context.
//   - [err]: not an error.
func WrongTypes(w string, r int, ctx string, err bool) { // want "name .w. is only allowed for a value of type net/http.ResponseWriter" "name .r. is only allowed for a value of type .net/http.Request" "name .ctx. is only allowed for a value of type context.Context" "name .err. is only allowed for a value of type error"
	var i int64 // want "name .i. is only allowed for a value of type int"
	_, _, _, _, _ = w, r, ctx, err, i
}

// [RightTypes] uses the allowed short names with the right types.
//
// Parameters:
//   - [ctx]: a context.
//
// Returns:
//   - [error]: [ctx]'s error.
func RightTypes(ctx context.Context) error {
	err := ctx.Err()
	return err
}

// [Abbreviations] declares abbreviated local names.
func Abbreviations() {
	tmpFile := "" // want "name .tmpFile. uses the abbreviation .tmp.: write .temporary. instead"
	eps := 0      // want "name .eps. uses the abbreviation .eps.: write .episodes. instead"
	resp := 0     // want "name .resp. uses the abbreviation .resp.: write .response. instead"
	ok := true    // want "name .ok. is too short"
	_, _, _, _ = tmpFile, eps, resp, ok
	subtitle, extension, conversion, subtitleExtension := 0, 0, 0, 0
	_, _, _, _ = subtitle, extension, conversion, subtitleExtension
	subFile := 0    // want "name .subFile. uses the abbreviation .sub.: write .subtitle. instead"
	cmd := 0        // want "name .cmd. uses the abbreviation .cmd.: write .command. instead"
	fileExt := 0    // want "name .fileExt. uses the abbreviation .ext.: write .extension. instead"
	timeoutSec := 0 // want "name .timeoutSec. uses the abbreviation .sec.: write .seconds. instead"
	_, _, _, _ = subFile, cmd, fileExt, timeoutSec
}

// [Generic] has type parameters, which may be short.
//
// Parameters:
//   - [values]: the values.
//   - [v]: a value whose type is a type parameter; the value's name is
//     still checked.
//
// Returns:
//   - [K]: the zero key.
func Generic[K comparable, V any](values map[K]V, v V) K { // want "name .v. is too short"
	var key K
	return key
}

// [fetcher] is an interface whose method parameters are checked.
type fetcher interface {
	// [Fetch] fetches.
	//
	// Parameters:
	//   - [u]: the address.
	Fetch(u string) // want "name .u. is too short"
}

// [srcDirectory] uses an abbreviation in a package-level name.
var srcDirectory = "" // want "name .srcDirectory. uses the abbreviation .src.: write .source. instead"

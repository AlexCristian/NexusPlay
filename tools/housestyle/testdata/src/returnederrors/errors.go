package returnederrors

import "errors"

// [ErrNoMatch] means nothing matched.
var ErrNoMatch = errors.New("no match")

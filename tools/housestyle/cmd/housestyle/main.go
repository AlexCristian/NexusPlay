// Command [housestyle] checks Go packages against the NexusPlay house style
// in CLAUDE.md. Run it like go vet:
//
//	go run ./tools/housestyle/cmd/housestyle ./...
//
// It exits with status 3 if it finds problems.
package main

import (
	"github.com/AlexCristian/NexusPlay/tools/housestyle"
	"golang.org/x/tools/go/analysis/singlechecker"
)

// [main] runs [housestyle.Analyzer] on the packages named on the command
// line.
func main() {
	singlechecker.Main(housestyle.Analyzer)
}

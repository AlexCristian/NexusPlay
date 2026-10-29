// Package [datafile] reads the small text files that hold this program's
// fixed data, such as word lists and lookup tables. The files are built into
// the program with "//go:embed", so a missing file fails the build.
//
// Every data file uses the same format: one entry per line, blank lines and
// lines starting with "#" skipped, and spaces around each entry trimmed.
package datafile

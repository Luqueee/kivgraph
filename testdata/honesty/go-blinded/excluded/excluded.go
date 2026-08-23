//go:build kivgraph_never

// Package excluded is guarded by a build tag no load satisfies, which is how a
// repository ends up with a scope the index could not read at all. `go list`
// answers "build constraints exclude all Go files" and the pass records
// PACKAGE_NOT_BUILDABLE with no file.
package excluded

// Excluded is declared here and nowhere the graph can see.
func Excluded() string { return "excluded" }

// Shadow shares its name with nothing else in the corpus, so a lookup for it
// finds no declaration while the source has one.
func Shadow() string { return Excluded() }

// Package rustsysroot holds one test and no code: the pass that indexes the
// Rust standard library as a synthetic provider.
//
// It is a package of its own because of what it costs. That pass runs the
// analyzer over the whole library workspace and took 84 s of the 138 s the
// tests of internal/indexer needed, and a Go test binary runs its tests one
// after another. internal/indexer was the slowest package of every suite in
// CI, so that one test decided how long the workflow took. Here it runs beside
// the others instead of ahead of them.
//
// The test uses only what internal/indexer exports, which is what lets it
// live outside.
package rustsysroot

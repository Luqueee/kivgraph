package agenthook

import "github.com/Luqueee/kivgraph/internal/config"

// ExtensionsFor is every extension the named languages are written in.
//
// It is a thin name over config.SourceExtensions rather than a table of its
// own. This package used to carry the second copy of that mapping and the
// watcher carried the first, and the two disagreed: the watcher knew Go,
// TypeScript and Rust, so a Python or Dart repository's sources were invisible
// to it. One table answers both now, beside the SupportedLanguages that decides
// what a configuration may declare in the first place.
func ExtensionsFor(languages []string) []string {
	return config.SourceExtensions(languages)
}

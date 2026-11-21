package agenthook

import (
	"slices"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
)

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

// namesLanguageType reports whether an extension spells a language rather than
// a file type. A type filter reaches the gate as a glob -- `--type rust` as
// `*.rust` -- and no file ends that way: it is a search over that language's
// code, whatever the spelling says.
func namesLanguageType(extension string) bool {
	languages := config.SupportedLanguages()
	return slices.Contains(languages, strings.TrimPrefix(extension, ".")) &&
		!slices.Contains(config.SourceExtensions(languages), extension)
}

package config

import (
	"path/filepath"
	"sort"
	"strings"
)

// languageExtensions is what each supported language is written in.
//
// It lives beside SupportedLanguages because that is where the vocabulary a
// repository may declare is decided, and every consumer that has to turn one of
// those names into a set of files was previously answering the question for
// itself. Two of them disagreed: the watcher knew Go, TypeScript and Rust and
// nothing else, so a change to a `.py` or a `.dart` file in a repository this
// build indexes was not a source change to it.
//
// The keys are every spelling `SupportedLanguages` accepts, because that is
// what a configuration is allowed to say and a lookup that only knew the
// canonical name would silently answer nothing for the alias.
var languageExtensions = map[string][]string{
	"go":         {".go"},
	"typescript": {".ts", ".tsx", ".mts", ".cts"},
	"ts":         {".ts", ".tsx", ".mts", ".cts"},
	"javascript": {".js", ".jsx", ".mjs", ".cjs"},
	"js":         {".js", ".jsx", ".mjs", ".cjs"},
	"node":       {".js", ".jsx", ".mjs", ".cjs"},
	"rust":       {".rs"},
	"rs":         {".rs"},
	"python":     {".py", ".pyi"},
	"py":         {".py", ".pyi"},
	"dart":       {".dart"},
	"java":       {".java"},
}

// SourceExtensions is every file extension the named languages are written in,
// lowercase and sorted, with duplicates removed.
//
// A language it does not recognise contributes nothing rather than everything:
// a configuration naming one is refused by validation long before this, and
// answering with the whole set would make a typo index the world.
func SourceExtensions(languages []string) []string {
	seen := map[string]struct{}{}
	for _, language := range languages {
		for _, extension := range languageExtensions[strings.ToLower(strings.TrimSpace(language))] {
			seen[extension] = struct{}{}
		}
	}
	extensions := make([]string, 0, len(seen))
	for extension := range seen {
		extensions = append(extensions, extension)
	}
	sort.Strings(extensions)
	return extensions
}

// SourceExtensionSet is SourceExtensions as a set, for a caller that asks about
// one path at a time.
//
// The watcher asks once per file of every scan, so it builds this once per
// repository rather than walking a switch on every question.
func SourceExtensionSet(languages []string) map[string]struct{} {
	extensions := SourceExtensions(languages)
	set := make(map[string]struct{}, len(extensions))
	for _, extension := range extensions {
		set[extension] = struct{}{}
	}
	return set
}

// HasSourceExtension reports whether a path is written in one of the languages
// a set was built from.
func HasSourceExtension(set map[string]struct{}, path string) bool {
	_, ok := set[strings.ToLower(filepath.Ext(path))]
	return ok
}

package agenthook

import "strings"

// languageExtensions is what each declared language is written in.
//
// A repository's configuration names languages, and a glob names an extension,
// so something has to translate between them before the gate can say whether
// `**/*.go` reaches indexed code.
//
// internal/watcher/reconcile.go carries a second, narrower copy of this
// mapping -- it knows Go, TypeScript and Rust, and not the Python or Dart that
// `config.SupportedLanguages` accepts. The two should be one table. Until they
// are, this is the one the gate reads, and being the wider of the two only ever
// costs a refusal the narrower one would have skipped.
var languageExtensions = map[string][]string{
	"go":         {".go"},
	"typescript": {".ts", ".tsx", ".mts", ".cts"},
	"ts":         {".ts", ".tsx", ".mts", ".cts"},
	"javascript": {".js", ".jsx", ".mjs", ".cjs"},
	"js":         {".js", ".jsx", ".mjs", ".cjs"},
	"rust":       {".rs"},
	"rs":         {".rs"},
	"python":     {".py", ".pyi"},
	"py":         {".py", ".pyi"},
	"dart":       {".dart"},
}

// ExtensionsFor is every extension the named languages are written in.
func ExtensionsFor(languages []string) []string {
	seen, extensions := map[string]bool{}, []string{}
	for _, language := range languages {
		for _, extension := range languageExtensions[strings.ToLower(strings.TrimSpace(language))] {
			if !seen[extension] {
				seen[extension] = true
				extensions = append(extensions, extension)
			}
		}
	}
	return extensions
}

package facts

import (
	"path"
	"strings"
)

// ModuleSymbolKind is the kind of the synthetic symbol that stands for a
// file's own top level scope.
//
// A use needs a declaration on both ends to become an edge, and some uses have
// none: a call in a top level statement, and a call inside an anonymous
// function passed as an argument -- the body of `it(...)` or `beforeEach(...)`
// in a test, a callback handed to a router, a closure in an initialiser. The
// checker resolves those targets perfectly well; what is missing is a source.
//
// Dropping them is not a small loss. Measured on `packages/core` of the workspace
// monorepo: 98 of 14100 uses in ordinary source, all of them top level
// statements in bootstrap files, and 38 of 38 uses in a test file -- every call
// a `vitest` file makes, because the idiom puts all of them inside a callback.
//
// So the file's own scope is a declaration like any other. The symbol covers
// the whole file, which is what makes it the loosest possible owner: anything
// narrower wins, and it is only reached when nothing else contains the use.
// The convention is the one `internal/dartloader` already established for a
// Dart library, extended to the languages that had no name for it.
const ModuleSymbolKind = "module"

// moduleSymbolExtensions are stripped from a module's qualified name. The
// stable key disambiguates on the path, so two files that differ only in
// extension do not collide; stripping is for readability alone.
var moduleSymbolExtensions = []string{
	".d.ts", ".ts", ".tsx", ".mts", ".cts",
	".js", ".jsx", ".mjs", ".cjs",
	".go", ".rs", ".dart", ".py", ".java",
}

// moduleQualifiedName names a file's module scope the way an import would:
// `src/shared/retry.ts` becomes `src.shared.retry`. It is derived from the
// repository-relative path, so it carries no machine and no absolute root.
func moduleQualifiedName(relative string) string {
	trimmed := path.Clean(strings.ReplaceAll(relative, "\\", "/"))
	trimmed = strings.TrimPrefix(trimmed, "./")
	for _, extension := range moduleSymbolExtensions {
		if strings.HasSuffix(trimmed, extension) {
			trimmed = strings.TrimSuffix(trimmed, extension)
			break
		}
	}
	return strings.ReplaceAll(trimmed, "/", ".")
}

// moduleSymbolName is what a reader sees in an answer: the file's base name.
func moduleSymbolName(relative string) string {
	return path.Base(path.Clean(strings.ReplaceAll(relative, "\\", "/")))
}

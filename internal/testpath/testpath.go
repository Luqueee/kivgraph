// Package testpath holds the one rule that says a path is a test file.
//
// It is a package of its own because two sides have to agree on it: a loader
// that leaves tests out of the graph when `include_tests` is false, and the
// tools that drop them from an answer under `exclude_tests`. With two rules a
// file could be in the graph as production code and be filtered as a test.
package testpath

import (
	"path"
	"path/filepath"
	"strings"
)

// Is says whether a repository-relative path is a test file by the
// conventions of the languages the graph indexes: a name or a directory. A Rust
// test module inside a source file, which carries no such mark, is not seen.
func Is(filePath string) bool {
	normalized := strings.ToLower(filepath.ToSlash(filePath))
	base := path.Base(normalized)
	stem := strings.TrimSuffix(base, path.Ext(base))
	switch {
	case strings.HasSuffix(base, "_test.go"), strings.HasSuffix(base, "_test.py"), strings.HasSuffix(base, "_test.dart"),
		strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"), base == "conftest.py",
		strings.Contains(base, ".test."), strings.Contains(base, ".spec."),
		strings.HasSuffix(stem, "test") && (strings.HasSuffix(base, ".java") || strings.HasSuffix(base, ".cs")),
		strings.HasSuffix(stem, "tests") && (strings.HasSuffix(base, ".java") || strings.HasSuffix(base, ".cs")):
		return true
	}
	for _, component := range strings.Split(path.Dir(normalized), "/") {
		switch component {
		case "test", "tests", "__tests__", "spec", "specs":
			return true
		}
	}
	return false
}

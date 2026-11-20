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
	slashed := filepath.ToSlash(filePath)
	normalized := strings.ToLower(slashed)
	base := path.Base(normalized)
	switch {
	case strings.HasSuffix(base, "_test.go"), strings.HasSuffix(base, "_test.py"), strings.HasSuffix(base, "_test.dart"),
		strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py"), base == "conftest.py",
		strings.Contains(base, ".test."), strings.Contains(base, ".spec."),
		isTestClass(path.Base(slashed)):
		return true
	}
	directory := path.Dir(normalized)
	for _, component := range strings.Split(directory, "/") {
		switch component {
		case "test", "tests", "__tests__", "spec", "specs":
			return true
		}
	}
	// Maven's integration tests. `it` alone is also a locale and a pronoun, so
	// it counts only under `src`, which is where the convention puts it.
	return strings.Contains("/"+directory+"/", "/src/it/")
}

// isTestClass reads the class-per-file convention of Java and C#: `ServiceTest`,
// `ServiceTests`. The capital is the word boundary, and it is what keeps
// `Latest.java` and `Contest.cs` out -- a comparison that folds case cannot
// tell a suffix from the end of another word.
func isTestClass(base string) bool {
	extension := path.Ext(base)
	if extension != ".java" && extension != ".cs" {
		return false
	}
	stem := strings.TrimSuffix(base, extension)
	return strings.HasSuffix(stem, "Test") || strings.HasSuffix(stem, "Tests")
}

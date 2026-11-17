package indexer

import (
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/dartloader"
)

// dartProviderRoots are the roots of the registered repositories whose files a Dart
// unit can read: the ones that hold a package its package config names, which is all
// the analysis server reads besides the repository itself, and the ones its facts
// name a target in.
//
// The loader is handed every registered repository as a provider, whatever language
// it was registered under, and the facts of a consumer name symbols of the one a
// reference lands in. The semantic registry digests the trees of the repositories
// that declare a semantic language, so a provider registered under another one could
// change, or be registered after the consumer was analysed, without the consumer's
// entry noticing: measured, the consumer was served with the old symbol.
//
// The unit's address takes the first of them, which are known before it is analysed,
// so a provider that appears is another address; the entry records the second as
// inputs to check when it is served.
func dartProviderRoots(unit analysisUnit, options FullOptions, composed map[string]composedTarget) []string {
	own := unit.repository.RealPath
	if own == "" {
		own = unit.repository.Path
	}
	packages := dartloader.PackageRoots(own, options.DartPackageConfig)
	for index, root := range packages {
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			packages[index] = resolved
		}
	}
	named := make(map[string]struct{}, len(composed))
	for _, target := range composed {
		named[target.Repository] = struct{}{}
	}
	roots := make([]string, 0)
	for _, repository := range options.Repositories {
		if repository.Name == unit.repository.Name {
			continue
		}
		root := repository.RealPath
		if root == "" {
			root = repository.Path
		}
		root = filepath.Clean(root)
		_, composedInto := named[repository.Name]
		if composedInto || holdsAnyOf(root, packages) {
			roots = append(roots, root)
		}
	}
	sort.Strings(roots)
	return roots
}

// holdsAnyOf reports whether any of the paths is the root or inside it. A directory
// that merely starts with the same characters is not inside it.
func holdsAnyOf(root string, paths []string) bool {
	for _, path := range paths {
		if path == root || strings.HasPrefix(path, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

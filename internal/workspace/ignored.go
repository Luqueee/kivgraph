package workspace

import (
	"context"
	"io/fs"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
)

// ignoredWalkBudget bounds the entries read below one ignored directory to
// find out whether it holds source. One that is larger is taken to.
const ignoredWalkBudget = 4000

// ignoredSources lists, repository-relative and sorted, the paths Git ignores
// that hold source of a registered language.
//
// What a repository ignores is what a build writes: the route types React
// Router generates, the `.next` of a Next application, a `build` that is a
// copy of `src`. A project that includes them in its `tsconfig.json` -- and
// the generated types must be, to type-check -- had them indexed: every
// declaration of the copy made its name ambiguous, and an edit of nothing
// changed the graph. They become exclusions, which every loader, the watcher
// and the freshness check already read.
//
// A path that holds no source is not listed. The list is part of what a pass
// compares to know that the sources moved, and a log directory that comes and
// goes would make every pass a stale one.
//
// A failure of Git lists nothing, which is what was done before.
func ignoredSources(ctx context.Context, git gitRunner, root string, languages []string) []string {
	output, err := git(ctx, root, "ls-files", "--others", "--ignored", "--exclude-standard", "--directory")
	if err != nil || output == "" {
		return nil
	}
	// A TypeScript project reads the JavaScript beside it, and a build of one
	// is JavaScript.
	if slices.Contains(languages, "typescript") {
		languages = append(slices.Clone(languages), "javascript")
	}
	extensions := config.SourceExtensions(languages)
	ignored := make([]string, 0)
	for _, line := range strings.Split(output, "\n") {
		entry := strings.TrimSuffix(strings.TrimSpace(line), "/")
		// Git quotes a path it had to escape, and an exclusion is a pattern:
		// a name with a metacharacter would mean something else.
		if entry == "" || strings.ContainsAny(entry, "\"*?[]{}\\") || entry == ".." || strings.HasPrefix(entry, "../") {
			continue
		}
		// No walk enters these, ignored or not.
		if segments := strings.Split(entry, "/"); slices.Contains(segments, "node_modules") || slices.Contains(segments, ".git") {
			continue
		}
		if holdsSource(filepath.Join(root, filepath.FromSlash(entry)), extensions) {
			ignored = append(ignored, entry)
		}
	}
	sort.Strings(ignored)
	return ignored
}

// holdsSource reports whether path is, or holds, a file with one of the
// extensions.
func holdsSource(path string, extensions []string) bool {
	budget := ignoredWalkBudget
	found := false
	_ = filepath.WalkDir(path, func(current string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if budget--; budget < 0 {
			found = true
			return filepath.SkipAll
		}
		if entry.IsDir() {
			if name := entry.Name(); current != path && (name == "node_modules" || name == ".git") {
				return filepath.SkipDir
			}
			return nil
		}
		if slices.Contains(extensions, strings.ToLower(filepath.Ext(current))) {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}

// IgnoredSources lists, as absolute paths, what the registry left out of the
// repository because Git ignores it, as CacheDirectories lists the tagged
// directories.
func IgnoredSources(_ context.Context, repository Repository) ([]string, error) {
	root := repository.RealPath
	if strings.TrimSpace(root) == "" {
		root = repository.Path
	}
	paths := make([]string, 0, len(repository.Ignored))
	for _, entry := range repository.Ignored {
		paths = append(paths, filepath.Join(root, filepath.FromSlash(entry)))
	}
	return paths, nil
}

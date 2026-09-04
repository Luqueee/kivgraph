package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// unclaimedOutputDirectoryNames are generated-output directory names pruned
// from the unclaimed walk on top of what discovery already prunes. A project
// that declares an "outDir" gets it excluded by its own configuration, but a
// repository whose build writes to a conventional directory without declaring
// it would otherwise offer its output as source nobody claims.
var unclaimedOutputDirectoryNames = map[string]struct{}{
	"dist":  {},
	"build": {},
}

// UnclaimedTypeScriptSources returns the absolute, sorted, duplicate-free
// .ts and .tsx files below the repository root that no discovered project
// owns.
//
// A file no project's "files"/"include" reaches belongs to no program: the
// compiler never type-checks it, so the graph cannot see it and nothing
// reports it absent. That is the set this function names, and it is named
// against the same resolution the project graph uses -- resolveTypeScriptConfig
// followed by resolveTypeScriptSources -- so a file is unclaimed here exactly
// when the compiler would leave it out of every program.
//
// Three kinds of file are deliberately not in it:
//
//   - A declaration file. It declares nothing a caller needs, and a `.d.ts`
//     that no project claims describes an artifact whose source is elsewhere.
//   - Anything below an installed-dependency or generated-output directory,
//     or below a path the repository's own exclusions name.
//   - Anything a project excludes. An "exclude" entry is a declaration about
//     that tree, not a gap in the index, so honouring it here is what keeps
//     the two readings of the same configuration from disagreeing.
func UnclaimedTypeScriptSources(
	ctx context.Context,
	repository Repository,
	discovery TypeScriptDiscovery,
) ([]string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	root := filepath.Clean(repositoryRootPath(repository))
	if err := validateExclusionPatterns(root, repository.Exclusions); err != nil {
		return nil, fmt.Errorf("validate TypeScript exclusions: %w", err)
	}

	claimed := make(map[string]struct{})
	excludePatternSegments := make([][]string, 0, len(discovery.Projects)*4)
	for _, project := range discovery.Projects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parsed, err := resolveTypeScriptConfig(project.ConfigPath, root)
		if err != nil {
			return nil, fmt.Errorf("resolve project %s: %w", project.ConfigPath, err)
		}
		sources, err := resolveTypeScriptSources(parsed, root)
		if err != nil {
			return nil, fmt.Errorf("resolve sources of project %s: %w", project.ConfigPath, err)
		}
		for _, source := range sources {
			claimed[source] = struct{}{}
		}
		excludePatternSegments = append(excludePatternSegments,
			splitGlobPatterns(effectiveTypeScriptExcludePatterns(parsed))...)
	}

	unclaimed := make([]string, 0)
	if err := walkUnclaimedTypeScriptFiles(ctx, root, root, repository.Exclusions, func(filePath string) {
		if _, owned := claimed[filePath]; owned {
			return
		}
		if isPathOrAncestorExcluded(filePath, root, excludePatternSegments) {
			return
		}
		unclaimed = append(unclaimed, filePath)
	}); err != nil {
		return nil, err
	}
	sort.Strings(unclaimed)
	return unclaimed, nil
}

// walkUnclaimedTypeScriptFiles visits every regular .ts or .tsx file below
// current, skipping declaration files, symlinks, the directories discovery
// already prunes, the repository's own exclusions and generated output.
func walkUnclaimedTypeScriptFiles(
	ctx context.Context,
	base, current string,
	exclusions []string,
	visit func(string),
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(current)
	if err != nil {
		return fmt.Errorf("read directory %q: %w", current, err)
	}
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		// A symlink could reintroduce a cycle or point outside the
		// repository, exactly as DiscoverTypeScript already reasons.
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		entryPath := filepath.Join(current, entry.Name())
		isDirectory := entry.IsDir()
		excluded, err := isDiscoveryExcluded(base, entryPath, entry.Name(), isDirectory, exclusions)
		if err != nil {
			return fmt.Errorf("check exclusion for %q: %w", entryPath, err)
		}
		if excluded {
			continue
		}
		if isDirectory {
			if _, output := unclaimedOutputDirectoryNames[entry.Name()]; output {
				continue
			}
			if err := walkUnclaimedTypeScriptFiles(ctx, base, entryPath, exclusions, visit); err != nil {
				return err
			}
			continue
		}
		if !isUnclaimedSourceName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect file %q: %w", entryPath, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		visit(entryPath)
	}
	return nil
}

// isUnclaimedSourceName reports whether name is a TypeScript source file that
// can carry a declaration a caller reaches. A ".d.ts" is excluded: it declares
// the shape of an artifact and never the code behind it.
//
// The JavaScript family is deliberately absent. Whether a ".mjs" is a source
// at all is the project's "allowJs" to decide, and this walk knows no
// project: offering one to a program that does not accept it produces a file
// the engine resolves nothing for.
func isUnclaimedSourceName(name string) bool {
	if strings.HasSuffix(name, ".d.ts") {
		return false
	}
	return strings.HasSuffix(name, ".ts") || strings.HasSuffix(name, ".tsx") ||
		strings.HasSuffix(name, ".mts") || strings.HasSuffix(name, ".cts")
}

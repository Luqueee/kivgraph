package workspace

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

var defaultTypeScriptExcludedDirectories = map[string]struct{}{
	".git":             {},
	"node_modules":     {},
	".pnpm":            {},
	".yarn":            {},
	"bower_components": {},
}

// DiscoverTypeScript finds package manifests, TypeScript project configs,
// workspace declarations and project references below a repository root.
// Results are deterministic and use absolute, canonical paths.
func DiscoverTypeScript(ctx context.Context, repository Repository) (TypeScriptDiscovery, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return TypeScriptDiscovery{}, err
	}
	rootInput := repository.RealPath
	if strings.TrimSpace(rootInput) == "" {
		rootInput = repository.Path
	}
	_, root, err := inspectRepositoryPath(rootInput)
	if err != nil {
		return TypeScriptDiscovery{}, fmt.Errorf("discover TypeScript root: %w", err)
	}

	packageManifests := make(map[string]struct{})
	configPaths := make(map[string]struct{})
	workspaces := make([]TypeScriptWorkspaceDeclaration, 0)
	workspaceKeys := make(map[string]struct{})
	err = walkTypeScriptFiles(ctx, root, root, repository.Exclusions, func(filePath string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := filepath.Base(filePath)
		switch {
		case name == "package.json":
			patterns, declared, err := parsePackageWorkspace(filePath)
			if err != nil {
				return fmt.Errorf("parse package manifest %q: %w", filePath, err)
			}
			packageManifests[filePath] = struct{}{}
			if declared {
				if err := appendTypeScriptWorkspace(root, &workspaces, workspaceKeys, TypeScriptWorkspaceDeclaration{
					ManifestPath: filePath,
					Format:       TypeScriptWorkspacePackageJSON,
					Patterns:     patterns,
				}); err != nil {
					return fmt.Errorf("workspace declaration %q: %w", filePath, err)
				}
			}
		case name == "pnpm-workspace.yaml" || name == "pnpm-workspace.yml":
			patterns, err := parsePNPMWorkspace(filePath)
			if err != nil {
				return fmt.Errorf("parse workspace manifest %q: %w", filePath, err)
			}
			if err := appendTypeScriptWorkspace(root, &workspaces, workspaceKeys, TypeScriptWorkspaceDeclaration{
				ManifestPath: filePath,
				Format:       TypeScriptWorkspacePNPM,
				Patterns:     patterns,
			}); err != nil {
				return fmt.Errorf("workspace declaration %q: %w", filePath, err)
			}
		case isTypeScriptConfigName(name):
			configPaths[filePath] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return TypeScriptDiscovery{}, err
	}

	projects, err := discoverTypeScriptProjects(ctx, root, configPaths)
	if err != nil {
		return TypeScriptDiscovery{}, err
	}

	result := TypeScriptDiscovery{
		PackageManifests: sortedPathSet(packageManifests),
		Projects:         projects,
		Workspaces:       workspaces,
	}
	sort.Slice(result.Workspaces, func(left, right int) bool {
		if result.Workspaces[left].ManifestPath != result.Workspaces[right].ManifestPath {
			return result.Workspaces[left].ManifestPath < result.Workspaces[right].ManifestPath
		}
		return result.Workspaces[left].Format < result.Workspaces[right].Format
	})
	return result, nil
}

func walkTypeScriptFiles(ctx context.Context, base, current string, exclusions []string, visit func(string) error) error {
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
		entryPath := filepath.Join(current, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		isDirectory := entry.IsDir()
		excluded, err := isDiscoveryExcluded(base, entryPath, entry.Name(), isDirectory, exclusions)
		if err != nil {
			return fmt.Errorf("check exclusion for %q: %w", entryPath, err)
		}
		if excluded {
			continue
		}
		if isDirectory {
			if err := walkTypeScriptFiles(ctx, base, entryPath, exclusions, visit); err != nil {
				return err
			}
			continue
		}
		if entry.Name() != "package.json" && entry.Name() != "pnpm-workspace.yaml" && entry.Name() != "pnpm-workspace.yml" && !isTypeScriptConfigName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect file %q: %w", entryPath, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if err := visit(entryPath); err != nil {
			return err
		}
	}
	return nil
}

func isDiscoveryExcluded(base, candidate, name string, isDirectory bool, exclusions []string) (bool, error) {
	excluded, err := MatchesExclusion(base, candidate, exclusions)
	if err != nil {
		return false, err
	}
	if isDirectory {
		if _, excluded := defaultTypeScriptExcludedDirectories[name]; excluded {
			return true, nil
		}
	}
	return excluded, nil
}

// MatchesExclusion reports whether candidate or one of its path ancestors is
// excluded by the configured repository patterns. It is the authoritative
// segment-aware matcher used by workspace discovery, including recursive
// `**`, absolute in-repository paths and `./` relative paths.
//
// Checking ancestors is important for callers that do not prune directories
// while walking: an exclusion such as `**/benchmarks` excludes the directory
// and every file below it, not just a path whose final segment is the pattern.
func MatchesExclusion(base, candidate string, exclusions []string) (bool, error) {
	base = filepath.Clean(base)
	relative, err := filepath.Rel(base, filepath.Clean(candidate))
	if err != nil {
		return false, fmt.Errorf("resolve candidate %q relative to %q: %w", candidate, base, err)
	}
	if relative == "." {
		return false, nil
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return false, fmt.Errorf("candidate %q escapes repository root %q", candidate, base)
	}
	parts := splitDiscoveryPath(filepath.ToSlash(relative))
	for index, rawPattern := range exclusions {
		pattern, err := normalizeExclusionPattern(base, rawPattern)
		if err != nil {
			return false, fmt.Errorf("exclusions[%d]: %w", index, err)
		}
		if pattern == "" || pattern == "." {
			continue
		}
		for end := 1; end <= len(parts); end++ {
			matched, err := discoveryPatternMatch(pattern, strings.Join(parts[:end], "/"))
			if err != nil {
				return false, fmt.Errorf("exclusions[%d] %q: %w", index, rawPattern, err)
			}
			if matched {
				return true, nil
			}
		}
	}
	return false, nil
}

func normalizeExclusionPattern(base, rawPattern string) (string, error) {
	pattern := strings.TrimSpace(rawPattern)
	if pattern == "" {
		return "", nil
	}
	if filepath.IsAbs(pattern) {
		relative, err := filepath.Rel(base, filepath.Clean(pattern))
		if err != nil {
			return "", fmt.Errorf("resolve %q: %w", rawPattern, err)
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
			return "", fmt.Errorf("path %q escapes repository root %q", rawPattern, base)
		}
		pattern = relative
	}
	pattern = filepath.ToSlash(filepath.Clean(filepath.FromSlash(pattern)))
	if pattern == ".." || strings.HasPrefix(pattern, "../") {
		return "", fmt.Errorf("path %q escapes repository root %q", rawPattern, base)
	}
	return strings.TrimPrefix(pattern, "./"), nil
}

func discoveryPatternMatch(pattern, relative string) (bool, error) {
	patternParts := splitDiscoveryPath(pattern)
	relativeParts := splitDiscoveryPath(relative)
	for _, patternPart := range patternParts {
		if patternPart == "**" {
			continue
		}
		if _, err := path.Match(patternPart, ""); err != nil {
			return false, err
		}
	}
	memo := make(map[[2]int]bool)
	visited := make(map[[2]int]bool)
	var match func(int, int) bool
	match = func(patternIndex, relativeIndex int) bool {
		key := [2]int{patternIndex, relativeIndex}
		if visited[key] {
			return memo[key]
		}
		visited[key] = true
		if patternIndex == len(patternParts) {
			memo[key] = relativeIndex == len(relativeParts)
			return memo[key]
		}
		if patternParts[patternIndex] == "**" {
			memo[key] = match(patternIndex+1, relativeIndex) || (relativeIndex < len(relativeParts) && match(patternIndex, relativeIndex+1))
			return memo[key]
		}
		if relativeIndex >= len(relativeParts) {
			return false
		}
		segmentMatches, err := path.Match(patternParts[patternIndex], relativeParts[relativeIndex])
		memo[key] = err == nil && segmentMatches && match(patternIndex+1, relativeIndex+1)
		return memo[key]
	}
	return match(0, 0), nil
}

func splitDiscoveryPath(value string) []string {
	value = strings.Trim(value, "/")
	if value == "" {
		return nil
	}
	return strings.Split(value, "/")
}

func appendTypeScriptWorkspace(root string, workspaces *[]TypeScriptWorkspaceDeclaration, keys map[string]struct{}, declaration TypeScriptWorkspaceDeclaration) error {
	patterns, err := normalizeWorkspacePatterns(root, declaration.Patterns)
	if err != nil {
		return err
	}
	declaration.Patterns = patterns
	key := declaration.ManifestPath + "\x00" + string(declaration.Format)
	if _, exists := keys[key]; exists {
		return nil
	}
	keys[key] = struct{}{}
	*workspaces = append(*workspaces, declaration)
	return nil
}

func discoverTypeScriptProjects(ctx context.Context, root string, configPaths map[string]struct{}) ([]TypeScriptProject, error) {
	queue := sortedPathSet(configPaths)
	queued := make(map[string]struct{}, len(queue))
	for _, configPath := range queue {
		queued[configPath] = struct{}{}
	}
	projects := make(map[string]TypeScriptProject, len(queue))
	for index := 0; index < len(queue); index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		configPath := queue[index]
		if _, alreadyParsed := projects[configPath]; alreadyParsed {
			continue
		}
		project, err := parseTypeScriptProject(configPath, root)
		if err != nil {
			return nil, fmt.Errorf("parse TypeScript project %q: %w", configPath, err)
		}
		projects[configPath] = project
		for _, reference := range project.References {
			if _, alreadyQueued := queued[reference]; alreadyQueued {
				continue
			}
			queued[reference] = struct{}{}
			queue = append(queue, reference)
		}
	}
	projectsList := make([]TypeScriptProject, 0, len(projects))
	for _, project := range projects {
		project.References = append([]string(nil), project.References...)
		projectsList = append(projectsList, project)
	}
	sort.Slice(projectsList, func(left, right int) bool {
		return projectsList[left].ConfigPath < projectsList[right].ConfigPath
	})
	return projectsList, nil
}

func sortedPathSet(paths map[string]struct{}) []string {
	result := make([]string, 0, len(paths))
	for path := range paths {
		result = append(result, path)
	}
	sort.Strings(result)
	return result
}

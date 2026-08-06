package workspace

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// defaultTypeScriptSourceExtensions are always recognised by an "include"
// glob that does not already end in an explicit extension.
var defaultTypeScriptSourceExtensions = []string{".ts", ".tsx", ".d.ts"}

// defaultExcludedSourceDirectoryNames are pruned from "include" expansion
// whenever the project does not declare its own "exclude".
var defaultExcludedSourceDirectoryNames = []string{"node_modules", "bower_components", "jspm_packages"}

// resolveTypeScriptSources computes the absolute, sorted, duplicate-free set
// of source files a TypeScript project owns, following the compiler's own
// "files"/"include"/"exclude" resolution. Explicit "files" are validated and
// always kept regardless of "exclude" -- the compiler honours them
// unconditionally -- while "include" is expanded through a "**"-aware glob
// walk and then filtered by "exclude" and by the extensions the project's
// compiler options recognise.
func resolveTypeScriptSources(configuration parsedTypeScriptConfig, repositoryRoot string) ([]string, error) {
	root := filepath.Clean(repositoryRoot)

	sources, err := resolveExplicitTypeScriptFiles(configuration.Files, root)
	if err != nil {
		return nil, err
	}

	includePatterns := effectiveTypeScriptIncludePatterns(configuration)
	if len(includePatterns) == 0 {
		return sortedUniqueSources(sources), nil
	}

	excludePatternSegments := splitGlobPatterns(effectiveTypeScriptExcludePatterns(configuration))
	allowedExtensions := allowedTypeScriptSourceExtensions(configuration.CompilerOptions)

	for _, includePattern := range includePatterns {
		matches, err := expandTypeScriptGlob(includePattern, root, excludePatternSegments, allowedExtensions)
		if err != nil {
			return nil, fmt.Errorf("include pattern %q: %w", includePattern, err)
		}
		for _, match := range matches {
			sources[match] = struct{}{}
		}
	}

	return sortedUniqueSources(sources), nil
}

// resolveExplicitTypeScriptFiles validates every path declared under
// "files". TypeScript treats these as explicit, mandatory entries: each one
// must exist as a regular file inside repositoryRoot, and none of them is
// filtered by "exclude" -- the compiler always honours an explicit file.
func resolveExplicitTypeScriptFiles(files []string, repositoryRoot string) (map[string]struct{}, error) {
	resolved := make(map[string]struct{}, len(files))
	for _, declaredFile := range files {
		cleanedFile := filepath.Clean(declaredFile)
		info, err := os.Stat(cleanedFile)
		if err != nil {
			return nil, fmt.Errorf("files entry %q: %w", declaredFile, err)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("files entry %q is not a regular file", declaredFile)
		}
		if !pathWithin(repositoryRoot, cleanedFile) {
			return nil, fmt.Errorf("files entry %q escapes repository root %q", declaredFile, repositoryRoot)
		}
		resolved[cleanedFile] = struct{}{}
	}
	return resolved, nil
}

// effectiveTypeScriptIncludePatterns applies the TypeScript default: a
// project that declares neither "files" nor "include" owns every file below
// its directory, exactly as if it had declared include: ["**/*"].
func effectiveTypeScriptIncludePatterns(configuration parsedTypeScriptConfig) []string {
	if !configuration.HasFiles && !configuration.HasInclude {
		return []string{filepath.Join(configuration.Directory, "**", "*")}
	}
	return configuration.Include
}

// effectiveTypeScriptExcludePatterns applies TypeScript's default excludes
// (node_modules, bower_components, jspm_packages, plus outDir and
// declarationDir when set) whenever the project does not declare its own
// "exclude". A declared "exclude" replaces that list entirely.
func effectiveTypeScriptExcludePatterns(configuration parsedTypeScriptConfig) []string {
	if configuration.HasExclude {
		patterns := make([]string, 0, len(configuration.Exclude)+1)
		patterns = append(patterns, configuration.Exclude...)
		// TypeScript always prunes node_modules from wildcard expansion for
		// performance, even when the project declares its own "exclude".
		// Ladygraph mirrors that observable compiler behaviour here instead of
		// diverging from it just because it could technically afford to walk
		// the tree.
		patterns = append(patterns, filepath.Join(configuration.Directory, "**", "node_modules"))
		return patterns
	}

	patterns := make([]string, 0, len(defaultExcludedSourceDirectoryNames)+2)
	for _, name := range defaultExcludedSourceDirectoryNames {
		patterns = append(patterns, filepath.Join(configuration.Directory, "**", name))
	}
	for _, pathOption := range []string{"outDir", "declarationDir"} {
		if value, ok := configuration.CompilerOptions[pathOption].(string); ok && value != "" {
			patterns = append(patterns, filepath.Clean(value))
		}
	}
	return patterns
}

// allowedTypeScriptSourceExtensions returns the extensions a wildcard
// "include" glob picks up when its own pattern does not already end in an
// explicit extension.
func allowedTypeScriptSourceExtensions(compilerOptions map[string]any) []string {
	extensions := append([]string(nil), defaultTypeScriptSourceExtensions...)
	if booleanCompilerOption(compilerOptions, "allowJs") {
		extensions = append(extensions, ".js", ".jsx")
	}
	if booleanCompilerOption(compilerOptions, "resolveJsonModule") {
		extensions = append(extensions, ".json")
	}
	return extensions
}

// expandTypeScriptGlob walks the filesystem beneath pattern's static,
// wildcard-free prefix and returns every regular file inside repositoryRoot
// that matches it, after applying excludePatternSegments and, when pattern
// has no explicit extension, allowedExtensions. A prefix that does not
// exist, or that falls outside repositoryRoot, yields no matches rather than
// an error: unlike "files", "include" only ever lists candidates.
func expandTypeScriptGlob(pattern, repositoryRoot string, excludePatternSegments [][]string, allowedExtensions []string) ([]string, error) {
	startDirectory := staticGlobPrefix(pattern)
	if !pathWithin(repositoryRoot, startDirectory) {
		return nil, nil
	}

	startInfo, err := os.Lstat(startDirectory)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("inspect %q: %w", startDirectory, err)
	}
	if startInfo.Mode()&os.ModeSymlink != 0 {
		return nil, nil
	}
	if isPathOrAncestorExcluded(startDirectory, repositoryRoot, excludePatternSegments) {
		return nil, nil
	}

	patternSegments := splitAbsolutePathSegments(pattern)
	filterByExtension := !patternHasExplicitExtension(pattern)

	if !startInfo.IsDir() {
		if !startInfo.Mode().IsRegular() || filepath.Clean(pattern) != startDirectory {
			return nil, nil
		}
		if filterByExtension && !hasAllowedExtension(startDirectory, allowedExtensions) {
			return nil, nil
		}
		return []string{startDirectory}, nil
	}

	var matches []string
	if err := walkTypeScriptGlobDirectory(startDirectory, patternSegments, excludePatternSegments, filterByExtension, allowedExtensions, &matches); err != nil {
		return nil, err
	}
	return matches, nil
}

// walkTypeScriptGlobDirectory recurses through directory in the
// deterministic order os.ReadDir already sorts entries into, collecting
// every regular file that satisfies patternSegments into matches.
func walkTypeScriptGlobDirectory(
	directory string,
	patternSegments []string,
	excludePatternSegments [][]string,
	filterByExtension bool,
	allowedExtensions []string,
	matches *[]string,
) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read directory %q: %w", directory, err)
	}
	for _, entry := range entries {
		// A symlink could reintroduce a cycle (a directory) or silently
		// bypass the extension/exclude rules below (a file), so every
		// symlink is skipped rather than just directory ones, matching how
		// DiscoverTypeScript already walks the repository.
		if entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		entryPath := filepath.Join(directory, entry.Name())
		entrySegments := splitAbsolutePathSegments(entryPath)
		if entry.IsDir() {
			if matchesAnyGlobPattern(entrySegments, excludePatternSegments) {
				continue
			}
			if err := walkTypeScriptGlobDirectory(entryPath, patternSegments, excludePatternSegments, filterByExtension, allowedExtensions, matches); err != nil {
				return err
			}
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect %q: %w", entryPath, err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		if !matchGlobSegments(patternSegments, entrySegments) {
			continue
		}
		if matchesAnyGlobPattern(entrySegments, excludePatternSegments) {
			continue
		}
		if filterByExtension && !hasAllowedExtension(entryPath, allowedExtensions) {
			continue
		}
		*matches = append(*matches, entryPath)
	}
	return nil
}

// splitGlobPatterns splits every pattern once into the segments
// matchGlobSegments compares, so a walk that tests many candidates against
// the same exclude patterns does not re-split them for every candidate.
func splitGlobPatterns(patterns []string) [][]string {
	segments := make([][]string, len(patterns))
	for index, pattern := range patterns {
		segments[index] = splitAbsolutePathSegments(pattern)
	}
	return segments
}

// matchesAnyGlobPattern reports whether candidateSegments satisfies at least
// one pattern in patternSegments.
func matchesAnyGlobPattern(candidateSegments []string, patternSegments [][]string) bool {
	for _, segments := range patternSegments {
		if matchGlobSegments(segments, candidateSegments) {
			return true
		}
	}
	return false
}

// isPathOrAncestorExcluded reports whether candidatePath, or any of its
// ancestors up to repositoryRoot, matches an exclude pattern. An "include"
// entry such as "node_modules/pkg/**/*.ts" starts its walk below
// node_modules itself, skipping the very directory a default exclude names;
// checking the ancestry closes that gap so an explicit include cannot reach
// back into an excluded tree.
func isPathOrAncestorExcluded(candidatePath, repositoryRoot string, excludePatternSegments [][]string) bool {
	current := filepath.Clean(candidatePath)
	root := filepath.Clean(repositoryRoot)
	for {
		if matchesAnyGlobPattern(splitAbsolutePathSegments(current), excludePatternSegments) {
			return true
		}
		if current == root {
			return false
		}
		parent := filepath.Dir(current)
		if parent == current || !pathWithin(root, parent) {
			return false
		}
		current = parent
	}
}

// matchGlobSegments reports whether candidateSegments satisfies the
// TypeScript wildcard segments in patternSegments. Segments are compared
// left to right: a "**" segment absorbs any number of nested directories,
// including zero, while every other segment is matched against exactly one
// candidate segment with path.Match, whose "*" and "?" already exclude the
// path separator the way TypeScript's own wildcards do. The search is
// memoised because a pattern with several "**" segments would otherwise
// re-explore the same (patternIndex, candidateIndex) pair exponentially.
func matchGlobSegments(patternSegments, candidateSegments []string) bool {
	memo := make(map[[2]int]bool)
	var match func(patternIndex, candidateIndex int) bool
	match = func(patternIndex, candidateIndex int) bool {
		key := [2]int{patternIndex, candidateIndex}
		if cached, seen := memo[key]; seen {
			return cached
		}
		var result bool
		switch {
		case patternIndex == len(patternSegments):
			result = candidateIndex == len(candidateSegments)
		case patternSegments[patternIndex] == "**":
			result = match(patternIndex+1, candidateIndex) ||
				(candidateIndex < len(candidateSegments) && match(patternIndex, candidateIndex+1))
		case candidateIndex == len(candidateSegments):
			result = false
		default:
			segmentMatches, err := path.Match(patternSegments[patternIndex], candidateSegments[candidateIndex])
			result = err == nil && segmentMatches && match(patternIndex+1, candidateIndex+1)
		}
		memo[key] = result
		return result
	}
	return match(0, 0)
}

// splitAbsolutePathSegments splits a cleaned absolute path into the
// components matchGlobSegments compares. Patterns and candidate files are
// both split this way, so their segments line up positionally.
func splitAbsolutePathSegments(absolutePath string) []string {
	slashed := strings.TrimPrefix(filepath.ToSlash(filepath.Clean(absolutePath)), "/")
	if slashed == "" {
		return nil
	}
	return strings.Split(slashed, "/")
}

// staticGlobPrefix returns the longest ancestor directory of pattern that
// contains no wildcard character. That is where the filesystem walk for
// pattern must start: nothing above it can ever be selected, and stopping
// there keeps a project from scanning directories none of its own patterns
// reference.
func staticGlobPrefix(pattern string) string {
	prefix := filepath.Clean(pattern)
	for strings.ContainsAny(filepath.Base(prefix), "*?") {
		parent := filepath.Dir(prefix)
		if parent == prefix {
			break
		}
		prefix = parent
	}
	return prefix
}

// patternHasExplicitExtension reports whether the final path segment of
// pattern already names a concrete extension, for example "*.ts" or
// "index.ts". TypeScript then relies on the glob match alone: a pattern
// ending in "src/**/*.ts" only ever matches ".ts" files. Without a literal
// extension in that last segment -- a bare "*", say -- TypeScript instead
// restricts matches to its recognised source extensions.
func patternHasExplicitExtension(pattern string) bool {
	return strings.Contains(filepath.Base(pattern), ".")
}

// hasAllowedExtension reports whether filePath ends in one of extensions.
func hasAllowedExtension(filePath string, extensions []string) bool {
	for _, extension := range extensions {
		if strings.HasSuffix(filePath, extension) {
			return true
		}
	}
	return false
}

// sortedUniqueSources drains sources into a lexicographically sorted slice.
func sortedUniqueSources(sources map[string]struct{}) []string {
	list := make([]string, 0, len(sources))
	for source := range sources {
		list = append(list, source)
	}
	sort.Strings(list)
	return list
}

package watcher

import (
	"fmt"
	pathpkg "path"
	"path/filepath"
	"strings"
)

var defaultIgnoredDirectoryNames = map[string]struct{}{
	".git":             {},
	"node_modules":     {},
	".pnpm":            {},
	".yarn":            {},
	"bower_components": {},
	"vendor":           {},
}

type ignoreMatcher struct {
	root     string
	patterns []string
}

func newIgnoreMatcher(root string, rawPatterns []string) (ignoreMatcher, error) {
	matcher := ignoreMatcher{root: filepath.Clean(root), patterns: make([]string, 0, len(rawPatterns))}
	for index, rawPattern := range rawPatterns {
		pattern := strings.TrimSpace(rawPattern)
		if pattern == "" {
			continue
		}
		if filepath.IsAbs(pattern) {
			relative, err := filepath.Rel(matcher.root, filepath.Clean(pattern))
			if err != nil {
				return ignoreMatcher{}, fmt.Errorf("exclusions[%d]: resolve %q: %w", index, rawPattern, err)
			}
			if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
				return ignoreMatcher{}, fmt.Errorf("exclusions[%d]: path %q escapes repository root", index, rawPattern)
			}
			pattern = relative
		}
		pattern = filepath.ToSlash(strings.TrimPrefix(pattern, "./"))
		if pattern == "" || pattern == "." {
			continue
		}
		matcher.patterns = append(matcher.patterns, pattern)
	}
	return matcher, nil
}

func (matcher ignoreMatcher) ignored(candidate string) bool {
	relative, err := filepath.Rel(matcher.root, filepath.Clean(candidate))
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return true
	}
	if relative == "." {
		return false
	}
	parts := splitPath(relative)
	for _, part := range parts {
		if _, ignored := defaultIgnoredDirectoryNames[part]; ignored {
			return true
		}
	}
	for end := 1; end <= len(parts); end++ {
		prefix := strings.Join(parts[:end], "/")
		for _, pattern := range matcher.patterns {
			if matchPathPattern(pattern, prefix) {
				return true
			}
		}
	}
	return false
}

func matchPathPattern(pattern, relative string) bool {
	patternParts := splitPath(pattern)
	relativeParts := splitPath(relative)
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
		segmentMatches, err := pathpkg.Match(patternParts[patternIndex], relativeParts[relativeIndex])
		memo[key] = err == nil && segmentMatches && match(patternIndex+1, relativeIndex+1)
		return memo[key]
	}
	return match(0, 0)
}

func splitPath(value string) []string {
	value = strings.Trim(value, "/")
	if value == "" {
		return nil
	}
	return strings.Split(value, "/")
}

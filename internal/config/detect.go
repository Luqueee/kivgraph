package config

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// detectedLanguageOrder is the canonical order used by the project-local
// index command. It intentionally contains no aliases, so the same project
// produces the same registry regardless of which spelling a configuration
// accepts.
var detectedLanguageOrder = []string{
	"go", "typescript", "javascript", "rust", "python", "dart", "java", "csharp",
}

// detectedLanguageExtensions maps source suffixes to the canonical language
// name accepted by the indexers. The extension lookup is case-insensitive to
// match the way SourceExtensions watches files.
var detectedLanguageExtensions = map[string]string{
	".go": "go",

	".ts":  "typescript",
	".tsx": "typescript",
	".mts": "typescript",
	".cts": "typescript",

	".js":  "javascript",
	".jsx": "javascript",
	".mjs": "javascript",
	".cjs": "javascript",

	".rs": "rust",

	".py":  "python",
	".pyi": "python",

	".dart": "dart",
	".java": "java",
	".cs":   "csharp",
}

// detectedLanguageFiles maps project manifests to the language they declare.
// A manifest is useful when a checkout has generated or empty source folders,
// and it also makes a freshly created project discoverable before its first
// source file is written.
var detectedLanguageFiles = map[string]string{
	"go.mod":              "go",
	"tsconfig.json":       "typescript",
	"package.json":        "javascript",
	"cargo.toml":          "rust",
	"pyproject.toml":      "python",
	"requirements.txt":    "python",
	"setup.py":            "python",
	"pipfile":             "python",
	"pubspec.yaml":        "dart",
	"pom.xml":             "java",
	"build.gradle":        "java",
	"build.gradle.kts":    "java",
	"settings.gradle":     "java",
	"settings.gradle.kts": "java",
}

// detectedLanguageExtensionsByName captures project files whose language is
// identified by their suffix rather than their source suffix.
var detectedLanguageExtensionsByName = map[string]string{
	".csproj": "csharp",
	".sln":    "csharp",
}

// directoriesIgnoredDuringDetection are dependencies, generated output and
// Kivgraph's own local state. Walking any of them would make a project claim a
// language that its source tree does not contain.
var directoriesIgnoredDuringDetection = map[string]struct{}{
	".git": {}, ".kivgraph": {}, "node_modules": {}, "vendor": {},
	"target": {}, "dist": {}, "build": {}, "out": {}, "coverage": {},
	".next": {}, ".nuxt": {}, ".dart_tool": {}, ".gradle": {},
	".idea": {}, ".vscode": {}, ".venv": {}, "__pycache__": {},
	"bin": {}, "obj": {},
}

// DetectLanguages walks root and returns the supported languages declared by
// source files or project manifests. It never follows symlinked files or
// directories, and it ignores generated/dependency trees so autodetection
// cannot widen an index from a vendored artifact.
func DetectLanguages(root string) ([]string, error) {
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}
	absolute, err = filepath.EvalSymlinks(absolute)
	if err != nil {
		return nil, fmt.Errorf("resolve project path %q: %w", root, err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return nil, fmt.Errorf("inspect project path %q: %w", absolute, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("project path %q is not a directory", absolute)
	}

	found := make(map[string]struct{}, len(detectedLanguageOrder))
	err = filepath.WalkDir(absolute, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("walk %q: %w", path, walkErr)
		}
		if path != absolute && entry.IsDir() {
			if _, ignored := directoriesIgnoredDuringDetection[strings.ToLower(entry.Name())]; ignored {
				return fs.SkipDir
			}
			return nil
		}
		if path == absolute || !entry.Type().IsRegular() {
			return nil
		}

		name := strings.ToLower(entry.Name())
		if language, ok := detectedLanguageFiles[name]; ok {
			found[language] = struct{}{}
		}
		if language, ok := detectedLanguageExtensionsByName[strings.ToLower(filepath.Ext(name))]; ok {
			found[language] = struct{}{}
		}
		if language, ok := detectedLanguageExtensions[strings.ToLower(filepath.Ext(name))]; ok {
			found[language] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	languages := make([]string, 0, len(found))
	for _, language := range detectedLanguageOrder {
		if _, ok := found[language]; ok {
			languages = append(languages, language)
		}
	}
	return languages, nil
}

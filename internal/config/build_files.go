package config

import (
	"path/filepath"
	"strings"
)

// analyzerBuildConfigurationNames is the conservative set of files whose
// contents can change the facts an analyzer produces. Both the fact cache and
// the content attestation consume this list through the helpers below; a
// second list would let one of them trust facts the other would invalidate.
var analyzerBuildConfigurationNames = []string{
	"analysis_options.yaml",
	"build.gradle",
	"build.gradle.kts",
	"build.sbt",
	"Cargo.lock",
	"Cargo.toml",
	"Directory.Build.props",
	"Directory.Build.targets",
	"Directory.Packages.props",
	"global.json",
	"gradle.properties",
	"gradle-wrapper.properties",
	"go.mod",
	"go.sum",
	"go.work",
	"go.work.sum",
	"jsconfig.json",
	"NuGet.config",
	"package-lock.json",
	"package.json",
	"packages.config",
	"packages.lock.json",
	"Pipfile",
	"Pipfile.lock",
	"pnpm-lock.yaml",
	"pnpm-workspace.yaml",
	"pnpm-workspace.yml",
	"poetry.lock",
	"pom.xml",
	"pubspec.lock",
	"pubspec.yaml",
	"pyrightconfig.json",
	"pyproject.toml",
	"requirements.txt",
	"setup.cfg",
	"setup.py",
	"settings.gradle",
	"settings.gradle.kts",
	"tsconfig.json",
	"uv.lock",
	"yarn.lock",
	filepath.Join(".dart_tool", "package_config.json"),
}

// IsBuildConfigurationFile reports whether the basename of path belongs to a
// build configuration consumed by one of the supported analyzers. Matching is
// case-insensitive because repository paths and the analyzers' manifest names
// are both treated that way by the workspace discovery code.
func IsBuildConfigurationFile(path string) bool {
	normalized := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	if normalized == ".dart_tool/package_config.json" || strings.HasSuffix(normalized, "/.dart_tool/package_config.json") {
		return true
	}
	base := strings.ToLower(filepath.Base(path))
	for _, name := range analyzerBuildConfigurationNames {
		if !strings.Contains(filepath.ToSlash(name), "/") && base == strings.ToLower(name) {
			return true
		}
	}
	if strings.HasPrefix(base, "requirements-") && strings.HasSuffix(base, ".txt") {
		return true
	}
	if (strings.HasPrefix(base, "tsconfig.") || strings.HasPrefix(base, "jsconfig.")) && strings.HasSuffix(base, ".json") {
		return true
	}
	return strings.HasSuffix(base, ".gradle") ||
		strings.HasSuffix(base, ".gradle.kts") ||
		strings.HasSuffix(base, ".sbt") ||
		strings.HasSuffix(base, ".csproj") ||
		strings.HasSuffix(base, ".sln") ||
		strings.HasSuffix(base, ".props") ||
		strings.HasSuffix(base, ".targets")
}

// BuildConfigurationPaths returns the named analyzer configuration paths that
// are checked as explicit file inputs. The returned paths are rooted at root;
// patterned files are covered by IsBuildConfigurationFile while walking.
func BuildConfigurationPaths(root string) []string {
	result := make([]string, len(analyzerBuildConfigurationNames))
	for index, path := range analyzerBuildConfigurationNames {
		result[index] = filepath.Join(root, path)
	}
	return result
}

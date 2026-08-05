package workspace

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ProviderConflictKind classifies one cross-repository provider conflict.
type ProviderConflictKind string

const (
	AmbiguousPackageProvider ProviderConflictKind = "AMBIGUOUS_PACKAGE_PROVIDER"
	AmbiguousModuleProvider  ProviderConflictKind = "AMBIGUOUS_MODULE_PROVIDER"
	PackageVersionMismatch   ProviderConflictKind = "PACKAGE_VERSION_MISMATCH"
	ModuleReplaceConflict    ProviderConflictKind = "MODULE_REPLACE_CONFLICT"
)

// ProviderConflict identifies all repositories and manifests participating in
// one classified conflict.
type ProviderConflict struct {
	Kind          ProviderConflictKind
	Provider      string
	Repositories  []string
	Versions      []string
	ManifestPaths []string
}

// ProviderConflictReport is the deterministic result of cross-repository
// provider validation.
type ProviderConflictReport struct {
	Conflicts []ProviderConflict
}

// HasConflicts reports whether any provider conflict was detected.
func (report ProviderConflictReport) HasConflicts() bool {
	return len(report.Conflicts) != 0
}

// List returns deep copies of all conflicts sorted by kind and provider.
func (report ProviderConflictReport) List() []ProviderConflict {
	conflicts := make([]ProviderConflict, len(report.Conflicts))
	for index, conflict := range report.Conflicts {
		conflicts[index] = cloneProviderConflict(conflict)
	}
	return conflicts
}

// DetectProviderConflicts builds the per-repository TypeScript and Go
// registries, then classifies duplicate providers and incompatible metadata.
// No provider is selected automatically.
func DetectProviderConflicts(ctx context.Context, repositories []Repository) (ProviderConflictReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return ProviderConflictReport{}, err
	}
	if err := validateProviderRepositories(repositories); err != nil {
		return ProviderConflictReport{}, err
	}
	providers := make([]repositoryProviders, 0, len(repositories))
	for _, repository := range repositories {
		if err := ctx.Err(); err != nil {
			return ProviderConflictReport{}, err
		}
		packages, err := NewTypeScriptPackageRegistry(ctx, repository)
		if err != nil {
			return ProviderConflictReport{}, fmt.Errorf("repository %q TypeScript providers: %w", repository.Name, err)
		}
		modules, err := NewGoModuleRegistry(ctx, repository)
		if err != nil {
			return ProviderConflictReport{}, fmt.Errorf("repository %q Go providers: %w", repository.Name, err)
		}
		providers = append(providers, repositoryProviders{
			name:     strings.TrimSpace(repository.Name),
			packages: packages.List(),
			modules:  modules.List(),
		})
	}
	return classifyProviderConflicts(ctx, providers)
}

type repositoryProviders struct {
	name     string
	packages []TypeScriptPackage
	modules  []GoModuleProvider
}

func validateProviderRepositories(repositories []Repository) error {
	seen := make(map[string]int, len(repositories))
	for index, repository := range repositories {
		name := strings.TrimSpace(repository.Name)
		if name == "" {
			return fmt.Errorf("repositories[%d]: name must not be empty", index)
		}
		if previous, exists := seen[name]; exists {
			return fmt.Errorf("repositories[%d] %q: duplicate name of repositories[%d]", index, name, previous)
		}
		seen[name] = index
	}
	return nil
}

func classifyProviderConflicts(ctx context.Context, providers []repositoryProviders) (ProviderConflictReport, error) {
	packagesByName := make(map[string][]packageProvider)
	modulesByPath := make(map[string][]moduleProvider)
	for _, repository := range providers {
		if err := ctx.Err(); err != nil {
			return ProviderConflictReport{}, err
		}
		for _, packageValue := range repository.packages {
			packagesByName[packageValue.Name] = append(packagesByName[packageValue.Name], packageProvider{
				repository:   repository.name,
				packageValue: packageValue,
			})
		}
		for _, module := range repository.modules {
			modulesByPath[module.ModulePath] = append(modulesByPath[module.ModulePath], moduleProvider{
				repository: repository.name,
				module:     module,
			})
		}
	}

	conflicts := make([]ProviderConflict, 0)
	for packageName, packageProviders := range packagesByName {
		if err := ctx.Err(); err != nil {
			return ProviderConflictReport{}, err
		}
		if len(packageProviders) < 2 {
			continue
		}
		conflict := newPackageConflict(AmbiguousPackageProvider, packageName, packageProviders)
		conflicts = append(conflicts, conflict)
		if packageVersions(packageProviders) > 1 {
			conflicts = append(conflicts, newPackageConflict(PackageVersionMismatch, packageName, packageProviders))
		}
	}
	for modulePath, moduleProviders := range modulesByPath {
		if err := ctx.Err(); err != nil {
			return ProviderConflictReport{}, err
		}
		if len(moduleProviders) < 2 {
			continue
		}
		conflicts = append(conflicts, newModuleConflict(AmbiguousModuleProvider, modulePath, moduleProviders))
		if moduleReplacementSetsDiffer(moduleProviders) {
			conflicts = append(conflicts, newModuleConflict(ModuleReplaceConflict, modulePath, moduleProviders))
		}
	}
	conflicts = appendUniqueProviderConflicts(conflicts, crossModuleReplacementConflicts(providers)...)
	sort.Slice(conflicts, func(left, right int) bool {
		if conflicts[left].Kind != conflicts[right].Kind {
			return conflicts[left].Kind < conflicts[right].Kind
		}
		return conflicts[left].Provider < conflicts[right].Provider
	})
	return ProviderConflictReport{Conflicts: conflicts}, nil
}
func appendUniqueProviderConflicts(conflicts []ProviderConflict, candidates ...ProviderConflict) []ProviderConflict {
	for _, candidate := range candidates {
		duplicate := false
		for _, existing := range conflicts {
			if existing.Kind == candidate.Kind && existing.Provider == candidate.Provider {
				duplicate = true
				break
			}
		}
		if !duplicate {
			conflicts = append(conflicts, candidate)
		}
	}
	return conflicts
}

func crossModuleReplacementConflicts(providers []repositoryProviders) []ProviderConflict {
	bySource := make(map[string][]replacementProvider)
	for _, repository := range providers {
		for _, module := range repository.modules {
			for _, replacement := range normalizedModuleReplacements(module) {
				source := goReplacementSourceKey(replacement)
				bySource[source] = append(bySource[source], replacementProvider{
					repository:   repository.name,
					modulePath:   module.ModulePath,
					manifestPath: module.ManifestPath,
					replacement:  replacement,
				})
			}
		}
	}
	conflicts := make([]ProviderConflict, 0)
	for _, replacements := range bySource {
		if len(replacements) < 2 || !replacementModulePathsDiffer(replacements) || !replacementTargetsDiffer(replacements) {
			continue
		}
		conflict := ProviderConflict{
			Kind:     ModuleReplaceConflict,
			Provider: replacements[0].replacement.OldPath,
		}
		for _, replacement := range replacements {
			conflict.Repositories = appendUniqueString(conflict.Repositories, replacement.repository)
			conflict.ManifestPaths = appendUniqueString(conflict.ManifestPaths, replacement.manifestPath)
		}
		sort.Strings(conflict.Repositories)
		sort.Strings(conflict.ManifestPaths)
		conflicts = append(conflicts, conflict)
	}
	sort.Slice(conflicts, func(left, right int) bool {
		return conflicts[left].Provider < conflicts[right].Provider
	})
	return conflicts
}

type replacementProvider struct {
	repository   string
	modulePath   string
	manifestPath string
	replacement  GoReplacement
}

func replacementModulePathsDiffer(replacements []replacementProvider) bool {
	paths := make(map[string]struct{}, len(replacements))
	for _, replacement := range replacements {
		paths[replacement.modulePath] = struct{}{}
	}
	return len(paths) > 1
}

func replacementTargetsDiffer(replacements []replacementProvider) bool {
	targets := make(map[string]struct{}, len(replacements))
	for _, replacement := range replacements {
		targets[goReplacementTargetKey(replacement.replacement)] = struct{}{}
	}
	return len(targets) > 1
}

type packageProvider struct {
	repository   string
	packageValue TypeScriptPackage
}

type moduleProvider struct {
	repository string
	module     GoModuleProvider
}

func newPackageConflict(kind ProviderConflictKind, provider string, providers []packageProvider) ProviderConflict {
	conflict := ProviderConflict{Kind: kind, Provider: provider}
	for _, candidate := range providers {
		conflict.Repositories = appendUniqueString(conflict.Repositories, candidate.repository)
		conflict.Versions = appendUniqueString(conflict.Versions, candidate.packageValue.Version)
		conflict.ManifestPaths = appendUniqueString(conflict.ManifestPaths, candidate.packageValue.ManifestPath)
	}
	sort.Strings(conflict.Repositories)
	sort.Strings(conflict.Versions)
	sort.Strings(conflict.ManifestPaths)
	return conflict
}

func newModuleConflict(kind ProviderConflictKind, provider string, providers []moduleProvider) ProviderConflict {
	conflict := ProviderConflict{Kind: kind, Provider: provider}
	for _, candidate := range providers {
		conflict.Repositories = appendUniqueString(conflict.Repositories, candidate.repository)
		conflict.ManifestPaths = appendUniqueString(conflict.ManifestPaths, candidate.module.ManifestPath)
	}
	sort.Strings(conflict.Repositories)
	sort.Strings(conflict.ManifestPaths)
	return conflict
}

func packageVersions(providers []packageProvider) int {
	versions := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		versions[provider.packageValue.Version] = struct{}{}
	}
	return len(versions)
}

func moduleReplacementSetsDiffer(providers []moduleProvider) bool {
	first := normalizedModuleReplacements(providers[0].module)
	for _, provider := range providers[1:] {
		if !equalGoReplacements(first, normalizedModuleReplacements(provider.module)) {
			return true
		}
	}
	return false
}

func normalizedModuleReplacements(module GoModuleProvider) []GoReplacement {
	replacements := make([]GoReplacement, 0, len(module.Replaces)+len(module.WorkspaceReplaces))
	for _, replacement := range append(cloneGoReplacements(module.Replaces), module.WorkspaceReplaces...) {
		if !containsGoReplacement(replacements, replacement) {
			replacements = append(replacements, replacement)
		}
	}
	sort.Slice(replacements, func(left, right int) bool {
		return goReplacementKey(replacements[left]) < goReplacementKey(replacements[right])
	})
	return replacements
}

func equalGoReplacements(left, right []GoReplacement) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func goReplacementSourceKey(replacement GoReplacement) string {
	return strings.Join([]string{replacement.OldPath, replacement.OldVersion}, "\x00")
}

func goReplacementTargetKey(replacement GoReplacement) string {
	return strings.Join([]string{replacement.NewPath, replacement.NewVersion, replacement.NewLocalPath}, "\x00")
}

func goReplacementKey(replacement GoReplacement) string {
	return strings.Join([]string{
		goReplacementSourceKey(replacement),
		goReplacementTargetKey(replacement),
	}, "\x00")
}

func appendUniqueString(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func cloneProviderConflict(conflict ProviderConflict) ProviderConflict {
	conflict.Repositories = append([]string(nil), conflict.Repositories...)
	conflict.Versions = append([]string(nil), conflict.Versions...)
	conflict.ManifestPaths = append([]string(nil), conflict.ManifestPaths...)
	return conflict
}

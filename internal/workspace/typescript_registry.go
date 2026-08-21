package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// TypeScriptPackage is a named package provider discovered in one repository.
type TypeScriptPackage struct {
	Name             string
	Version          string
	Private          bool
	Repository       string
	RootPath         string
	ManifestPath     string
	Exports          json.RawMessage
	TypesPath        string
	ProjectPath      string
	SourceRoots      []string
	DeclarationRoots []string
}

// TypeScriptPackageRegistry is an immutable package-name index for one
// repository. Cross-repository ambiguity is handled by LUQUE-0408.
type TypeScriptPackageRegistry struct {
	packages  []TypeScriptPackage
	byName    map[string]int
	conflicts []TypeScriptPackageConflict
}

// TypeScriptPackageConflict is one package name several manifests declare.
//
// Neither manifest can be chosen: a reference to that name has no single
// provider. The ambiguity is reported the same way an ambiguous Go module
// provider is -- the candidates leave the registry and the conflict is
// declared -- because a repository that vendors or fixtures a second copy of
// a package must not make the rest of the graph unbuildable.
type TypeScriptPackageConflict struct {
	Name      string
	Manifests []string
}

// NewTypeScriptPackageRegistry discovers and indexes named package.json
// manifests for one repository.
func NewTypeScriptPackageRegistry(ctx context.Context, repository Repository) (*TypeScriptPackageRegistry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	discovery, err := DiscoverTypeScript(ctx, repository)
	if err != nil {
		return nil, err
	}
	return newTypeScriptPackageRegistry(ctx, repository, discovery)
}

func newTypeScriptPackageRegistry(ctx context.Context, repository Repository, discovery TypeScriptDiscovery) (*TypeScriptPackageRegistry, error) {
	registry := &TypeScriptPackageRegistry{
		packages:  make([]TypeScriptPackage, 0, len(discovery.PackageManifests)),
		byName:    make(map[string]int, len(discovery.PackageManifests)),
		conflicts: make([]TypeScriptPackageConflict, 0),
	}
	byName := make(map[string][]TypeScriptPackage, len(discovery.PackageManifests))
	order := make([]string, 0, len(discovery.PackageManifests))
	for _, manifestPath := range discovery.PackageManifests {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		packageValue, named, err := buildTypeScriptPackage(repository, discovery, manifestPath)
		if err != nil {
			return nil, fmt.Errorf("build package provider %q: %w", manifestPath, err)
		}
		if !named {
			continue
		}
		if _, seen := byName[packageValue.Name]; !seen {
			order = append(order, packageValue.Name)
		}
		byName[packageValue.Name] = append(byName[packageValue.Name], packageValue)
	}
	for _, name := range order {
		candidates := byName[name]
		if len(candidates) == 1 {
			registry.packages = append(registry.packages, candidates[0])
			continue
		}
		manifests := make([]string, 0, len(candidates))
		for _, candidate := range candidates {
			manifests = append(manifests, candidate.ManifestPath)
		}
		sort.Strings(manifests)
		registry.conflicts = append(registry.conflicts, TypeScriptPackageConflict{
			Name:      name,
			Manifests: manifests,
		})
	}
	sort.Slice(registry.packages, func(left, right int) bool {
		if registry.packages[left].Name != registry.packages[right].Name {
			return registry.packages[left].Name < registry.packages[right].Name
		}
		return registry.packages[left].ManifestPath < registry.packages[right].ManifestPath
	})
	sort.Slice(registry.conflicts, func(left, right int) bool {
		return registry.conflicts[left].Name < registry.conflicts[right].Name
	})
	registry.byName = make(map[string]int, len(registry.packages))
	for index, packageValue := range registry.packages {
		registry.byName[packageValue.Name] = index
	}
	return registry, nil
}

// Conflicts returns the package names this registry refused to resolve.
func (registry *TypeScriptPackageRegistry) Conflicts() []TypeScriptPackageConflict {
	if registry == nil {
		return nil
	}
	conflicts := make([]TypeScriptPackageConflict, len(registry.conflicts))
	for index, conflict := range registry.conflicts {
		conflicts[index] = TypeScriptPackageConflict{
			Name:      conflict.Name,
			Manifests: append([]string(nil), conflict.Manifests...),
		}
	}
	return conflicts
}

// List returns deep copies sorted by package name.
func (registry *TypeScriptPackageRegistry) List() []TypeScriptPackage {
	if registry == nil {
		return nil
	}
	packages := make([]TypeScriptPackage, len(registry.packages))
	for index, packageValue := range registry.packages {
		packages[index] = cloneTypeScriptPackage(packageValue)
	}
	return packages
}

// Get returns a deep copy of the package registered under name.
func (registry *TypeScriptPackageRegistry) Get(name string) (TypeScriptPackage, bool) {
	if registry == nil {
		return TypeScriptPackage{}, false
	}
	index, exists := registry.byName[strings.TrimSpace(name)]
	if !exists {
		return TypeScriptPackage{}, false
	}
	return cloneTypeScriptPackage(registry.packages[index]), true
}

type typeScriptPackageManifest struct {
	Name    string          `json:"name"`
	Version string          `json:"version"`
	Private bool            `json:"private"`
	Exports json.RawMessage `json:"exports"`
	Types   string          `json:"types"`
	Typings string          `json:"typings"`
	Source  string          `json:"source"`
}

type typeScriptConfigMetadata struct {
	ConfigDirectory string
	CompilerOptions struct {
		RootDir        string   `json:"rootDir"`
		RootDirs       []string `json:"rootDirs"`
		DeclarationDir string   `json:"declarationDir"`
	} `json:"compilerOptions"`
	Include []string `json:"include"`
	Files   []string `json:"files"`
}

func buildTypeScriptPackage(repository Repository, discovery TypeScriptDiscovery, manifestPath string) (TypeScriptPackage, bool, error) {
	data, err := readTypeScriptManifest(manifestPath)
	if err != nil {
		return TypeScriptPackage{}, false, err
	}
	var manifest typeScriptPackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return TypeScriptPackage{}, false, fmt.Errorf("parse JSON: %w", err)
	}
	name := strings.TrimSpace(manifest.Name)
	if name == "" {
		return TypeScriptPackage{}, false, nil
	}
	if err := validateTypeScriptPackageName(name); err != nil {
		return TypeScriptPackage{}, false, err
	}
	packageRoot := filepath.Dir(manifestPath)
	packageValue := TypeScriptPackage{
		Name:         name,
		Version:      strings.TrimSpace(manifest.Version),
		Private:      manifest.Private,
		Repository:   strings.TrimSpace(repository.Name),
		RootPath:     packageRoot,
		ManifestPath: filepath.Clean(manifestPath),
		Exports:      append(json.RawMessage(nil), manifest.Exports...),
	}
	if err := validateTypeScriptExports(packageRoot, manifest.Exports); err != nil {
		return TypeScriptPackage{}, false, err
	}
	typesValue := strings.TrimSpace(manifest.Types)
	if typesValue == "" {
		typesValue = strings.TrimSpace(manifest.Typings)
	}
	if typesValue != "" {
		packageValue.TypesPath, err = resolveTypeScriptPackagePath(packageRoot, typesValue, "types")
		if err != nil {
			return TypeScriptPackage{}, false, err
		}
	}
	project := nearestTypeScriptProject(discovery.Projects, packageRoot)
	if project != nil {
		packageValue.ProjectPath = project.ConfigPath
	}
	metadata, err := packageTypeScriptConfigMetadata(project)
	if err != nil {
		return TypeScriptPackage{}, false, err
	}
	// Solution tsconfigs used by Vite/React Router often contain only
	// references to the real application and node projects. The worker indexes
	// one project per package, so selecting that empty solution would produce a
	// successful but empty package. Prefer the first referenced project with
	// source inputs; ordinary single-project packages keep their current path.
	if project != nil && !typeScriptMetadataHasSources(metadata) {
		for _, reference := range project.References {
			for index := range discovery.Projects {
				candidate := &discovery.Projects[index]
				if filepath.Clean(candidate.ConfigPath) != filepath.Clean(reference) {
					continue
				}
				candidateMetadata, metadataErr := packageTypeScriptConfigMetadata(candidate)
				if metadataErr != nil {
					return TypeScriptPackage{}, false, metadataErr
				}
				if !typeScriptMetadataHasSources(candidateMetadata) {
					continue
				}
				project = candidate
				packageValue.ProjectPath = project.ConfigPath
				metadata = candidateMetadata
				break
			}
			if typeScriptMetadataHasSources(metadata) {
				break
			}
		}
	}
	packageValue.SourceRoots, packageValue.DeclarationRoots, err = resolveTypeScriptRoots(packageRoot, packageValue.TypesPath, metadata)
	if err != nil {
		return TypeScriptPackage{}, false, err
	}
	if strings.TrimSpace(manifest.Source) != "" {
		sourcePath, err := resolveTypeScriptPackagePath(packageRoot, manifest.Source, "source")
		if err != nil {
			return TypeScriptPackage{}, false, err
		}
		sourceRoot := sourcePath
		if filepath.Ext(filepath.Base(sourcePath)) != "" {
			sourceRoot = filepath.Dir(sourcePath)
		}
		packageValue.SourceRoots = appendUniqueTypeScriptPath(packageValue.SourceRoots, sourceRoot)
	}
	return packageValue, true, nil
}

func typeScriptMetadataHasSources(metadata typeScriptConfigMetadata) bool {
	return len(metadata.Include) > 0 || len(metadata.Files) > 0 ||
		len(metadata.CompilerOptions.RootDirs) > 0 ||
		strings.TrimSpace(metadata.CompilerOptions.RootDir) != ""
}

func validateTypeScriptPackageName(name string) error {
	if strings.ContainsAny(name, " \t\r\n") {
		return fmt.Errorf("package name %q contains whitespace", name)
	}
	if strings.HasPrefix(name, "@") {
		parts := strings.SplitN(name[1:], "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" || strings.Contains(parts[1], "/") {
			return fmt.Errorf("package name %q is not a valid scoped name", name)
		}
		return nil
	}
	if strings.Contains(name, "/") {
		return fmt.Errorf("package name %q is not a valid unscoped name", name)
	}
	return nil
}

func validateTypeScriptExports(packageRoot string, raw json.RawMessage) error {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return fmt.Errorf("exports: %w", err)
	}
	return validateTypeScriptExportValue(packageRoot, value)
}

func validateTypeScriptExportValue(packageRoot string, value any) error {
	switch value := value.(type) {
	case string:
		if !strings.HasPrefix(value, "./") && !strings.HasPrefix(value, "../") && !filepath.IsAbs(value) {
			return nil
		}
		if _, err := resolveTypeScriptPackagePath(packageRoot, value, "exports"); err != nil {
			return err
		}
	case []any:
		for _, item := range value {
			if err := validateTypeScriptExportValue(packageRoot, item); err != nil {
				return err
			}
		}
	case map[string]any:
		for _, item := range value {
			if err := validateTypeScriptExportValue(packageRoot, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func resolveTypeScriptPackagePath(packageRoot, rawPath, field string) (string, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", fmt.Errorf("%s path must not be empty", field)
	}
	candidate := filepath.FromSlash(rawPath)
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(packageRoot, candidate)
	}
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("%s path %q: make absolute: %w", field, rawPath, err)
	}
	absolute = filepath.Clean(absolute)
	if !pathWithin(packageRoot, absolute) {
		return "", fmt.Errorf("%s path %q escapes package root %q", field, rawPath, packageRoot)
	}
	return absolute, nil
}

func nearestTypeScriptProject(projects []TypeScriptProject, packageRoot string) *TypeScriptProject {
	var nearest *TypeScriptProject
	for index := range projects {
		project := &projects[index]
		projectRoot := filepath.Dir(project.ConfigPath)
		if !pathWithin(projectRoot, packageRoot) {
			continue
		}
		if nearest == nil || typeScriptProjectLessSpecific(nearest, project) {
			nearest = project
		}
	}
	return nearest
}

func typeScriptProjectLessSpecific(current, candidate *TypeScriptProject) bool {
	currentRoot := filepath.Dir(current.ConfigPath)
	candidateRoot := filepath.Dir(candidate.ConfigPath)
	if len(currentRoot) != len(candidateRoot) {
		return len(currentRoot) < len(candidateRoot)
	}
	currentPrimary := filepath.Base(current.ConfigPath) == "tsconfig.json"
	candidatePrimary := filepath.Base(candidate.ConfigPath) == "tsconfig.json"
	if currentPrimary != candidatePrimary {
		return !currentPrimary && candidatePrimary
	}
	return candidate.ConfigPath < current.ConfigPath
}

func packageTypeScriptConfigMetadata(project *TypeScriptProject) (typeScriptConfigMetadata, error) {
	if project == nil {
		return typeScriptConfigMetadata{}, nil
	}
	data, err := readTypeScriptManifest(project.ConfigPath)
	if err != nil {
		return typeScriptConfigMetadata{}, fmt.Errorf("read project %q: %w", project.ConfigPath, err)
	}
	data, err = decodeJSONC(data)
	if err != nil {
		return typeScriptConfigMetadata{}, fmt.Errorf("parse project %q: %w", project.ConfigPath, err)
	}
	var metadata typeScriptConfigMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return typeScriptConfigMetadata{}, fmt.Errorf("parse project %q: %w", project.ConfigPath, err)
	}
	metadata.ConfigDirectory = filepath.Dir(project.ConfigPath)
	return metadata, nil
}

func resolveTypeScriptRoots(packageRoot, typesPath string, metadata typeScriptConfigMetadata) ([]string, []string, error) {
	sourceRoots := make([]string, 0)
	declarationRoots := make([]string, 0)
	configDirectory := packageRoot
	if metadata.ConfigDirectory != "" {
		configDirectory = metadata.ConfigDirectory
	}
	rootCandidates := metadata.CompilerOptions.RootDirs
	if len(rootCandidates) == 0 && strings.TrimSpace(metadata.CompilerOptions.RootDir) != "" {
		rootCandidates = []string{metadata.CompilerOptions.RootDir}
	}
	for _, candidate := range rootCandidates {
		resolved, err := resolveConfigRelativePath(configDirectory, candidate, "source root")
		if err != nil {
			return nil, nil, err
		}
		if !pathWithin(packageRoot, resolved) {
			continue
		}
		sourceRoots = appendUniqueTypeScriptPath(sourceRoots, resolved)
	}
	for _, pattern := range metadata.Include {
		base := typeScriptPatternBase(pattern)
		resolved, err := resolveConfigRelativePath(configDirectory, base, "include")
		if err != nil {
			return nil, nil, err
		}
		if !pathWithin(packageRoot, resolved) {
			continue
		}
		sourceRoots = appendUniqueTypeScriptPath(sourceRoots, resolved)
	}
	for _, file := range metadata.Files {
		resolved, err := resolveConfigRelativePath(configDirectory, filepath.Dir(file), "file")
		if err != nil {
			return nil, nil, err
		}
		if !pathWithin(packageRoot, resolved) {
			continue
		}
		sourceRoots = appendUniqueTypeScriptPath(sourceRoots, resolved)
	}
	if len(sourceRoots) == 0 {
		sourceRoots = append(sourceRoots, packageRoot)
	}
	if typesPath != "" {
		declarationRoots = append(declarationRoots, filepath.Dir(typesPath))
	}
	if metadata.CompilerOptions.DeclarationDir != "" {
		resolved, err := resolveConfigRelativePath(configDirectory, metadata.CompilerOptions.DeclarationDir, "declaration root")
		if err != nil {
			return nil, nil, err
		}
		if pathWithin(packageRoot, resolved) {
			declarationRoots = appendUniqueTypeScriptPath(declarationRoots, resolved)
		}
	}
	return sourceRoots, declarationRoots, nil

}
func resolveConfigRelativePath(configDirectory, rawPath, field string) (string, error) {
	candidate := filepath.FromSlash(strings.TrimSpace(rawPath))
	if candidate == "" {
		return "", fmt.Errorf("%s path must not be empty", field)
	}
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(configDirectory, candidate)
	}
	absolute, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("%s path %q: make absolute: %w", field, rawPath, err)
	}
	return filepath.Clean(absolute), nil
}

func typeScriptPatternBase(pattern string) string {
	pattern = strings.TrimSpace(pattern)
	for index, character := range pattern {
		if character == '*' || character == '?' || character == '[' || character == '{' {
			pattern = pattern[:index]
			break
		}
	}
	pattern = strings.TrimRight(pattern, "/\\")
	if pattern == "" {
		return "."
	}
	return pattern
}

func appendUniqueTypeScriptPath(paths []string, value string) []string {
	for _, existing := range paths {
		if existing == value {
			return paths
		}
	}
	return append(paths, value)
}

func cloneTypeScriptPackage(packageValue TypeScriptPackage) TypeScriptPackage {
	packageValue.Exports = append(json.RawMessage(nil), packageValue.Exports...)
	packageValue.SourceRoots = append([]string(nil), packageValue.SourceRoots...)
	packageValue.DeclarationRoots = append([]string(nil), packageValue.DeclarationRoots...)
	return packageValue
}

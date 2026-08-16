package workspace

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/semver"
)

// TypeScriptVersionSource records where the compiler version came from. The
// order of resolution is local, then workspace, then the pinned fallback, as
// required by LUQUE-0605.
type TypeScriptVersionSource string

const (
	// TypeScriptVersionLocal is a compiler installed inside the project's own
	// package.
	TypeScriptVersionLocal TypeScriptVersionSource = "local"
	// TypeScriptVersionWorkspace is a compiler hoisted to an ancestor of the
	// project but still inside the repository.
	TypeScriptVersionWorkspace TypeScriptVersionSource = "workspace"
	// TypeScriptVersionPinned is the compiler Kivgraph ships, used when the
	// project has none installed.
	TypeScriptVersionPinned TypeScriptVersionSource = "pinned"
)

const typeScriptPackageDirectory = "typescript"

// TypeScriptEngine describes the compiler Kivgraph actually runs and the version
// window whose facts may be exact. It mirrors the HELLO announcement of the
// worker; per ADR 0010 the engine never changes with the project, only the
// confidence of the facts does.
type TypeScriptEngine struct {
	// Version is the pinned compiler, used as the last resort.
	Version string
	// SupportedMin and SupportedMax bound the window of project versions whose
	// facts may be emitted as exact. An empty bound is open.
	SupportedMin string
	SupportedMax string
}

// TypeScriptVersion is the compiler resolved for one project.
type TypeScriptVersion struct {
	// Version is the resolved compiler version, without a leading v.
	Version string
	// Source is which of the three resolution steps produced it.
	Source TypeScriptVersionSource
	// ManifestPath is the package.json of the resolved compiler. It is empty
	// for the pinned fallback, which is not installed in the repository.
	ManifestPath string
	// Declared is the range the nearest package.json asks for, when it does.
	// It is evidence for auditing, never the resolved version.
	Declared string
	// DeclaredBy is the manifest carrying Declared.
	DeclaredBy string
	// WithinSupportedWindow reports whether facts from this project may be
	// exact. Outside the window they must degrade to CANDIDATE.
	WithinSupportedWindow bool
	// OutsideWindowReason explains a false WithinSupportedWindow. It is empty
	// when the project is inside the window.
	OutsideWindowReason string
}

// TypeScriptVersionResolver resolves the compiler of each project inside one
// repository. It is immutable and safe for concurrent use.
type TypeScriptVersionResolver struct {
	repositoryRoot string
	engine         TypeScriptEngine
}

// NewTypeScriptVersionResolver builds a resolver bounded by the repository
// root. The walk never leaves that root, so a compiler installed outside the
// registered repository cannot silently decide the confidence of its facts.
func NewTypeScriptVersionResolver(repository Repository, engine TypeScriptEngine) (*TypeScriptVersionResolver, error) {
	root := strings.TrimSpace(repository.RealPath)
	if root == "" {
		return nil, errors.New("repository real path must not be empty")
	}
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("repository real path must be absolute, got %q", root)
	}
	if strings.TrimSpace(engine.Version) == "" {
		return nil, errors.New("engine version must not be empty")
	}
	if !isSemanticVersion(engine.Version) {
		return nil, fmt.Errorf("engine version %q is not a semantic version", engine.Version)
	}
	for _, bound := range []struct {
		field string
		value string
	}{{"minimum", engine.SupportedMin}, {"maximum", engine.SupportedMax}} {
		if bound.value != "" && !isSemanticVersion(bound.value) {
			return nil, fmt.Errorf("supported %s %q is not a semantic version", bound.field, bound.value)
		}
	}
	if engine.SupportedMin != "" && engine.SupportedMax != "" &&
		compareVersions(engine.SupportedMin, engine.SupportedMax) > 0 {
		return nil, fmt.Errorf("supported window %s..%s is inverted", engine.SupportedMin, engine.SupportedMax)
	}
	return &TypeScriptVersionResolver{repositoryRoot: filepath.Clean(root), engine: engine}, nil
}

// Engine returns the compiler this resolver falls back to.
func (resolver *TypeScriptVersionResolver) Engine() TypeScriptEngine {
	return resolver.engine
}

// TypeScriptProjectVersion binds one discovered project to its compiler.
type TypeScriptProjectVersion struct {
	ConfigPath string
	TypeScriptVersion
}

// ResolveProjects resolves the compiler of every discovered project, in the
// order the discovery reported them. One unreadable install fails the whole
// batch: a repository indexed with a version Kivgraph could not determine would
// produce facts whose confidence nobody can audit.
func (resolver *TypeScriptVersionResolver) ResolveProjects(discovery TypeScriptDiscovery) ([]TypeScriptProjectVersion, error) {
	if resolver == nil {
		return nil, errors.New("resolver must not be nil")
	}
	resolved := make([]TypeScriptProjectVersion, 0, len(discovery.Projects))
	for _, project := range discovery.Projects {
		version, err := resolver.Resolve(project.ConfigPath)
		if err != nil {
			return nil, fmt.Errorf("resolve TypeScript for %s: %w", project.ConfigPath, err)
		}
		resolved = append(resolved, TypeScriptProjectVersion{ConfigPath: project.ConfigPath, TypeScriptVersion: version})
	}
	return resolved, nil
}

// Resolve returns the compiler for the project whose tsconfig lives at
// configPath.
func (resolver *TypeScriptVersionResolver) Resolve(configPath string) (TypeScriptVersion, error) {
	if resolver == nil {
		return TypeScriptVersion{}, errors.New("resolver must not be nil")
	}
	if !filepath.IsAbs(configPath) {
		return TypeScriptVersion{}, fmt.Errorf("project config path must be absolute, got %q", configPath)
	}
	cleaned := filepath.Clean(configPath)
	if !isWithinRoot(resolver.repositoryRoot, cleaned) {
		return TypeScriptVersion{}, fmt.Errorf("project %q is outside repository %q", cleaned, resolver.repositoryRoot)
	}

	projectDirectory := filepath.Dir(cleaned)
	packageRoot, declared, declaredBy, err := resolver.nearestDeclaration(projectDirectory)
	if err != nil {
		return TypeScriptVersion{}, err
	}

	resolved := TypeScriptVersion{Declared: declared, DeclaredBy: declaredBy}
	installedPath, installedVersion, err := resolver.findInstalled(projectDirectory)
	if err != nil {
		return TypeScriptVersion{}, err
	}
	switch {
	case installedVersion == "":
		resolved.Version = resolver.engine.Version
		resolved.Source = TypeScriptVersionPinned
	default:
		resolved.Version = installedVersion
		resolved.ManifestPath = installedPath
		resolved.Source = TypeScriptVersionWorkspace
		// A compiler under the project's own package root is local; anything
		// found further up was hoisted by the workspace.
		if packageRoot != "" && isWithinRoot(packageRoot, installedPath) {
			resolved.Source = TypeScriptVersionLocal
		}
	}

	resolved.WithinSupportedWindow, resolved.OutsideWindowReason = resolver.withinWindow(resolved.Version)
	return resolved, nil
}

// nearestDeclaration finds the package.json closest to the project and the
// TypeScript range it declares, if any.
func (resolver *TypeScriptVersionResolver) nearestDeclaration(projectDirectory string) (string, string, string, error) {
	for directory := projectDirectory; ; directory = filepath.Dir(directory) {
		manifestPath := filepath.Join(directory, "package.json")
		declared, found, err := readDeclaredTypeScript(manifestPath)
		if err != nil {
			return "", "", "", err
		}
		if found {
			if declared == "" {
				return directory, "", "", nil
			}
			return directory, declared, manifestPath, nil
		}
		if directory == resolver.repositoryRoot || !isWithinRoot(resolver.repositoryRoot, directory) {
			return "", "", "", nil
		}
	}
}

// findInstalled walks up from the project looking for node_modules/typescript,
// which is how Node resolves the compiler.
func (resolver *TypeScriptVersionResolver) findInstalled(projectDirectory string) (string, string, error) {
	for directory := projectDirectory; ; directory = filepath.Dir(directory) {
		manifestPath := filepath.Join(directory, "node_modules", typeScriptPackageDirectory, "package.json")
		version, found, err := readInstalledTypeScript(manifestPath)
		if err != nil {
			return "", "", err
		}
		if found {
			return manifestPath, version, nil
		}
		if directory == resolver.repositoryRoot || !isWithinRoot(resolver.repositoryRoot, directory) {
			return "", "", nil
		}
	}
}

// withinWindow decides whether facts from this version may be exact.
func (resolver *TypeScriptVersionResolver) withinWindow(version string) (bool, string) {
	if !isSemanticVersion(version) {
		return false, fmt.Sprintf("version %q is not a semantic version", version)
	}
	if minimum := resolver.engine.SupportedMin; minimum != "" && compareVersions(version, minimum) < 0 {
		return false, fmt.Sprintf("version %s is below the supported minimum %s", version, minimum)
	}
	if maximum := resolver.engine.SupportedMax; maximum != "" && compareVersions(version, maximum) > 0 {
		return false, fmt.Sprintf("version %s is above the supported maximum %s", version, maximum)
	}
	return true, ""
}

type typeScriptDependencyManifest struct {
	Dependencies         map[string]string `json:"dependencies"`
	DevDependencies      map[string]string `json:"devDependencies"`
	PeerDependencies     map[string]string `json:"peerDependencies"`
	OptionalDependencies map[string]string `json:"optionalDependencies"`
}

// readDeclaredTypeScript reports the range a manifest asks for. The second
// result distinguishes a missing manifest from one without TypeScript.
func readDeclaredTypeScript(manifestPath string) (string, bool, error) {
	data, err := readTypeScriptManifest(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	// package.json is strict JSON by specification, and the rest of this
	// package already parses it that way.
	var manifest typeScriptDependencyManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", false, fmt.Errorf("parse %s: %w", manifestPath, err)
	}
	// devDependencies first: a compiler is a build tool, and a project that
	// lists it in both places builds with the dev entry.
	for _, group := range []map[string]string{
		manifest.DevDependencies,
		manifest.Dependencies,
		manifest.PeerDependencies,
		manifest.OptionalDependencies,
	} {
		if declared, ok := group[typeScriptPackageDirectory]; ok {
			return strings.TrimSpace(declared), true, nil
		}
	}
	return "", true, nil
}

type installedTypeScriptManifest struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// readInstalledTypeScript reads the version of an installed compiler.
func readInstalledTypeScript(manifestPath string) (string, bool, error) {
	data, err := readTypeScriptManifest(manifestPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, err
	}
	var manifest installedTypeScriptManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", false, fmt.Errorf("parse %s: %w", manifestPath, err)
	}
	if manifest.Name != typeScriptPackageDirectory {
		return "", false, fmt.Errorf("%s declares package %q, want %q", manifestPath, manifest.Name, typeScriptPackageDirectory)
	}
	version := strings.TrimSpace(manifest.Version)
	if version == "" {
		return "", false, fmt.Errorf("%s declares no version", manifestPath)
	}
	return version, true, nil
}

// isWithinRoot reports whether candidate is root or lives under it.
func isWithinRoot(root, candidate string) bool {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return false
	}
	return relative == "." || (!strings.HasPrefix(relative, ".."+string(filepath.Separator)) && relative != "..")
}

// isSemanticVersion reports whether value is a semantic version. Prereleases
// such as 7.0.0-beta are accepted; ranges such as ^5.9.3 are not.
func isSemanticVersion(value string) bool {
	return semver.IsValid(semanticVersion(value))
}

// compareVersions orders two semantic versions.
func compareVersions(left, right string) int {
	return semver.Compare(semanticVersion(left), semanticVersion(right))
}

func semanticVersion(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "v") {
		return trimmed
	}
	return "v" + trimmed
}

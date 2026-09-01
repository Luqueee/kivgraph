// Package workspace discovers what a registered repository actually contains
// -- its modules, crates, packages and sources -- and which provider claims
// each file.
package workspace

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/topology"
)

// Repository is the runtime metadata registered for one source repository.
type Repository struct {
	Name       string
	Path       string
	RealPath   string
	Commit     string
	Branch     string
	Dirty      bool
	Languages  []string
	Manifests  []string
	Roots      []string
	Exclusions []string
}

// Registry is an immutable, name-indexed set of registered repositories.
type Registry struct {
	repositories []Repository
	byName       map[string]int
	composition  *topology.ProfileComposition
}

type gitRunner func(context.Context, string, ...string) (string, error)

// NewSyntheticRepository builds a repository that is not registered and has no
// version control: a provider the pass derives from the machine rather than
// from `repositories.yaml`. The standard library of a Rust toolchain is the
// only one today.
//
// The path policy is the registry's, unchanged: a synthetic provider that
// resolved through a symlink component would let a pass read outside the tree
// it declared, and the reason this exists is to index code, not to relax that.
func NewSyntheticRepository(name, path string, languages []string) (Repository, error) {
	trimmed := strings.TrimSpace(name)
	if err := validateRepositoryIdentifier(trimmed); err != nil {
		return Repository{}, fmt.Errorf("synthetic repository: %w", err)
	}
	if !IsSyntheticRepository(trimmed) {
		return Repository{}, fmt.Errorf("synthetic repository %q: a derived provider must take the %q namespace",
			trimmed, SyntheticRepositoryPrefix)
	}
	resolved, realPath, err := inspectRepositoryPath(path)
	if err != nil {
		return Repository{}, fmt.Errorf("synthetic repository %q: %w", trimmed, err)
	}
	return Repository{
		Name:      trimmed,
		Path:      resolved,
		RealPath:  realPath,
		Languages: append([]string(nil), languages...),
	}, nil
}

// NewRegistry resolves configured paths, validates repository boundaries and
// records filesystem and Git metadata. It preserves the order from
// repositories.yaml.
func NewRegistry(ctx context.Context, source config.RepositoriesFile) (*Registry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return newRegistry(ctx, source, runGit)
}

func newRegistry(ctx context.Context, source config.RepositoriesFile, git gitRunner) (*Registry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	validatedPaths, err := validatePaths(ctx, source)
	if err != nil {
		return nil, fmt.Errorf("validate paths: %w", err)
	}
	registry := &Registry{
		repositories: make([]Repository, 0, len(source.Repositories)),
		byName:       make(map[string]int, len(source.Repositories)),
	}
	for index, configured := range source.Repositories {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		repository, err := registerRepository(ctx, configured, validatedPaths[index], git)
		if err != nil {
			return nil, fmt.Errorf("register repositories[%d] %q: %w", index, configured.Name, err)
		}
		if previous, exists := registry.byName[repository.Name]; exists {
			return nil, fmt.Errorf("register repositories[%d] %q: duplicate name of repositories[%d]", index, repository.Name, previous)
		}
		registry.byName[repository.Name] = len(registry.repositories)
		registry.repositories = append(registry.repositories, repository)
	}
	return registry, nil
}

func registerRepository(ctx context.Context, configured config.Repository, validated validatedRepositoryPath, git gitRunner) (Repository, error) {
	name := strings.TrimSpace(configured.Name)
	if name == "" {
		return Repository{}, fmt.Errorf("name must not be empty")
	}
	languages, err := normalizeLanguages(configured.Languages)
	if err != nil {
		return Repository{}, err
	}
	path := validated.path
	realPath := validated.realPath

	commit, err := git(ctx, realPath, "rev-parse", "HEAD")
	if err != nil {
		return Repository{}, fmt.Errorf("read commit: %w", err)
	}
	if commit == "" {
		return Repository{}, fmt.Errorf("read commit: git returned an empty value")
	}
	branch, err := git(ctx, realPath, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		branch, err = git(ctx, realPath, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return Repository{}, fmt.Errorf("read branch: %w", err)
		}
	}
	if branch == "" {
		return Repository{}, fmt.Errorf("read branch: git returned an empty value")
	}
	status, err := git(ctx, realPath, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return Repository{}, fmt.Errorf("read dirty state: %w", err)
	}

	manifests, err := resolveRepositoryPaths(realPath, configured.Manifests, "manifests")
	if err != nil {
		return Repository{}, err
	}
	roots, err := resolveRepositoryPaths(realPath, configured.Roots, "roots")
	if err != nil {
		return Repository{}, err
	}
	exclusions, err := copyPatterns(configured.Exclusions, "exclusions")
	if err != nil {
		return Repository{}, err
	}
	return Repository{
		Name:       name,
		Path:       path,
		RealPath:   filepath.Clean(realPath),
		Commit:     commit,
		Branch:     branch,
		Dirty:      strings.TrimSpace(status) != "",
		Languages:  languages,
		Manifests:  manifests,
		Roots:      roots,
		Exclusions: exclusions,
	}, nil
}

// List returns a deep copy in the order declared by repositories.yaml.
func (registry *Registry) List() []Repository {
	if registry == nil {
		return nil
	}
	repositories := make([]Repository, len(registry.repositories))
	for index, repository := range registry.repositories {
		repositories[index] = cloneRepository(repository)
	}
	return repositories
}

// Get returns a deep copy of the repository registered under name.
func (registry *Registry) Get(name string) (Repository, bool) {
	if registry == nil {
		return Repository{}, false
	}
	index, exists := registry.byName[strings.TrimSpace(name)]
	if !exists {
		return Repository{}, false
	}
	return cloneRepository(registry.repositories[index]), true
}

// Composition returns the profile membership and selected worktrees that
// produced this registry. The result is a copy suitable for diagnostics; a
// plain registry has no composition provenance.
func (registry *Registry) Composition() (topology.ProfileComposition, bool) {
	if registry == nil || registry.composition == nil {
		return topology.ProfileComposition{}, false
	}
	return cloneProfileComposition(*registry.composition), true
}

func cloneRepository(repository Repository) Repository {
	repository.Languages = append([]string(nil), repository.Languages...)
	repository.Manifests = append([]string(nil), repository.Manifests...)
	repository.Roots = append([]string(nil), repository.Roots...)
	repository.Exclusions = append([]string(nil), repository.Exclusions...)
	return repository
}

func resolveRepositoryPaths(base string, values []string, field string) ([]string, error) {
	if values == nil {
		return nil, nil
	}
	resolved := make([]string, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s[%d]: must not be empty", field, index)
		}
		if !filepath.IsAbs(value) {
			value = filepath.Join(base, value)
		}
		absolute, err := filepath.Abs(value)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: make path absolute: %w", field, index, err)
		}
		resolved[index] = filepath.Clean(absolute)
	}
	return resolved, nil
}

func normalizeLanguages(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("languages: must contain at least one language")
	}
	languages := make([]string, len(values))
	seen := make(map[string]struct{}, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("languages[%d]: must not be empty", index)
		}
		if _, exists := seen[value]; exists {
			return nil, fmt.Errorf("languages[%d]: duplicate language %q", index, value)
		}
		seen[value] = struct{}{}
		languages[index] = value
	}
	return languages, nil
}

func copyPatterns(values []string, field string) ([]string, error) {
	if values == nil {
		return nil, nil
	}
	patterns := make([]string, len(values))
	for index, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return nil, fmt.Errorf("%s[%d]: must not be empty", field, index)
		}
		patterns[index] = value
	}
	return patterns, nil
}

func runGit(ctx context.Context, directory string, arguments ...string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	command := exec.CommandContext(ctx, "git", arguments...)
	command.Dir = directory
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.Output()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		detail := strings.TrimSpace(stderr.String())
		if detail != "" {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(arguments, " "), err, detail)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(arguments, " "), err)
	}
	return strings.TrimSpace(string(output)), nil
}

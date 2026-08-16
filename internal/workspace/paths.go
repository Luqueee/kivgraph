package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Luqueee/kivgraph/internal/config"
)

type validatedRepositoryPath struct {
	path     string
	realPath string
}

// ValidatePaths validates repository boundaries without invoking Git.
// It is safe to call before NewRegistry when only filesystem validation is
// required.
func ValidatePaths(ctx context.Context, source config.RepositoriesFile) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := validatePaths(ctx, source)
	return err
}

func validatePaths(ctx context.Context, source config.RepositoriesFile) ([]validatedRepositoryPath, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	validated := make([]validatedRepositoryPath, 0, len(source.Repositories))
	seenNames := make(map[string]int, len(source.Repositories))
	for index, repository := range source.Repositories {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(repository.Name)
		if err := validateRepositoryName(name); err != nil {
			return nil, fmt.Errorf("validate repositories[%d]: %w", index, err)
		}
		// Names are compared exactly. A repository name is an
		// identifier, never a path component -- nothing derives a
		// directory, a temporary file or a state entry from it -- and
		// the stable keys that carry it are case sensitive. A monorepo
		// that really holds `tempvoice-module` and `tempVoice-module`
		// as two git repositories has to be nameable, and an alias
		// invented to get past this check makes the graph name
		// something that does not exist on disk.
		if previous, exists := seenNames[name]; exists {
			return nil, fmt.Errorf("validate repositories[%d] %q: name collision with repositories[%d]", index, name, previous)
		}
		seenNames[name] = index

		path, realPath, err := inspectRepositoryPath(repository.Path)
		if err != nil {
			return nil, fmt.Errorf("validate repositories[%d] %q: %w", index, name, err)
		}
		if err := validateRepositoryScopedPaths(realPath, repository); err != nil {
			return nil, fmt.Errorf("validate repositories[%d] %q: %w", index, name, err)
		}
		validated = append(validated, validatedRepositoryPath{path: path, realPath: realPath})
	}
	if err := validateRepositoryRelationships(validated, source.Repositories); err != nil {
		return nil, err
	}
	return validated, nil
}

// SyntheticRepositoryPrefix is the namespace of the providers Kivgraph derives
// from the machine instead of from `repositories.yaml`. The standard library of
// a Rust toolchain is registered as `rust:1.96.1`.
//
// The namespace is reserved at registration, and that is what makes the name
// authoritative: nothing else in the graph can be called this, so a consumer
// can tell a derived provider from an indexed repository without a second
// column to carry the answer.
const SyntheticRepositoryPrefix = "rust:"

// IsSyntheticRepository reports whether a repository name belongs to the
// reserved namespace.
func IsSyntheticRepository(name string) bool {
	return strings.HasPrefix(strings.TrimSpace(name), SyntheticRepositoryPrefix)
}

// validateRepositoryName checks a name a user chose, which may not take the
// reserved namespace.
func validateRepositoryName(name string) error {
	if err := validateRepositoryIdentifier(name); err != nil {
		return err
	}
	if IsSyntheticRepository(name) {
		return fmt.Errorf("name %q is reserved: the %q namespace names the providers Kivgraph derives from the toolchain",
			name, SyntheticRepositoryPrefix)
	}
	return nil
}

// validateRepositoryIdentifier checks the shape every repository name has,
// including the synthetic ones this package mints itself.
func validateRepositoryIdentifier(name string) error {
	if name == "" {
		return fmt.Errorf("name must not be empty")
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\\`) {
		return fmt.Errorf("name %q is not a valid repository identifier", name)
	}
	return nil
}

func inspectRepositoryPath(rawPath string) (string, string, error) {
	if !filepath.IsAbs(rawPath) {
		return "", "", fmt.Errorf("path must be absolute, got %q", rawPath)
	}
	path := filepath.Clean(rawPath)
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("path %q does not exist or is inaccessible: %w", path, err)
	}
	if !info.IsDir() {
		return "", "", fmt.Errorf("path %q is not a directory", path)
	}
	if symlink, err := FirstSymlink(path); err != nil {
		return "", "", fmt.Errorf("inspect symlinks for %q: %w", path, err)
	} else if symlink != "" {
		return "", "", fmt.Errorf("path %q contains symlink component %q", path, symlink)
	}
	if err := validateDirectoryPermissions(path, info); err != nil {
		return "", "", err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", fmt.Errorf("resolve realpath %q: %w", path, err)
	}
	realPath, err = filepath.Abs(realPath)
	if err != nil {
		return "", "", fmt.Errorf("make realpath absolute for %q: %w", path, err)
	}
	return path, filepath.Clean(realPath), nil
}

func validateDirectoryPermissions(path string, info os.FileInfo) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	permissions := info.Mode().Perm()
	if permissions&0444 == 0 || permissions&0111 == 0 {
		return fmt.Errorf("path %q is not readable and searchable (permissions %04o)", path, permissions)
	}
	if permissions&0002 != 0 {
		return fmt.Errorf("path %q is world-writable (permissions %04o)", path, permissions)
	}
	return nil
}

// FirstSymlink returns the first component of path that is a symbolic link,
// walking from the filesystem root inwards, or the empty string when none is.
//
// It is exported because two layers enforce the same policy for the same reason:
// this package refuses to index through a link, and the MCP surface refuses to
// serve bytes through one. Duplicating four lines of a security check is how two
// copies of it end up disagreeing.
func FirstSymlink(path string) (string, error) {
	path = filepath.Clean(path)
	components := make([]string, 0, 8)
	for current := path; ; current = filepath.Dir(current) {
		components = append(components, current)
		parent := filepath.Dir(current)
		if parent == current {
			break
		}
	}
	for index := len(components) - 1; index >= 0; index-- {
		info, err := os.Lstat(components[index])
		if err != nil {
			return "", err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return components[index], nil
		}
	}
	return "", nil
}

func validateRepositoryScopedPaths(base string, repository config.Repository) error {
	for index, value := range repository.Manifests {
		if err := validateScopedPath(base, value); err != nil {
			return fmt.Errorf("manifests[%d]: %w", index, err)
		}
	}
	for index, value := range repository.Roots {
		if err := validateScopedPath(base, value); err != nil {
			return fmt.Errorf("roots[%d]: %w", index, err)
		}
	}
	for index, value := range repository.Exclusions {
		if err := validateScopedPath(base, value); err != nil {
			return fmt.Errorf("exclusions[%d]: %w", index, err)
		}
	}
	return nil
}

func validateScopedPath(base, rawPath string) error {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return fmt.Errorf("must not be empty")
	}
	candidate := rawPath
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(base, candidate)
	}
	candidate, err := filepath.Abs(candidate)
	if err != nil {
		return fmt.Errorf("make path absolute: %w", err)
	}
	candidate = filepath.Clean(candidate)
	if !pathWithin(base, candidate) {
		return fmt.Errorf("path %q escapes repository realpath %q", rawPath, base)
	}

	if _, err := os.Lstat(candidate); err == nil {
		if symlink, err := FirstSymlink(candidate); err != nil {
			return fmt.Errorf("inspect symlinks for %q: %w", rawPath, err)
		} else if symlink != "" {
			return fmt.Errorf("path %q contains symlink component %q", rawPath, symlink)
		}
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return fmt.Errorf("resolve realpath %q: %w", rawPath, err)
		}
		resolved, err = filepath.Abs(resolved)
		if err != nil {
			return fmt.Errorf("make realpath absolute for %q: %w", rawPath, err)
		}
		if !pathWithin(base, filepath.Clean(resolved)) {
			return fmt.Errorf("path %q escapes repository realpath %q after symlink resolution", rawPath, base)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect path %q: %w", rawPath, err)
	}
	return nil
}

func validateRepositoryRelationships(paths []validatedRepositoryPath, repositories []config.Repository) error {
	for left := range paths {
		for right := left + 1; right < len(paths); right++ {
			if paths[left].realPath == paths[right].realPath {
				return fmt.Errorf("validate repositories[%d] %q and repositories[%d] %q: duplicate realpath %q", left, repositories[left].Name, right, repositories[right].Name, paths[left].realPath)
			}
			if pathContains(paths[left].realPath, paths[right].realPath) {
				return fmt.Errorf("validate repositories[%d] %q and repositories[%d] %q: nested repositories (%q contains %q)", left, repositories[left].Name, right, repositories[right].Name, paths[left].realPath, paths[right].realPath)
			}
			if pathContains(paths[right].realPath, paths[left].realPath) {
				return fmt.Errorf("validate repositories[%d] %q and repositories[%d] %q: nested repositories (%q contains %q)", right, repositories[right].Name, left, repositories[left].Name, paths[right].realPath, paths[left].realPath)
			}
		}
	}
	return nil
}

func pathWithin(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)))
}

// validateContainedPath answers the realpath of a target that must stay inside
// a repository. A symlink component is rejected rather than resolved: the
// resolution would change which file the facts describe.
func validateContainedPath(repositoryRoot, target string) (string, error) {
	if symlink, err := FirstSymlink(target); err != nil {
		return "", fmt.Errorf("inspect symlinks for %q: %w", target, err)
	} else if symlink != "" {
		return "", fmt.Errorf("path %q contains symlink component %q", target, symlink)
	}
	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("resolve realpath %q: %w", target, err)
	}
	resolved, err = filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("make realpath absolute: %w", err)
	}
	resolved = filepath.Clean(resolved)
	if !pathWithin(repositoryRoot, resolved) {
		return "", fmt.Errorf("path %q resolves outside repository realpath %q", target, repositoryRoot)
	}
	return resolved, nil
}

func pathContains(parent, child string) bool {
	relative, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return relative != "." && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

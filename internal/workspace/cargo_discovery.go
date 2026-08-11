package workspace

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var defaultCargoExcludedDirectories = map[string]struct{}{
	".git":         {},
	"target":       {},
	"vendor":       {},
	"node_modules": {},
}

// DiscoverCargo finds Cargo manifests, workspaces, crates and lockfiles below
// a repository root. Results are deterministic and use absolute, canonical
// paths.
//
// Nothing here runs `cargo`: discovery must be hermetic, cheap and free of
// writes, and a manifest already says what a crate is called and which
// workspace resolves it.
func DiscoverCargo(ctx context.Context, repository Repository) (CargoDiscovery, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return CargoDiscovery{}, err
	}
	rootInput := repository.RealPath
	if strings.TrimSpace(rootInput) == "" {
		rootInput = repository.Path
	}
	_, root, err := inspectRepositoryPath(rootInput)
	if err != nil {
		return CargoDiscovery{}, fmt.Errorf("discover Cargo root: %w", err)
	}

	manifests := make(map[string]cargoManifest)
	lockFiles := make(map[string]struct{})
	err = walkCargoFiles(ctx, root, root, repository.Exclusions, func(filePath string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch filepath.Base(filePath) {
		case "Cargo.toml":
			parsed, err := parseCargoManifest(filePath)
			if err != nil {
				return fmt.Errorf("parse Cargo manifest %q: %w", filePath, err)
			}
			manifests[parsed.path] = parsed
		case "Cargo.lock":
			lockFiles[filePath] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return CargoDiscovery{}, err
	}

	workspaces, membership, err := resolveCargoWorkspaces(root, manifests)
	if err != nil {
		return CargoDiscovery{}, err
	}
	crates := make(map[string]CargoCrate, len(manifests))
	for path, manifest := range manifests {
		if manifest.pkg == nil {
			continue
		}
		owner, resolved := membership[path]
		if !resolved {
			return CargoDiscovery{}, fmt.Errorf("crate manifest %q belongs to no workspace", path)
		}
		crate, err := resolveCargoCrate(manifest, manifests[owner])
		if err != nil {
			return CargoDiscovery{}, fmt.Errorf("resolve crate %q: %w", path, err)
		}
		crates[path] = crate
	}

	for path, workspace := range workspaces {
		lockPath := filepath.Join(workspace.RootPath, "Cargo.lock")
		if _, exists := lockFiles[lockPath]; exists {
			workspace.LockPath = lockPath
			workspaces[path] = workspace
		}
	}

	return CargoDiscovery{
		Workspaces: sortedCargoWorkspaces(workspaces),
		Crates:     sortedCargoCrates(crates),
		LockFiles:  sortedPathSet(lockFiles),
	}, nil
}

// resolveCargoWorkspaces decides, for every manifest, which workspace resolves
// it. It answers the workspaces and the manifest-to-workspace map.
//
// Cargo resolves membership by directory: a package belongs to the nearest
// ancestor manifest that declares [workspace], unless that workspace excludes
// it. Following the same rule here means a member reached only as a path
// dependency -- never listed under `members` -- lands in its workspace instead
// of becoming a second unit that would load the same workspace again.
func resolveCargoWorkspaces(
	root string,
	manifests map[string]cargoManifest,
) (map[string]CargoWorkspace, map[string]string, error) {
	roots := make([]cargoManifest, 0, len(manifests))
	for _, manifest := range manifests {
		if manifest.workspace != nil {
			roots = append(roots, manifest)
		}
	}
	sort.Slice(roots, func(left, right int) bool {
		return roots[left].path < roots[right].path
	})

	workspaces := make(map[string]CargoWorkspace, len(roots))
	for _, manifest := range roots {
		workspaces[manifest.path] = CargoWorkspace{
			ManifestPath: manifest.path,
			RootPath:     manifest.directory,
			Virtual:      manifest.pkg == nil,
		}
	}

	membership := make(map[string]string, len(manifests))
	paths := make([]string, 0, len(manifests))
	for path := range manifests {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	for _, path := range paths {
		manifest := manifests[path]
		if manifest.pkg == nil {
			continue
		}
		owner, err := ownerWorkspace(root, manifest, manifests, workspaces)
		if err != nil {
			return nil, nil, err
		}
		if owner == "" {
			if manifest.workspace == nil {
				// A package with no [workspace] of its own and no ancestor
				// workspace is a workspace of one, exactly as Cargo treats it.
				workspaces[manifest.path] = CargoWorkspace{
					ManifestPath: manifest.path,
					RootPath:     manifest.directory,
				}
			}
			owner = manifest.path
		}
		membership[path] = owner
		workspace := workspaces[owner]
		workspace.Members = append(workspace.Members, path)
		workspaces[owner] = workspace
	}
	return workspaces, membership, nil
}

// ownerWorkspace answers the workspace manifest that claims this package, or
// an empty string when the package stands alone.
func ownerWorkspace(
	root string,
	manifest cargoManifest,
	manifests map[string]cargoManifest,
	workspaces map[string]CargoWorkspace,
) (string, error) {
	if declared := strings.TrimSpace(manifest.pkg.Workspace); declared != "" {
		target := declared
		if !filepath.IsAbs(target) {
			target = filepath.Join(manifest.directory, filepath.FromSlash(target))
		}
		resolved, err := validateContainedPath(root, filepath.Clean(target))
		if err != nil {
			return "", fmt.Errorf("crate %q declares workspace %q: %w", manifest.path, declared, err)
		}
		ownerPath := filepath.Join(resolved, "Cargo.toml")
		if _, exists := workspaces[ownerPath]; !exists {
			return "", fmt.Errorf("crate %q declares workspace %q, which declares no [workspace]", manifest.path, declared)
		}
		if excluded, err := workspaceExcludes(manifests[ownerPath], manifest.directory); err != nil {
			return "", err
		} else if excluded {
			return "", fmt.Errorf("crate %q declares workspace %q, which excludes it", manifest.path, declared)
		}
		return ownerPath, nil
	}

	if manifest.workspace != nil {
		// A manifest that declares its own [workspace] is a root, even when a
		// workspace exists above it. Cargo rejects nesting, and claiming it
		// from above would publish a membership no build agrees with.
		if ancestor := nearestWorkspace(manifest, workspaces); ancestor != "" {
			return "", fmt.Errorf("manifest %q declares [workspace] inside the workspace %q", manifest.path, ancestor)
		}
		return manifest.path, nil
	}

	ancestor := nearestWorkspace(manifest, workspaces)
	if ancestor == "" {
		return "", nil
	}
	excluded, err := workspaceExcludes(manifests[ancestor], manifest.directory)
	if err != nil {
		return "", err
	}
	if excluded {
		return "", nil
	}
	return ancestor, nil
}

// nearestWorkspace answers the closest ancestor workspace manifest, which is
// the one Cargo would resolve.
func nearestWorkspace(manifest cargoManifest, workspaces map[string]CargoWorkspace) string {
	nearest := ""
	for candidate, workspace := range workspaces {
		if candidate == manifest.path {
			continue
		}
		if !pathWithin(workspace.RootPath, manifest.directory) {
			continue
		}
		if manifest.directory == workspace.RootPath {
			continue
		}
		if nearest == "" || len(workspace.RootPath) > len(workspaces[nearest].RootPath) {
			nearest = candidate
		}
	}
	return nearest
}

// workspaceExcludes reports whether a workspace excludes a directory. The
// patterns are repository relative paths, matched the way Cargo matches them:
// a listed directory excludes itself and everything below it.
func workspaceExcludes(workspace cargoManifest, directory string) (bool, error) {
	if workspace.workspace == nil {
		return false, nil
	}
	for _, pattern := range workspace.workspace.Exclude {
		cleaned := strings.TrimSpace(pattern)
		if cleaned == "" {
			continue
		}
		excluded := filepath.Join(workspace.directory, filepath.FromSlash(cleaned))
		if directory == excluded || pathWithin(excluded, directory) {
			return true, nil
		}
	}
	return false, nil
}

func walkCargoFiles(ctx context.Context, base, current string, exclusions []string, visit func(string) error) error {
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
		if isCargoDiscoveryExcluded(base, entryPath, entry.Name(), isDirectory, exclusions) {
			continue
		}
		if isDirectory {
			if err := walkCargoFiles(ctx, base, entryPath, exclusions, visit); err != nil {
				return err
			}
			continue
		}
		if entry.Name() != "Cargo.toml" && entry.Name() != "Cargo.lock" {
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

func isCargoDiscoveryExcluded(base, candidate, name string, isDirectory bool, exclusions []string) bool {
	if isDirectory {
		if _, excluded := defaultCargoExcludedDirectories[name]; excluded {
			return true
		}
	}
	return isDiscoveryExcluded(base, candidate, name, isDirectory, exclusions)
}

package workspace

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

const maxGoManifestBytes = 16 << 20

// GoDiscovery contains Go modules, workspaces, sums and packages found in a
// registered repository.
type GoDiscovery struct {
	Modules    []GoModule
	Workspaces []GoWorkspace
	SumFiles   []string
	Packages   []GoPackage
}

// GoModule describes one go.mod and its replace directives.
type GoModule struct {
	ManifestPath string
	SumPath      string
	RootPath     string
	ModulePath   string
	GoVersion    string
	Replaces     []GoReplacement
}

// GoWorkspace describes one go.work and its resolved module manifests.
type GoWorkspace struct {
	Path      string
	GoVersion string
	Modules   []string
	Replaces  []GoReplacement
}

// GoReplacement preserves a replace directive. NewLocalPath is set only when
// NewPath is a local filesystem path.
type GoReplacement struct {
	OldPath      string
	OldVersion   string
	NewPath      string
	NewVersion   string
	NewLocalPath string
}

// GoPackage describes one package directory and its source files.
type GoPackage struct {
	Directory  string
	ImportPath string
	ModulePath string
	Name       string
	Files      []string
}

type parsedGoModule struct {
	module GoModule
	file   *modfile.File
}

type parsedGoWorkspace struct {
	workspace GoWorkspace
	file      *modfile.WorkFile
}

func readGoManifest(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxGoManifestBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxGoManifestBytes {
		return nil, fmt.Errorf("manifest exceeds %d bytes", maxGoManifestBytes)
	}
	return data, nil
}

func parseGoModule(manifestPath, repositoryRoot string) (parsedGoModule, error) {
	data, err := readGoManifest(manifestPath)
	if err != nil {
		return parsedGoModule{}, err
	}
	file, err := modfile.Parse(manifestPath, data, nil)
	if err != nil {
		return parsedGoModule{}, fmt.Errorf("parse go.mod: %w", err)
	}
	if file.Module == nil || strings.TrimSpace(file.Module.Mod.Path) == "" {
		return parsedGoModule{}, fmt.Errorf("go.mod has no module directive")
	}
	root := filepath.Dir(manifestPath)
	module := GoModule{
		ManifestPath: filepath.Clean(manifestPath),
		RootPath:     root,
		ModulePath:   file.Module.Mod.Path,
	}
	if file.Go != nil {
		module.GoVersion = file.Go.Version
	}
	sumPath := filepath.Join(root, "go.sum")
	if info, err := os.Stat(sumPath); err == nil && info.Mode().IsRegular() {
		module.SumPath = filepath.Clean(sumPath)
	} else if err != nil && !os.IsNotExist(err) {
		return parsedGoModule{}, fmt.Errorf("inspect go.sum %q: %w", sumPath, err)
	}
	module.Replaces = make([]GoReplacement, 0, len(file.Replace))
	for index, replacement := range file.Replace {
		converted, err := convertGoReplacement(repositoryRoot, root, replacement)
		if err != nil {
			return parsedGoModule{}, fmt.Errorf("replace[%d]: %w", index, err)
		}
		module.Replaces = append(module.Replaces, converted)
	}
	return parsedGoModule{module: module, file: file}, nil
}

func parseGoWorkspace(workspacePath, repositoryRoot string) (parsedGoWorkspace, error) {
	data, err := readGoManifest(workspacePath)
	if err != nil {
		return parsedGoWorkspace{}, err
	}
	file, err := modfile.ParseWork(workspacePath, data, nil)
	if err != nil {
		return parsedGoWorkspace{}, fmt.Errorf("parse go.work: %w", err)
	}
	workspace := GoWorkspace{Path: filepath.Clean(workspacePath)}
	if file.Go != nil {
		workspace.GoVersion = file.Go.Version
	}
	workspace.Modules = make([]string, len(file.Use))
	for index, use := range file.Use {
		modulePath, err := resolveGoModuleManifest(filepath.Dir(workspacePath), repositoryRoot, use.Path)
		if err != nil {
			return parsedGoWorkspace{}, fmt.Errorf("use[%d] %q: %w", index, use.Path, err)
		}
		workspace.Modules[index] = modulePath
	}
	workspace.Replaces = make([]GoReplacement, 0, len(file.Replace))
	for index, replacement := range file.Replace {
		converted, err := convertGoReplacement(repositoryRoot, filepath.Dir(workspacePath), replacement)
		if err != nil {
			return parsedGoWorkspace{}, fmt.Errorf("replace[%d]: %w", index, err)
		}
		workspace.Replaces = append(workspace.Replaces, converted)
	}
	return parsedGoWorkspace{workspace: workspace, file: file}, nil
}

func resolveGoModuleManifest(baseDirectory, repositoryRoot, rawPath string) (string, error) {
	rawPath = strings.TrimSpace(rawPath)
	if rawPath == "" {
		return "", fmt.Errorf("path must not be empty")
	}
	target := filepath.FromSlash(rawPath)
	if !filepath.IsAbs(target) {
		target = filepath.Join(baseDirectory, target)
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("make path absolute: %w", err)
	}
	target = filepath.Clean(absolute)
	if !pathWithin(repositoryRoot, target) {
		return "", fmt.Errorf("path escapes repository realpath %q", repositoryRoot)
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("path does not exist or is inaccessible: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("module use path is not a directory")
	}
	target = filepath.Join(target, "go.mod")
	info, err = os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("module directory has no go.mod: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("module manifest is not a regular file")
	}
	return validateGoResolvedPath(repositoryRoot, target)
}

func convertGoReplacement(repositoryRoot, baseDirectory string, replacement *modfile.Replace) (GoReplacement, error) {
	if replacement == nil {
		return GoReplacement{}, fmt.Errorf("replacement must not be nil")
	}
	converted := GoReplacement{
		OldPath:    replacement.Old.Path,
		OldVersion: replacement.Old.Version,
		NewPath:    replacement.New.Path,
		NewVersion: replacement.New.Version,
	}
	if isLocalGoPath(replacement.New.Path) {
		resolved, err := resolveGoLocalPath(baseDirectory, repositoryRoot, replacement.New.Path)
		if err != nil {
			return GoReplacement{}, err
		}
		converted.NewLocalPath = resolved
	}
	return converted, nil
}

func isLocalGoPath(value string) bool {
	value = strings.TrimSpace(value)
	return filepath.IsAbs(value) || value == "." || value == ".." || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") || strings.HasPrefix(value, `.\`) || strings.HasPrefix(value, `..\`)
}

func resolveGoLocalPath(baseDirectory, repositoryRoot, rawPath string) (string, error) {
	target := filepath.FromSlash(strings.TrimSpace(rawPath))
	if !filepath.IsAbs(target) {
		target = filepath.Join(baseDirectory, target)
	}
	absolute, err := filepath.Abs(target)
	if err != nil {
		return "", fmt.Errorf("make local replacement absolute: %w", err)
	}
	target = filepath.Clean(absolute)
	if !pathWithin(repositoryRoot, target) {
		return "", fmt.Errorf("local replacement %q escapes repository realpath %q", rawPath, repositoryRoot)
	}
	info, err := os.Stat(target)
	if err != nil {
		return "", fmt.Errorf("local replacement %q does not exist or is inaccessible: %w", rawPath, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("local replacement %q is not a directory", rawPath)
	}
	return validateGoResolvedPath(repositoryRoot, target)
}

func validateGoResolvedPath(repositoryRoot, target string) (string, error) {
	if symlink, err := firstSymlink(target); err != nil {
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

package workspace

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var defaultGoExcludedDirectories = map[string]struct{}{
	".git":         {},
	"vendor":       {},
	"node_modules": {},
	".pnpm":        {},
	".yarn":        {},
}

// DiscoverGo finds go.mod, go.sum, go.work, Go packages and replace
// directives below a repository root. Results are deterministic and use
// absolute, canonical paths.
func DiscoverGo(ctx context.Context, repository Repository) (GoDiscovery, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return GoDiscovery{}, err
	}
	rootInput := repository.RealPath
	if strings.TrimSpace(rootInput) == "" {
		rootInput = repository.Path
	}
	_, root, err := inspectRepositoryPath(rootInput)
	if err != nil {
		return GoDiscovery{}, fmt.Errorf("discover Go root: %w", err)
	}

	modules := make(map[string]GoModule)
	workspaces := make(map[string]GoWorkspace)
	sumFiles := make(map[string]struct{})
	goFiles := make(map[string][]string)
	err = walkGoFiles(ctx, root, root, repository.Exclusions, func(filePath string) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		switch filepath.Base(filePath) {
		case "go.mod":
			parsed, err := parseGoModule(filePath, root)
			if err != nil {
				return fmt.Errorf("parse module manifest %q: %w", filePath, err)
			}
			modules[parsed.module.ManifestPath] = parsed.module
			if parsed.module.SumPath != "" {
				sumFiles[parsed.module.SumPath] = struct{}{}
			}
		case "go.sum":
			sumFiles[filePath] = struct{}{}
		case "go.work":
			parsed, err := parseGoWorkspace(filePath, root)
			if err != nil {
				return fmt.Errorf("parse workspace manifest %q: %w", filePath, err)
			}
			workspaces[parsed.workspace.Path] = parsed.workspace
		default:
			if strings.HasSuffix(filepath.Base(filePath), ".go") {
				directory := filepath.Dir(filePath)
				goFiles[directory] = append(goFiles[directory], filePath)
			}
		}
		return nil
	})
	if err != nil {
		return GoDiscovery{}, err
	}
	if err := addWorkspaceModules(root, workspaces, modules, sumFiles); err != nil {
		return GoDiscovery{}, err
	}
	packages, err := discoverGoPackages(ctx, root, repository.Exclusions, goFiles, modules)
	if err != nil {
		return GoDiscovery{}, err
	}

	result := GoDiscovery{
		Modules:    sortedGoModules(modules),
		Workspaces: sortedGoWorkspaces(workspaces),
		SumFiles:   sortedPathSet(sumFiles),
		Packages:   packages,
	}
	return result, nil
}

func walkGoFiles(ctx context.Context, base, current string, exclusions []string, visit func(string) error) error {
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
		excluded, err := isGoDiscoveryExcluded(base, entryPath, entry.Name(), isDirectory, exclusions)
		if err != nil {
			return fmt.Errorf("check Go exclusion for %q: %w", entryPath, err)
		}
		if excluded {
			continue
		}
		if isDirectory {
			if err := walkGoFiles(ctx, base, entryPath, exclusions, visit); err != nil {
				return err
			}
			continue
		}
		name := entry.Name()
		if name != "go.mod" && name != "go.sum" && name != "go.work" && !strings.HasSuffix(name, ".go") {
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

func isGoDiscoveryExcluded(base, candidate, name string, isDirectory bool, exclusions []string) (bool, error) {
	excluded, err := MatchesExclusion(base, candidate, exclusions)
	if err != nil {
		return false, err
	}
	if isDirectory {
		if _, excluded := defaultGoExcludedDirectories[name]; excluded {
			return true, nil
		}
	}
	return excluded, nil
}

func addWorkspaceModules(repositoryRoot string, workspaces map[string]GoWorkspace, modules map[string]GoModule, sumFiles map[string]struct{}) error {
	workspacePaths := make([]string, 0, len(workspaces))
	for workspacePath := range workspaces {
		workspacePaths = append(workspacePaths, workspacePath)
	}
	sort.Strings(workspacePaths)
	for _, workspacePath := range workspacePaths {
		workspace := workspaces[workspacePath]
		for _, manifestPath := range workspace.Modules {
			if _, exists := modules[manifestPath]; exists {
				continue
			}
			parsed, err := parseGoModule(manifestPath, repositoryRoot)
			if err != nil {
				return fmt.Errorf("parse workspace module %q: %w", manifestPath, err)
			}
			modules[parsed.module.ManifestPath] = parsed.module
			if parsed.module.SumPath != "" {
				sumFiles[parsed.module.SumPath] = struct{}{}
			}
		}
	}
	return nil
}

func discoverGoPackages(ctx context.Context, repositoryRoot string, exclusions []string, filesByDirectory map[string][]string, modules map[string]GoModule) ([]GoPackage, error) {
	moduleRoots := make([]GoModule, 0, len(modules))
	for _, module := range modules {
		moduleRoots = append(moduleRoots, module)
	}
	sort.Slice(moduleRoots, func(left, right int) bool {
		if len(moduleRoots[left].RootPath) != len(moduleRoots[right].RootPath) {
			return len(moduleRoots[left].RootPath) > len(moduleRoots[right].RootPath)
		}
		return moduleRoots[left].RootPath < moduleRoots[right].RootPath
	})

	directories := make([]string, 0, len(filesByDirectory))
	for directory := range filesByDirectory {
		directories = append(directories, directory)
	}
	sort.Strings(directories)
	packages := make([]GoPackage, 0, len(directories))
	for _, directory := range directories {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		excluded, err := isGoDiscoveryExcludedPath(repositoryRoot, directory, exclusions)
		if err != nil {
			return nil, fmt.Errorf("check Go package exclusion for %q: %w", directory, err)
		}
		if excluded {
			continue
		}
		files := append([]string(nil), filesByDirectory[directory]...)
		sort.Strings(files)
		name, packageFiles, err := parseGoPackage(directory, files)
		if err != nil {
			return nil, err
		}
		module := moduleForDirectory(directory, moduleRoots)
		goPackage := GoPackage{
			Directory: directory,
			Name:      name,
			Files:     packageFiles,
		}
		if module.ModulePath != "" {
			goPackage.ModulePath = module.ModulePath
			relative, err := filepath.Rel(module.RootPath, directory)
			if err != nil {
				return nil, fmt.Errorf("resolve package path %q: %w", directory, err)
			}
			if relative == "." {
				goPackage.ImportPath = module.ModulePath
			} else {
				goPackage.ImportPath = strings.TrimSuffix(module.ModulePath, "/") + "/" + filepath.ToSlash(relative)
			}
		}
		packages = append(packages, goPackage)
	}
	return packages, nil
}

func isGoDiscoveryExcludedPath(base, directory string, exclusions []string) (bool, error) {
	relative, err := filepath.Rel(base, directory)
	if err != nil {
		return false, nil
	}
	if relative == "." {
		return false, nil
	}
	return isGoDiscoveryExcluded(base, directory, filepath.Base(directory), true, exclusions)
}

func parseGoPackage(directory string, files []string) (string, []string, error) {
	filesByName := make(map[string][]string)
	for _, filePath := range files {
		file, err := parser.ParseFile(token.NewFileSet(), filePath, nil, parser.PackageClauseOnly)
		if err != nil {
			return "", nil, fmt.Errorf("parse Go package file %q: %w", filePath, err)
		}
		filesByName[file.Name.Name] = append(filesByName[file.Name.Name], filePath)
	}
	candidateNames := make([]string, 0, len(filesByName))
	for name := range filesByName {
		if strings.HasSuffix(name, "_test") {
			baseName := strings.TrimSuffix(name, "_test")
			if _, exists := filesByName[baseName]; exists {
				continue
			}
		}
		candidateNames = append(candidateNames, name)
	}
	if len(candidateNames) == 0 {
		for name := range filesByName {
			candidateNames = append(candidateNames, name)
		}
	}
	if len(candidateNames) != 1 {
		sort.Strings(candidateNames)
		return "", nil, fmt.Errorf("directory %q contains multiple Go packages: %s", directory, strings.Join(candidateNames, ", "))
	}
	name := candidateNames[0]
	packageFiles := append([]string(nil), filesByName[name]...)
	sort.Strings(packageFiles)
	return name, packageFiles, nil
}

func moduleForDirectory(directory string, modules []GoModule) GoModule {
	for _, module := range modules {
		if pathWithin(module.RootPath, directory) {
			return module
		}
	}
	return GoModule{}
}

func sortedGoModules(modules map[string]GoModule) []GoModule {
	result := make([]GoModule, 0, len(modules))
	for _, module := range modules {
		module.Replaces = append([]GoReplacement(nil), module.Replaces...)
		result = append(result, module)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].ManifestPath < result[right].ManifestPath
	})
	return result
}

func sortedGoWorkspaces(workspaces map[string]GoWorkspace) []GoWorkspace {
	result := make([]GoWorkspace, 0, len(workspaces))
	for _, workspace := range workspaces {
		workspace.Modules = append([]string(nil), workspace.Modules...)
		workspace.Replaces = append([]GoReplacement(nil), workspace.Replaces...)
		result = append(result, workspace)
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Path < result[right].Path
	})
	return result
}

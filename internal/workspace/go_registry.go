package workspace

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// GoModuleProvider is a named Go module provider discovered in one
// repository. WorkspaceReplaces preserves replacements declared by go.work
// files that include this module.
type GoModuleProvider struct {
	ModulePath        string
	Repository        string
	ManifestPath      string
	SumPath           string
	RootPath          string
	GoVersion         string
	Packages          []GoPackage
	Replaces          []GoReplacement
	WorkspaceReplaces []GoReplacement
}

// GoModuleRegistry is an immutable module-path index for one repository.
// Cross-repository ambiguity is handled by LUQUE-0408.
type GoModuleRegistry struct {
	modules []GoModuleProvider
	byPath  map[string]int
}

// NewGoModuleRegistry discovers and indexes Go modules for one repository.
func NewGoModuleRegistry(ctx context.Context, repository Repository) (*GoModuleRegistry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	discovery, err := DiscoverGo(ctx, repository)
	if err != nil {
		return nil, err
	}
	return newGoModuleRegistry(ctx, repository, discovery)
}

func newGoModuleRegistry(ctx context.Context, repository Repository, discovery GoDiscovery) (*GoModuleRegistry, error) {
	registry := &GoModuleRegistry{
		modules: make([]GoModuleProvider, 0, len(discovery.Modules)),
		byPath:  make(map[string]int, len(discovery.Modules)),
	}
	workspaceReplaces := workspaceReplacementsByModule(discovery.Workspaces)
	for _, module := range discovery.Modules {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		modulePath := strings.TrimSpace(module.ModulePath)
		if modulePath == "" {
			return nil, fmt.Errorf("module manifest %q has an empty module path", module.ManifestPath)
		}
		if previous, exists := registry.byPath[modulePath]; exists {
			return nil, fmt.Errorf("duplicate Go module path %q in %q and %q", modulePath, registry.modules[previous].ManifestPath, module.ManifestPath)
		}
		provider := GoModuleProvider{
			ModulePath:        modulePath,
			Repository:        strings.TrimSpace(repository.Name),
			ManifestPath:      module.ManifestPath,
			SumPath:           module.SumPath,
			RootPath:          module.RootPath,
			GoVersion:         module.GoVersion,
			Packages:          packagesForModule(module, discovery.Packages),
			Replaces:          cloneGoReplacements(module.Replaces),
			WorkspaceReplaces: cloneGoReplacements(workspaceReplaces[module.ManifestPath]),
		}
		registry.byPath[modulePath] = len(registry.modules)
		registry.modules = append(registry.modules, provider)
	}
	sort.Slice(registry.modules, func(left, right int) bool {
		if registry.modules[left].ModulePath != registry.modules[right].ModulePath {
			return registry.modules[left].ModulePath < registry.modules[right].ModulePath
		}
		return registry.modules[left].ManifestPath < registry.modules[right].ManifestPath
	})
	registry.byPath = make(map[string]int, len(registry.modules))
	for index, module := range registry.modules {
		registry.byPath[module.ModulePath] = index
	}
	return registry, nil
}

// List returns deep copies sorted by module path.
func (registry *GoModuleRegistry) List() []GoModuleProvider {
	if registry == nil {
		return nil
	}
	modules := make([]GoModuleProvider, len(registry.modules))
	for index, module := range registry.modules {
		modules[index] = cloneGoModuleProvider(module)
	}
	return modules
}

// Get returns a deep copy of the module registered under modulePath.
func (registry *GoModuleRegistry) Get(modulePath string) (GoModuleProvider, bool) {
	if registry == nil {
		return GoModuleProvider{}, false
	}
	index, exists := registry.byPath[strings.TrimSpace(modulePath)]
	if !exists {
		return GoModuleProvider{}, false
	}
	return cloneGoModuleProvider(registry.modules[index]), true
}

func packagesForModule(module GoModule, packages []GoPackage) []GoPackage {
	result := make([]GoPackage, 0)
	for _, packageValue := range packages {
		if packageValue.ModulePath != module.ModulePath || !pathWithin(module.RootPath, packageValue.Directory) {
			continue
		}
		result = append(result, cloneGoPackage(packageValue))
	}
	sort.Slice(result, func(left, right int) bool {
		return result[left].Directory < result[right].Directory
	})
	return result
}

func workspaceReplacementsByModule(workspaces []GoWorkspace) map[string][]GoReplacement {
	result := make(map[string][]GoReplacement)
	for _, workspace := range workspaces {
		for _, manifestPath := range workspace.Modules {
			for _, replacement := range workspace.Replaces {
				if !containsGoReplacement(result[manifestPath], replacement) {
					result[manifestPath] = append(result[manifestPath], replacement)
				}
			}
		}
	}
	return result
}

func containsGoReplacement(replacements []GoReplacement, candidate GoReplacement) bool {
	for _, replacement := range replacements {
		if replacement == candidate {
			return true
		}
	}
	return false
}

func cloneGoModuleProvider(module GoModuleProvider) GoModuleProvider {
	packages := make([]GoPackage, len(module.Packages))
	for index, packageValue := range module.Packages {
		packages[index] = cloneGoPackage(packageValue)
	}
	module.Packages = packages
	module.Replaces = cloneGoReplacements(module.Replaces)
	module.WorkspaceReplaces = cloneGoReplacements(module.WorkspaceReplaces)
	return module
}

func cloneGoPackage(packageValue GoPackage) GoPackage {
	packageValue.Files = append([]string(nil), packageValue.Files...)
	return packageValue
}

func cloneGoReplacements(replacements []GoReplacement) []GoReplacement {
	return append([]GoReplacement(nil), replacements...)
}

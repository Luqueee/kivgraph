package workspace

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
)

// TypeScriptProgram is one fully resolved tsconfig: its inheritance chain, its
// effective compiler options, the projects it references, the source files it
// owns and the compiler version that will analyse it.
type TypeScriptProgram struct {
	// ConfigPath is the absolute, canonical tsconfig path. It identifies the
	// project everywhere else in Kivgraph.
	ConfigPath string
	// Directory is the directory containing ConfigPath. Relative options and
	// patterns are resolved against it.
	Directory string
	// Extends is the resolved inheritance chain, nearest parent first. It is
	// empty for a config that extends nothing.
	Extends []string
	// CompilerOptions are the options after applying the chain, with path
	// valued options rebased to absolute paths.
	CompilerOptions map[string]any
	// References are the absolute config paths this project depends on.
	References []string
	// SourceFiles are the absolute files the project owns, sorted and free of
	// duplicates.
	SourceFiles []string
	// Composite reports whether the project may be referenced by others.
	Composite bool
	// Version is the compiler resolved for this project, per LUQUE-0605.
	Version TypeScriptVersion
}

// TypeScriptProjectGraph is the immutable reference DAG of one repository.
type TypeScriptProjectGraph struct {
	programs   map[string]TypeScriptProgram
	order      []string
	dependents map[string][]string
}

// NewTypeScriptProjectGraph resolves every discovered project and links them
// through their references.
//
// A resolver is required: a project whose compiler version is unknown would
// produce facts whose confidence nobody can audit, per ADR 0010.
func NewTypeScriptProjectGraph(
	ctx context.Context,
	repository Repository,
	discovery TypeScriptDiscovery,
	resolver *TypeScriptVersionResolver,
) (*TypeScriptProjectGraph, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if resolver == nil {
		return nil, errors.New("a TypeScript version resolver is required")
	}
	_, root, err := inspectRepositoryPath(repositoryRootPath(repository))
	if err != nil {
		return nil, fmt.Errorf("resolve repository root: %w", err)
	}

	programs := make([]TypeScriptProgram, 0, len(discovery.Projects))
	for _, project := range discovery.Projects {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		program, err := buildTypeScriptProgram(project, root, resolver)
		if err != nil {
			return nil, fmt.Errorf("resolve project %s: %w", project.ConfigPath, err)
		}
		programs = append(programs, program)
	}
	return newTypeScriptProjectGraph(programs)
}

func repositoryRootPath(repository Repository) string {
	if repository.RealPath != "" {
		return repository.RealPath
	}
	return repository.Path
}

// buildTypeScriptProgram resolves one project end to end.
func buildTypeScriptProgram(
	project TypeScriptProject,
	repositoryRoot string,
	resolver *TypeScriptVersionResolver,
) (TypeScriptProgram, error) {
	parsed, err := resolveTypeScriptConfig(project.ConfigPath, repositoryRoot)
	if err != nil {
		return TypeScriptProgram{}, err
	}
	sources, err := resolveTypeScriptSources(parsed, repositoryRoot)
	if err != nil {
		return TypeScriptProgram{}, err
	}
	version, err := resolver.Resolve(project.ConfigPath)
	if err != nil {
		return TypeScriptProgram{}, err
	}
	return TypeScriptProgram{
		ConfigPath:      parsed.ConfigPath,
		Directory:       parsed.Directory,
		Extends:         parsed.ExtendsChain,
		CompilerOptions: parsed.CompilerOptions,
		References:      append([]string(nil), project.References...),
		SourceFiles:     sources,
		Composite:       booleanCompilerOption(parsed.CompilerOptions, "composite"),
		Version:         version,
	}, nil
}

// booleanCompilerOption reads a boolean option, defaulting to false when it is
// absent or of another type.
func booleanCompilerOption(options map[string]any, name string) bool {
	value, ok := options[name].(bool)
	return ok && value
}

// newTypeScriptProjectGraph indexes the programs and orders them.
func newTypeScriptProjectGraph(programs []TypeScriptProgram) (*TypeScriptProjectGraph, error) {
	indexed := make(map[string]TypeScriptProgram, len(programs))
	for _, program := range programs {
		key := filepath.Clean(program.ConfigPath)
		if _, duplicated := indexed[key]; duplicated {
			return nil, fmt.Errorf("project %s is discovered twice", key)
		}
		indexed[key] = program
	}

	order, dependents, err := topologicalTypeScriptOrder(indexed)
	if err != nil {
		return nil, err
	}
	return &TypeScriptProjectGraph{programs: indexed, order: order, dependents: dependents}, nil
}

// Len returns the number of projects.
func (graph *TypeScriptProjectGraph) Len() int {
	if graph == nil {
		return 0
	}
	return len(graph.programs)
}

// Get returns one project by its config path.
func (graph *TypeScriptProjectGraph) Get(configPath string) (TypeScriptProgram, bool) {
	if graph == nil {
		return TypeScriptProgram{}, false
	}
	program, ok := graph.programs[filepath.Clean(configPath)]
	if !ok {
		return TypeScriptProgram{}, false
	}
	return cloneTypeScriptProgram(program), true
}

// Order returns the config paths in dependency order: a referenced project
// always precedes the projects that reference it.
func (graph *TypeScriptProjectGraph) Order() []string {
	if graph == nil {
		return nil
	}
	return append([]string(nil), graph.order...)
}

// Programs returns every project in dependency order.
func (graph *TypeScriptProjectGraph) Programs() []TypeScriptProgram {
	if graph == nil {
		return nil
	}
	programs := make([]TypeScriptProgram, 0, len(graph.order))
	for _, configPath := range graph.order {
		programs = append(programs, cloneTypeScriptProgram(graph.programs[configPath]))
	}
	return programs
}

// Dependents returns the projects that reference configPath, sorted.
func (graph *TypeScriptProjectGraph) Dependents(configPath string) []string {
	if graph == nil {
		return nil
	}
	return append([]string(nil), graph.dependents[filepath.Clean(configPath)]...)
}

// Unsupported returns the projects whose facts cannot be exact, sorted by
// config path. It is the audit list ADR 0010 requires.
func (graph *TypeScriptProjectGraph) Unsupported() []TypeScriptProgram {
	if graph == nil {
		return nil
	}
	unsupported := make([]TypeScriptProgram, 0)
	for _, configPath := range graph.order {
		if program := graph.programs[configPath]; !program.Version.WithinSupportedWindow {
			unsupported = append(unsupported, cloneTypeScriptProgram(program))
		}
	}
	sort.Slice(unsupported, func(left, right int) bool {
		return unsupported[left].ConfigPath < unsupported[right].ConfigPath
	})
	return unsupported
}

func cloneTypeScriptProgram(program TypeScriptProgram) TypeScriptProgram {
	clone := program
	clone.Extends = append([]string(nil), program.Extends...)
	clone.References = append([]string(nil), program.References...)
	clone.SourceFiles = append([]string(nil), program.SourceFiles...)
	clone.CompilerOptions = cloneCompilerOptions(program.CompilerOptions)
	return clone
}

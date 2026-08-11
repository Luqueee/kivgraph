// Package rustloader turns the Rust code of a registered repository into the
// facts Ladygraph can store.
//
// Rust has no type checker Ladygraph can link into its own process, so the
// authority for identity and resolution is `rust-analyzer`, invoked as a batch
// indexer over one Cargo workspace at a time. This package owns that
// invocation, the index it produces, and the registry that decides which
// registered repository provides a crate.
package rustloader

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Luqueee/ladygraph/internal/workspace"
)

// unknownCrateVersion is the version rust-analyzer writes into a symbol when
// it does not know one. It identifies nothing, so it never resolves.
const unknownCrateVersion = "."

// CrateStatus reports whether a crate reference could be attributed to a
// registered provider.
type CrateStatus string

const (
	// CrateResolved means one registered repository provides the crate at
	// the requested version.
	CrateResolved CrateStatus = "RESOLVED"
	// CrateProviderNotFound means no registered repository declares it.
	CrateProviderNotFound CrateStatus = "CRATE_PROVIDER_NOT_FOUND"
	// AmbiguousCrateProvider means several providers declare the crate at
	// the requested version. None of them provides it.
	AmbiguousCrateProvider CrateStatus = "AMBIGUOUS_CRATE_PROVIDER"
	// CrateVersionUnknown means the reference carries no usable version, so
	// no provider can be proven to be the code it was compiled against.
	CrateVersionUnknown CrateStatus = "CRATE_VERSION_UNKNOWN"
	// CrateVersionMismatch means the crate is declared by a registered
	// repository, at another version.
	CrateVersionMismatch CrateStatus = "CRATE_VERSION_MISMATCH"
)

// CrateProvider is one crate offered by a registered repository.
type CrateProvider struct {
	Repository    string
	Name          string
	Version       string
	RootPath      string
	ManifestPath  string
	WorkspacePath string
}

// CrateRegistry indexes crate names across every registered repository.
//
// Ambiguity is preserved, never resolved by preference: two repositories
// declaring the same crate is a fact the caller must see, exactly as it is for
// a Go module path or a TypeScript package name.
type CrateRegistry struct {
	providers map[string][]CrateProvider
}

// NewCrateRegistry builds the cross-repository crate index.
func NewCrateRegistry(ctx context.Context, repositories []workspace.Repository) (*CrateRegistry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	registry := &CrateRegistry{providers: make(map[string][]CrateProvider)}
	for _, repository := range repositories {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(repository.Name)
		if name == "" {
			return nil, fmt.Errorf("repository %q: name must not be empty", repository.Path)
		}
		discovery, err := workspace.DiscoverCargo(ctx, repository)
		if err != nil {
			return nil, fmt.Errorf("repository %q Cargo crates: %w", name, err)
		}
		for _, crate := range discovery.Crates {
			crateName := strings.TrimSpace(crate.Name)
			if crateName == "" {
				return nil, fmt.Errorf("repository %q crate manifest %q has an empty name", name, crate.ManifestPath)
			}
			registry.providers[crateName] = append(registry.providers[crateName], CrateProvider{
				Repository:    name,
				Name:          crateName,
				Version:       strings.TrimSpace(crate.Version),
				RootPath:      crate.RootPath,
				ManifestPath:  crate.ManifestPath,
				WorkspacePath: crate.WorkspacePath,
			})
		}
	}
	for crateName := range registry.providers {
		sort.Slice(registry.providers[crateName], func(left, right int) bool {
			candidates := registry.providers[crateName]
			if candidates[left].Repository != candidates[right].Repository {
				return candidates[left].Repository < candidates[right].Repository
			}
			return candidates[left].ManifestPath < candidates[right].ManifestPath
		})
	}
	return registry, nil
}

// CrateNames answers every indexed crate name, sorted.
func (registry *CrateRegistry) CrateNames() []string {
	if registry == nil {
		return nil
	}
	names := make([]string, 0, len(registry.providers))
	for name := range registry.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Providers answers the registered providers of one crate name, in a
// deterministic order.
func (registry *CrateRegistry) Providers(name string) []CrateProvider {
	if registry == nil {
		return nil
	}
	stored := registry.providers[strings.TrimSpace(name)]
	return append([]CrateProvider(nil), stored...)
}

// Resolve attributes a crate reference to the repository that provides it.
//
// The version is part of the question, not a detail: two repositories may
// declare the same crate, and a consumer compiled against a version nobody
// registered was not compiled against the code in the graph.
func (registry *CrateRegistry) Resolve(name, version string) (CrateProvider, CrateStatus) {
	if registry == nil {
		return CrateProvider{}, CrateProviderNotFound
	}
	candidates := registry.providers[strings.TrimSpace(name)]
	if len(candidates) == 0 {
		return CrateProvider{}, CrateProviderNotFound
	}
	requested := strings.TrimSpace(version)
	if requested == "" || requested == unknownCrateVersion {
		return CrateProvider{}, CrateVersionUnknown
	}
	matching := make([]CrateProvider, 0, 1)
	for _, candidate := range candidates {
		if candidate.Version == requested {
			matching = append(matching, candidate)
		}
	}
	switch len(matching) {
	case 0:
		return CrateProvider{}, CrateVersionMismatch
	case 1:
		return matching[0], CrateResolved
	default:
		return CrateProvider{}, AmbiguousCrateProvider
	}
}

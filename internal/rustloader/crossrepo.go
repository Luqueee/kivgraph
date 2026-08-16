// Package rustloader turns the Rust code of a registered repository into the
// facts Kivgraph can store.
//
// Rust has no type checker Kivgraph can link into its own process, so the
// authority for identity and resolution is `rust-analyzer`, invoked as a batch
// indexer over one Cargo workspace at a time. This package owns that
// invocation, the index it produces, and the registry that decides which
// registered repository provides a crate.
package rustloader

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/workspace"
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

// CrateProvider is one crate offered by a repository.
type CrateProvider struct {
	Repository    string
	Name          string
	Version       string
	RootPath      string
	ManifestPath  string
	WorkspacePath string
	// Lang marks a crate of the standard library, provided by the synthetic
	// sysroot repository. Its version field is not a release: a consumer
	// spells it as an origin URL and the sysroot's own index spells it
	// `0.0.0`, so the two sides never agree on it. They agree on the crate
	// name and the descriptor path, which is all the stable key is built
	// from, so a lang crate resolves without comparing versions.
	Lang bool
}

// CrateRegistry indexes crate names across every registered repository.
//
// Ambiguity is preserved, never resolved by preference: two repositories
// declaring the same crate is a fact the caller must see, exactly as it is for
// a Go module path or a TypeScript package name.
type CrateRegistry struct {
	providers map[string][]CrateProvider
}

// NewCrateRegistry builds the cross-repository crate index. A non-nil sysroot
// adds the standard library as a provider of its own crates.
func NewCrateRegistry(
	ctx context.Context,
	repositories []workspace.Repository,
	sysroot *SysrootProvider,
) (*CrateRegistry, error) {
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
	if sysroot != nil {
		if err := registry.addSysroot(ctx, *sysroot); err != nil {
			return nil, err
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

// addSysroot registers the crates of the standard library as lang providers.
//
// Discovery is the same one every repository gets: the sysroot is a Cargo
// workspace and its crates are read from its manifests, never from a hardcoded
// list of names. A toolchain that ships one more library crate provides it
// without this package learning its name.
//
// Only the crates of the library workspace are registered. A toolchain vendors
// several independent workspaces inside its library directory, and registering
// their crates would promise a provider the pass does not index: every use of
// one would compose a key nobody publishes.
func (registry *CrateRegistry) addSysroot(ctx context.Context, sysroot SysrootProvider) error {
	discovery, err := workspace.DiscoverCargo(ctx, sysroot.Repository)
	if err != nil {
		return fmt.Errorf("sysroot %q Cargo crates: %w", sysroot.Repository.Name, err)
	}
	library := filepath.Join(sysroot.LibraryPath, "Cargo.toml")
	for _, crate := range discovery.Crates {
		if crate.WorkspacePath != library {
			continue
		}
		crateName := strings.TrimSpace(crate.Name)
		if crateName == "" {
			return fmt.Errorf("sysroot %q crate manifest %q has an empty name", sysroot.Repository.Name, crate.ManifestPath)
		}
		registry.providers[crateName] = append(registry.providers[crateName], CrateProvider{
			Repository:    sysroot.Repository.Name,
			Name:          crateName,
			Version:       strings.TrimSpace(crate.Version),
			RootPath:      crate.RootPath,
			ManifestPath:  crate.ManifestPath,
			WorkspacePath: crate.WorkspacePath,
			Lang:          true,
		})
	}
	return nil
}

// IsLangOrigin reports whether a moniker's version field names the standard
// library rather than a release.
//
// This is the evidence that attributes a reference to the sysroot. Resolving
// `core` because a provider happens to be called `core` would be attribution by
// name, which never produces an exact edge in this project.
func IsLangOrigin(version string) bool {
	return strings.HasPrefix(strings.TrimSpace(version), LangCrateOrigin)
}

// Resolve attributes a crate reference to the repository that provides it.
//
// The version is part of the question, not a detail: two repositories may
// declare the same crate, and a consumer compiled against a version nobody
// registered was not compiled against the code in the graph.
//
// A crate of the standard library is the one case where the version cannot be
// compared, because the two sides of the boundary do not write the same thing
// in that field. What is compared instead is the origin the analyzer named,
// which is evidence of the same kind: a reference that does not carry it is not
// a reference to the standard library.
func (registry *CrateRegistry) Resolve(name, version string) (CrateProvider, CrateStatus) {
	if registry == nil {
		return CrateProvider{}, CrateProviderNotFound
	}
	candidates := registry.providers[strings.TrimSpace(name)]
	if len(candidates) == 0 {
		return CrateProvider{}, CrateProviderNotFound
	}
	requested := strings.TrimSpace(version)
	if IsLangOrigin(requested) {
		return resolveLangCrate(candidates)
	}
	if requested == "" || requested == unknownCrateVersion {
		return CrateProvider{}, CrateVersionUnknown
	}
	matching := make([]CrateProvider, 0, 1)
	for _, candidate := range candidates {
		if candidate.Lang {
			// The reference names a release; the standard library is not
			// one. Attributing it here would put a symbol of `core` behind
			// a version nobody compiled against.
			continue
		}
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

// resolveLangCrate attributes a reference the analyzer marked as coming from
// the standard library.
//
// A registered repository declaring a crate the standard library also declares
// is an ambiguity, not a preference: exactly as it is for two repositories, and
// so are two toolchains in one graph.
func resolveLangCrate(candidates []CrateProvider) (CrateProvider, CrateStatus) {
	lang := make([]CrateProvider, 0, 1)
	registered := 0
	for _, candidate := range candidates {
		if candidate.Lang {
			lang = append(lang, candidate)
			continue
		}
		registered++
	}
	switch {
	case len(lang) == 0:
		return CrateProvider{}, CrateProviderNotFound
	case len(lang) > 1 || registered > 0:
		return CrateProvider{}, AmbiguousCrateProvider
	default:
		return lang[0], CrateResolved
	}
}

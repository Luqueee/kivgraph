package goloader

import (
	"context"
	"fmt"
	"go/types"
	"sort"
	"strings"

	"golang.org/x/tools/go/types/objectpath"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

// CrossRepositoryStatus reports whether a use could be attributed to a
// registered module provider.
type CrossRepositoryStatus string

const (
	// CrossRepositoryResolved means provider and target identity are known.
	CrossRepositoryResolved CrossRepositoryStatus = "RESOLVED"
	// ModuleProviderNotFound means no registered repository declares the
	// module that owns the target.
	ModuleProviderNotFound CrossRepositoryStatus = "MODULE_PROVIDER_NOT_FOUND"
	// AmbiguousModuleProvider means several repositories declare it.
	AmbiguousModuleProvider CrossRepositoryStatus = "AMBIGUOUS_MODULE_PROVIDER"
	// ObjectPathUnavailable means the exported target has no object path, so
	// it cannot be addressed from another repository.
	ObjectPathUnavailable CrossRepositoryStatus = "OBJECT_PATH_UNAVAILABLE"
	// ReplaceConflictTarget means the workspace had to guess the replacement
	// of the target module, so no edge may depend on it.
	ReplaceConflictTarget CrossRepositoryStatus = "REPLACE_CONFLICT"
)

// ModuleProvider is one module offered by a registered repository.
type ModuleProvider struct {
	Repository   string
	ModulePath   string
	RootPath     string
	ManifestPath string
}

// ModuleRegistry indexes module paths across every registered repository.
//
// Ambiguity is preserved, never resolved by preference: two repositories
// declaring the same module path is a fact the caller must see.
type ModuleRegistry struct {
	providers map[string][]ModuleProvider
}

// NewModuleRegistry builds the cross-repository module index.
func NewModuleRegistry(ctx context.Context, repositories []workspace.Repository) (*ModuleRegistry, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	registry := &ModuleRegistry{providers: make(map[string][]ModuleProvider)}
	for _, repository := range repositories {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := strings.TrimSpace(repository.Name)
		if name == "" {
			return nil, fmt.Errorf("repository %q: name must not be empty", repository.Path)
		}
		modules, err := workspace.NewGoModuleRegistry(ctx, repository)
		if err != nil {
			return nil, fmt.Errorf("repository %q Go modules: %w", name, err)
		}
		for _, module := range modules.List() {
			registry.providers[module.ModulePath] = append(registry.providers[module.ModulePath], ModuleProvider{
				Repository:   name,
				ModulePath:   module.ModulePath,
				RootPath:     module.RootPath,
				ManifestPath: module.ManifestPath,
			})
		}
	}
	for modulePath := range registry.providers {
		sort.Slice(registry.providers[modulePath], func(left, right int) bool {
			return registry.providers[modulePath][left].Repository <
				registry.providers[modulePath][right].Repository
		})
	}
	return registry, nil
}

// Providers returns every repository declaring modulePath.
func (registry *ModuleRegistry) Providers(modulePath string) []ModuleProvider {
	if registry == nil {
		return nil
	}
	providers := registry.providers[strings.TrimSpace(modulePath)]
	return append([]ModuleProvider(nil), providers...)
}

// ModulePaths returns every indexed module path, sorted.
func (registry *ModuleRegistry) ModulePaths() []string {
	if registry == nil {
		return nil
	}
	paths := make([]string, 0, len(registry.providers))
	for modulePath := range registry.providers {
		paths = append(paths, modulePath)
	}
	sort.Strings(paths)
	return paths
}

// CrossRepositoryReference is one use whose target lives in another module.
type CrossRepositoryReference struct {
	Use

	// ConsumerRepository is the repository that contains the use.
	ConsumerRepository string
	// Provider is the repository that declares the target module.
	Provider ModuleProvider
	// Providers lists every candidate when the module path is ambiguous.
	Providers []ModuleProvider

	Status CrossRepositoryStatus
	// TargetObjectPath addresses the target inside its package.
	TargetObjectPath string
	// TargetStableKey is the durable identity of the target symbol.
	TargetStableKey hotsnapshot.StableKey
	// TargetCanonicalIdentity is the auditable text behind the key.
	TargetCanonicalIdentity string
}

// CrossRepositoryOptions tunes resolution.
type CrossRepositoryOptions struct {
	// ConsumerRepository is the repository the load belongs to.
	ConsumerRepository string
	// IncludeSameModule keeps uses whose target is in the consumer module.
	// They are intra-repository facts and are excluded by default.
	IncludeSameModule bool
	// ConflictingModules are module paths whose replacement the synthetic
	// workspace had to guess. No edge may be built on them.
	ConflictingModules []string
}

// ResolveCrossRepository attributes each use to the repository that provides
// its target module and derives the durable identity of that target.
//
// The module path comes from the loader, which already followed the synthetic
// workspace and any replace directive, so the provider is the module that
// really supplied the code, not the one the import path spells.
func ResolveCrossRepository(
	ctx context.Context,
	uses []Use,
	registry *ModuleRegistry,
	options CrossRepositoryOptions,
) ([]CrossRepositoryReference, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if registry == nil {
		return nil, fmt.Errorf("module registry must not be nil")
	}

	encoders := make(map[string]*objectpath.Encoder)
	conflicting := make(map[string]bool, len(options.ConflictingModules))
	for _, modulePath := range options.ConflictingModules {
		conflicting[strings.TrimSpace(modulePath)] = true
	}
	references := make([]CrossRepositoryReference, 0)
	for _, use := range uses {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if use.TargetModulePath == "" || use.TargetPackagePath == use.PackagePath {
			continue
		}
		if !options.IncludeSameModule && use.TargetModulePath == use.ModulePath {
			continue
		}
		reference := CrossRepositoryReference{
			Use:                use,
			ConsumerRepository: options.ConsumerRepository,
		}
		providers := registry.Providers(use.TargetModulePath)
		switch {
		case conflicting[use.TargetModulePath]:
			reference.Status = ReplaceConflictTarget
		case len(providers) == 0:
			reference.Status = ModuleProviderNotFound
		case len(providers) > 1:
			reference.Status = AmbiguousModuleProvider
			reference.Providers = providers
		default:
			reference.Provider = providers[0]
			resolveTargetIdentity(&reference, encoders)
		}
		references = append(references, reference)
	}
	sort.SliceStable(references, func(left, right int) bool {
		if references[left].FileName != references[right].FileName {
			return references[left].FileName < references[right].FileName
		}
		return references[left].Offset < references[right].Offset
	})
	return references, nil
}

// resolveTargetIdentity derives the durable key of a target that lives in the
// provider repository. A target with no object path cannot be addressed from
// another repository, and that failure is reported instead of guessed.
func resolveTargetIdentity(
	reference *CrossRepositoryReference,
	encoders map[string]*objectpath.Encoder,
) {
	object := reference.Object()
	path := objectPathFor(reference.TargetPackagePath, object, encoders)
	if path == "" {
		reference.Status = ObjectPathUnavailable
		return
	}
	reference.TargetObjectPath = path

	identity := ObjectIdentity{
		Repository:    reference.Provider.Repository,
		ModulePath:    reference.Provider.ModulePath,
		PackagePath:   reference.TargetPackagePath,
		QualifiedName: reference.TargetQualifiedName,
		Kind:          reference.TargetKind,
		Signature:     targetSignature(object),
		Object:        object,
	}
	resolved, err := identity.Resolve(encoders)
	if err != nil {
		reference.Status = ObjectPathUnavailable
		return
	}
	reference.Status = CrossRepositoryResolved
	reference.TargetStableKey = resolved.StableKey
	reference.TargetCanonicalIdentity = resolved.CanonicalIdentity
}

// targetSignature prints the target type with fully qualified package paths,
// so the discriminator is comparable across loads and repositories.
//
// An instantiated generic member is signed by its declared origin: the
// consumer sees `int` where the provider declares `T`, and signing the
// instance would give one symbol two identities.
func targetSignature(object types.Object) string {
	if object == nil {
		return ""
	}
	if origin := genericOrigin(object); origin != nil {
		object = origin
	}
	return types.TypeString(object.Type(), func(other *types.Package) string {
		if other == nil {
			return ""
		}
		return other.Path()
	})
}

package facts

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/ladygraph/internal/goloader"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

// GoInput is one normalisation request: the facts of a single Go load.
type GoInput struct {
	Repository workspace.Repository
	// Definitions carry the durable identity assigned by LUQUE-0804.
	Definitions []goloader.KeyedDefinition
	// References are the classified uses of LUQUE-0806, 0807 and 0814.
	References []goloader.Reference
	// CrossRepository attributes targets to their provider repository.
	CrossRepository []goloader.CrossRepositoryReference
	// PackageDependencies are the deduplicated (source, target) package
	// pairs a package-boundary use belongs to. Each one may become
	// PACKAGE_DEPENDS_ON and, when it also crosses a Go module boundary,
	// MODULE_DEPENDS_ON.
	PackageDependencies []goloader.PackageDependency
	// TypeRelations are the IMPLEMENTS, EMBEDS and OVERRIDES facts: structural
	// relations the type checker decides on its own, with no source
	// occurrence of their own to anchor them.
	TypeRelations []goloader.TypeRelation
	// Unresolved are the classified failures of LUQUE-0810.
	Unresolved []goloader.UnresolvedReference
}

// GoReport records what normalisation could not keep, so a dropped fact is
// visible instead of silently missing.
type GoReport struct {
	// EdgesWithoutSource counts references, type relations and package
	// dependencies whose source has no durable identity in this pass,
	// typically a use at file scope or a package with no definitions.
	EdgesWithoutSource int
	// EdgesWithoutTarget counts references, type relations and package
	// dependencies whose target is not indexed in this pass and was not
	// attributed to a provider repository.
	EdgesWithoutTarget int
	// UnresolvedWithoutFile counts module-level failures with no file.
	UnresolvedWithoutFile int
}

// NormalizeGo converts the facts of one Go load into the canonical model.
//
// Only facts with a durable identity on both ends become edges. A reference
// whose target is not indexed is not downgraded to a name: it is dropped and
// counted, because a graph edge without identity is a wrong edge.
func NormalizeGo(ctx context.Context, input GoInput) (Set, GoReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Set{}, GoReport{}, err
	}
	name := strings.TrimSpace(input.Repository.Name)
	if name == "" {
		return Set{}, GoReport{}, fmt.Errorf("%w: repository name must not be empty", ErrInvalidFacts)
	}
	root := input.Repository.Path
	if strings.TrimSpace(root) == "" {
		return Set{}, GoReport{}, fmt.Errorf("%w: repository %q has no path", ErrInvalidFacts, name)
	}

	set := Set{Repositories: []Repository{{
		Key:       RepositoryKey(name),
		Name:      name,
		RootPath:  filepath.Clean(root),
		Languages: []Language{LanguageGo},
	}}}
	repositoryKey := set.Repositories[0].Key

	packages := make(map[string]Package)
	files := make(map[string]File)
	symbolsByKey := make(map[string]Symbol)
	// symbolsByQualifiedName resolves an edge endpoint declared in this pass.
	symbolsByQualifiedName := make(map[string]string)

	for _, definition := range input.Definitions {
		if err := ctx.Err(); err != nil {
			return Set{}, GoReport{}, err
		}
		packageKey := PackageKey(LanguageGo, name, definition.PackagePath)
		if _, exists := packages[packageKey]; !exists {
			packages[packageKey] = Package{
				Key:           packageKey,
				RepositoryKey: repositoryKey,
				Language:      LanguageGo,
				Name:          definition.PackagePath,
				RootPath:      filepath.Dir(definition.FileName),
				Container:     definition.ModulePath,
			}
		}
		relative := repositoryRelativePath(root, definition.FileName)
		fileKey := FileKey(name, relative)
		if _, exists := files[fileKey]; !exists {
			files[fileKey] = File{
				Key:           fileKey,
				RepositoryKey: repositoryKey,
				PackageKey:    packageKey,
				Path:          relative,
				Language:      LanguageGo,
			}
		}
		symbol := Symbol{
			Key:               string(definition.StableKey),
			CanonicalIdentity: definition.CanonicalIdentity,
			RepositoryKey:     repositoryKey,
			PackageKey:        packageKey,
			FileKey:           fileKey,
			Language:          LanguageGo,
			Name:              definition.Name,
			QualifiedName:     definition.QualifiedName,
			Kind:              string(definition.Kind),
			Exported:          definition.Exported,
			Signature:         definition.Signature,
			Start: Position{
				Line:   definition.StartLine,
				Column: definition.StartColumn,
				Offset: definition.NameOffset,
			},
			End: Position{Line: definition.EndLine, Offset: definition.DeclarationEndOffset},
		}
		symbolsByKey[symbol.Key] = symbol
		symbolsByQualifiedName[definition.PackagePath+"\x00"+definition.QualifiedName] = symbol.Key
		set.Edges = append(set.Edges, Edge{
			Kind:       Defines,
			SourceKey:  fileKey,
			TargetKey:  symbol.Key,
			Confidence: StructuralCertain,
			Provenance: GoTypesDefinition,
		})
	}

	for _, entry := range packages {
		set.Edges = append(set.Edges, Edge{
			Kind:       ContainsPackage,
			SourceKey:  repositoryKey,
			TargetKey:  entry.Key,
			Confidence: StructuralCertain,
			Provenance: PackageManifest,
		})
	}
	for _, file := range files {
		set.Edges = append(set.Edges, Edge{
			Kind:       ContainsFile,
			SourceKey:  file.PackageKey,
			TargetKey:  file.Key,
			Confidence: StructuralCertain,
			Provenance: PackageManifest,
		})
	}

	report := GoReport{}
	crossByLocation := make(map[string]goloader.CrossRepositoryReference, len(input.CrossRepository))
	for _, reference := range input.CrossRepository {
		crossByLocation[locationKey(reference.FileName, reference.Offset)] = reference
	}

	for _, reference := range input.References {
		if err := ctx.Err(); err != nil {
			return Set{}, GoReport{}, err
		}
		sourceKey, hasSource := symbolsByQualifiedName[reference.PackagePath+"\x00"+reference.SourceQualifiedName]
		if !hasSource {
			report.EdgesWithoutSource++
			continue
		}
		targetKey, confidence, provenance, resolved := resolveGoTarget(
			reference, crossByLocation, symbolsByQualifiedName)
		if !resolved {
			report.EdgesWithoutTarget++
			continue
		}
		relative := repositoryRelativePath(root, reference.FileName)
		fileKey := FileKey(name, relative)
		if _, exists := files[fileKey]; !exists {
			// A reference always lives in a file this pass already indexed.
			report.EdgesWithoutSource++
			continue
		}
		evidence := Evidence{
			Key:           EvidenceKey(fileKey, reference.Offset, reference.EndOffset),
			RepositoryKey: repositoryKey,
			FileKey:       fileKey,
			Start: Position{
				Line:   reference.StartLine,
				Column: reference.StartColumn,
				Offset: reference.Offset,
			},
			End:  Position{Line: reference.StartLine, Offset: reference.EndOffset},
			Text: reference.Name,
		}
		set.Evidence = append(set.Evidence, evidence)
		set.Edges = append(set.Edges, Edge{
			Kind:        goEdgeKind(reference.Kind),
			SourceKey:   sourceKey,
			TargetKey:   targetKey,
			Confidence:  confidence,
			Provenance:  provenance,
			EvidenceKey: evidence.Key,
		})
	}

	crossByTarget := make(map[string]goloader.CrossRepositoryReference, len(input.CrossRepository))
	for _, reference := range input.CrossRepository {
		if reference.Status != goloader.CrossRepositoryResolved || reference.TargetStableKey == "" {
			continue
		}
		crossByTarget[reference.TargetPackagePath+"\x00"+reference.TargetQualifiedName] = reference
	}

	for _, relation := range input.TypeRelations {
		if err := ctx.Err(); err != nil {
			return Set{}, GoReport{}, err
		}
		sourceKey, hasSource := symbolsByQualifiedName[relation.PackagePath+"\x00"+relation.SourceQualifiedName]
		if !hasSource {
			report.EdgesWithoutSource++
			continue
		}
		targetKey, confidence, provenance, resolved := resolveGoRelationTarget(relation, crossByTarget, symbolsByQualifiedName)
		if !resolved {
			report.EdgesWithoutTarget++
			continue
		}
		relative := repositoryRelativePath(root, relation.FileName)
		fileKey := FileKey(name, relative)
		if _, exists := files[fileKey]; !exists {
			// A relation always lives in a file this pass already indexed.
			report.EdgesWithoutSource++
			continue
		}
		evidence := Evidence{
			Key:           EvidenceKey(fileKey, relation.Offset, relation.EndOffset),
			RepositoryKey: repositoryKey,
			FileKey:       fileKey,
			Start: Position{
				Line:   relation.StartLine,
				Column: relation.StartColumn,
				Offset: relation.Offset,
			},
			End:  Position{Line: relation.StartLine, Offset: relation.EndOffset},
			Text: relation.Name,
		}
		set.Evidence = append(set.Evidence, evidence)
		set.Edges = append(set.Edges, Edge{
			Kind:        goRelationEdgeKind(relation.Kind),
			SourceKey:   sourceKey,
			TargetKey:   targetKey,
			Confidence:  confidence,
			Provenance:  provenance,
			EvidenceKey: evidence.Key,
		})
	}

	for _, dependency := range input.PackageDependencies {
		if err := ctx.Err(); err != nil {
			return Set{}, GoReport{}, err
		}
		sourcePackage, hasSource := packages[PackageKey(LanguageGo, name, dependency.PackagePath)]
		if !hasSource {
			report.EdgesWithoutSource++
			continue
		}
		target, resolved := resolveGoPackageTarget(dependency, name, packages, crossByLocation)
		if !resolved {
			report.EdgesWithoutTarget++
			continue
		}
		relative := repositoryRelativePath(root, dependency.FileName)
		fileKey := FileKey(name, relative)
		if _, exists := files[fileKey]; !exists {
			// A dependency always lives in a file this pass already indexed.
			report.EdgesWithoutSource++
			continue
		}
		evidence := Evidence{
			Key:           EvidenceKey(fileKey, dependency.Offset, dependency.EndOffset),
			RepositoryKey: repositoryKey,
			FileKey:       fileKey,
			Start: Position{
				Line:   dependency.StartLine,
				Column: dependency.StartColumn,
				Offset: dependency.Offset,
			},
			End:  Position{Line: dependency.StartLine, Offset: dependency.EndOffset},
			Text: dependency.Name,
		}
		set.Evidence = append(set.Evidence, evidence)
		set.Edges = append(set.Edges, Edge{
			Kind:        PackageDependsOn,
			SourceKey:   sourcePackage.Key,
			TargetKey:   target.Key,
			Confidence:  ExactTypechecked,
			Provenance:  target.Provenance,
			EvidenceKey: evidence.Key,
		})
		// MODULE_DEPENDS_ON has no container of its own in the model: a Go
		// module is not one of the graph's node kinds, so the edge stays
		// Package -> Package exactly like PACKAGE_DEPENDS_ON and is told
		// apart only by Kind. It is emitted in addition to, never instead
		// of, PACKAGE_DEPENDS_ON: two packages of the same module produce
		// only the first; two packages of different modules produce both,
		// because a dependency that also crosses a module boundary is
		// still, first of all, a package dependency.
		if sourcePackage.Container != target.ModulePath {
			set.Edges = append(set.Edges, Edge{
				Kind:        ModuleDependsOn,
				SourceKey:   sourcePackage.Key,
				TargetKey:   target.Key,
				Confidence:  ExactTypechecked,
				Provenance:  target.Provenance,
				EvidenceKey: evidence.Key,
			})
		}
	}

	for _, entry := range input.Unresolved {
		if err := ctx.Err(); err != nil {
			return Set{}, GoReport{}, err
		}
		unresolved := UnresolvedReference{
			RepositoryKey:    repositoryKey,
			Language:         LanguageGo,
			RequestedPackage: entry.RequestedPackagePath,
			RequestedSymbol:  entry.RequestedSymbol,
			Reason:           string(entry.Reason),
			Detail:           entry.Detail,
			Start: Position{
				Line:   entry.StartLine,
				Column: entry.StartColumn,
				Offset: entry.Offset,
			},
		}
		if entry.RequestedModulePath != "" {
			unresolved.RequestedPackage = entry.RequestedModulePath
			if entry.RequestedPackagePath != "" {
				unresolved.RequestedPackage += " " + entry.RequestedPackagePath
			}
		}
		if entry.FileName != "" {
			fileKey := FileKey(name, repositoryRelativePath(root, entry.FileName))
			if _, exists := files[fileKey]; exists {
				unresolved.FileKey = fileKey
			}
		}
		if unresolved.FileKey == "" {
			report.UnresolvedWithoutFile++
		}
		set.Unresolved = append(set.Unresolved, unresolved)
	}

	set.Packages = make([]Package, 0, len(packages))
	for _, entry := range packages {
		set.Packages = append(set.Packages, entry)
	}
	set.Files = make([]File, 0, len(files))
	for _, file := range files {
		set.Files = append(set.Files, file)
	}
	set.Symbols = make([]Symbol, 0, len(symbolsByKey))
	for _, symbol := range symbolsByKey {
		set.Symbols = append(set.Symbols, symbol)
	}
	set.Evidence = deduplicateEvidence(set.Evidence)
	set.Sort()
	return set, report, nil
}

// resolveGoTarget finds the durable identity of a reference target.
//
// A cross-repository target keeps the key derived from its provider; a local
// target uses the identity assigned in this pass. Anything else has no key and
// must not become an edge.
func resolveGoTarget(
	reference goloader.Reference,
	crossByLocation map[string]goloader.CrossRepositoryReference,
	symbolsByQualifiedName map[string]string,
) (string, Confidence, Provenance, bool) {
	if cross, exists := crossByLocation[locationKey(reference.FileName, reference.Offset)]; exists {
		if cross.Status == goloader.CrossRepositoryResolved && cross.TargetStableKey != "" {
			return string(cross.TargetStableKey), ExactTypechecked, GoObjectPath, true
		}
		return "", "", "", false
	}
	key, exists := symbolsByQualifiedName[reference.TargetPackagePath+"\x00"+reference.TargetQualifiedName]
	if !exists {
		return "", "", "", false
	}
	return key, ExactTypechecked, goProvenance(reference), true
}

func goProvenance(reference goloader.Reference) Provenance {
	switch {
	case reference.Kind == goloader.ReferenceCallsDirect:
		return GoASTCall
	case reference.Kind == goloader.ReferencePassesAsCallback:
		return GoASTCallback
	case reference.Selection != goloader.SelectionNone:
		return GoTypesSelection
	default:
		return GoTypesUse
	}
}

func goEdgeKind(kind goloader.ReferenceKind) EdgeKind {
	switch kind {
	case goloader.ReferenceCallsDirect:
		return CallsDirect
	case goloader.ReferencePassesAsCallback:
		return PassesAsCallback
	case goloader.ReferenceAssignsFunction:
		return AssignsFunction
	case goloader.ReferenceReturnsFunction:
		return ReturnsFunction
	case goloader.ReferenceTypeUses:
		return TypeUses
	default:
		return References
	}
}

// resolveGoRelationTarget finds the durable identity of a type relation's
// target: a local target uses the identity assigned in this pass, exactly
// like a reference; a cross-repository target reuses the identity any other
// reference to the same symbol already resolved, since that identity
// depends only on the target, never on the occurrence that reached it.
func resolveGoRelationTarget(
	relation goloader.TypeRelation,
	crossByTarget map[string]goloader.CrossRepositoryReference,
	symbolsByQualifiedName map[string]string,
) (string, Confidence, Provenance, bool) {
	if key, exists := symbolsByQualifiedName[relation.TargetPackagePath+"\x00"+relation.TargetQualifiedName]; exists {
		return key, ExactTypechecked, GoTypesUse, true
	}
	if cross, exists := crossByTarget[relation.TargetPackagePath+"\x00"+relation.TargetQualifiedName]; exists {
		return string(cross.TargetStableKey), ExactTypechecked, GoObjectPath, true
	}
	return "", "", "", false
}

// goRelationEdgeKind maps a structural relation to its canonical edge kind.
func goRelationEdgeKind(kind goloader.TypeRelationKind) EdgeKind {
	switch kind {
	case goloader.RelationImplements:
		return Implements
	case goloader.RelationEmbeds:
		return Embeds
	case goloader.RelationOverrides:
		return Overrides
	default:
		return References
	}
}

// packageTarget is the resolved identity of a package dependency's target:
// its durable key, its module path (Container in the model), and the
// provenance that identity carries — which differs between a local and a
// cross-repository resolution exactly like it does for a plain reference.
type packageTarget struct {
	Key        string
	ModulePath string
	Provenance Provenance
}

// resolveGoPackageTarget finds the identity of a package dependency's
// target.
//
// A local target is one of the packages this pass already indexed from its
// own definitions, found the same way the packages map itself was built. A
// cross-repository target is attributed through the CrossRepositoryReference
// of the dependency's witness use, keyed by that use's location exactly like
// resolveGoTarget keys a plain reference: the witness is a real use, so the
// same lookup that would resolve its symbol also resolves the package that
// symbol belongs to, complete with the provider repository the module
// registry attributed it to.
func resolveGoPackageTarget(
	dependency goloader.PackageDependency,
	repositoryName string,
	packages map[string]Package,
	crossByLocation map[string]goloader.CrossRepositoryReference,
) (packageTarget, bool) {
	localKey := PackageKey(LanguageGo, repositoryName, dependency.TargetPackagePath)
	if local, exists := packages[localKey]; exists {
		return packageTarget{Key: local.Key, ModulePath: local.Container, Provenance: GoTypesUse}, true
	}
	cross, exists := crossByLocation[locationKey(dependency.FileName, dependency.Offset)]
	if !exists || cross.Status != goloader.CrossRepositoryResolved {
		return packageTarget{}, false
	}
	return packageTarget{
		Key:        PackageKey(LanguageGo, cross.Provider.Repository, cross.TargetPackagePath),
		ModulePath: cross.Provider.ModulePath,
		Provenance: GoObjectPath,
	}, true
}

func locationKey(fileName string, offset int) string {
	return fmt.Sprintf("%s\x00%d", fileName, offset)
}

// repositoryRelativePath keeps paths portable: a key must not embed the
// machine that produced it.
func repositoryRelativePath(root, path string) string {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	if err != nil || strings.HasPrefix(relative, "..") {
		return filepath.ToSlash(filepath.Clean(path))
	}
	return filepath.ToSlash(relative)
}

func deduplicateEvidence(entries []Evidence) []Evidence {
	seen := make(map[string]struct{}, len(entries))
	unique := make([]Evidence, 0, len(entries))
	for _, entry := range entries {
		if _, exists := seen[entry.Key]; exists {
			continue
		}
		seen[entry.Key] = struct{}{}
		unique = append(unique, entry)
	}
	sort.Slice(unique, func(left, right int) bool { return unique[left].Key < unique[right].Key })
	return unique
}

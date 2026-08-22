package facts

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Luqueee/kivgraph/internal/rustloader"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// RustInput is one normalisation request: the facts of a single Cargo
// workspace, as the analyzer indexed it.
type RustInput struct {
	Repository workspace.Repository
	Analysis   rustloader.Analysis
}

// RustReport records what normalisation could not keep, so a dropped fact is
// visible instead of silently missing.
type RustReport struct {
	// EdgesWithoutSource counts uses with no enclosing declaration.
	EdgesWithoutSource int
	// EdgesWithoutTarget counts edges this Set could not close: an end it
	// could not place in a file or a crate of its own, or a target of this
	// repository that the pass decided not to publish. No merge resolves
	// either, so the edge is dropped here instead of dangling in the graph.
	EdgesWithoutTarget int
	// FilesWithoutPackage counts indexed files whose crate no manifest of this
	// workspace declares. They are dropped: a file belongs to exactly one
	// package, and a row without one is rejected when the generation is built.
	FilesWithoutPackage int
	// DefinitionsWithoutCrate counts symbols whose crate no manifest of this
	// workspace declares, which is a discovery and an index that disagree.
	DefinitionsWithoutCrate int
	// UnresolvedWithoutFile counts workspace level failures with no file.
	UnresolvedWithoutFile int
}

// NormalizeRust converts the analysis of one Cargo workspace into the
// canonical model.
//
// Only facts with a durable identity on both ends become edges. A target in
// another repository is named by the key its provider publishes, so this Set
// alone fails Validate with a dangling edge until the caller merges the
// provider's Set in -- the same contract the TypeScript and Go paths keep.
func NormalizeRust(ctx context.Context, input RustInput) (Set, RustReport, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Set{}, RustReport{}, err
	}
	name := strings.TrimSpace(input.Repository.Name)
	if name == "" {
		return Set{}, RustReport{}, fmt.Errorf("%w: repository name must not be empty", ErrInvalidFacts)
	}
	root := strings.TrimSpace(input.Repository.RealPath)
	if root == "" {
		root = strings.TrimSpace(input.Repository.Path)
	}
	if root == "" {
		return Set{}, RustReport{}, fmt.Errorf("%w: repository %q has no path", ErrInvalidFacts, name)
	}

	report := RustReport{EdgesWithoutSource: input.Analysis.ReferencesWithoutSource}
	repositoryKey := RepositoryKey(name)
	set := Set{Repositories: []Repository{{
		Key:       repositoryKey,
		Name:      name,
		RootPath:  filepath.Clean(root),
		Languages: []Language{LanguageRust},
	}}}

	container := workspaceContainer(root, input.Analysis.Workspace.RootPath)
	packages := make(map[string]Package, len(input.Analysis.Crates))
	packageByCrate := make(map[string]string, len(input.Analysis.Crates))
	for _, crate := range input.Analysis.Crates {
		key := PackageKey(LanguageRust, name, crate.Name)
		packages[key] = Package{
			Key:           key,
			RepositoryKey: repositoryKey,
			Language:      LanguageRust,
			Name:          crate.Name,
			Version:       crate.Version,
			RootPath:      filepath.Clean(crate.RootPath),
			ManifestPath:  crate.ManifestPath,
			Container:     container,
		}
		packageByCrate[crate.Name] = key
	}

	files := make(map[string]File, len(input.Analysis.Files))
	packageOfFile := make(map[string]string, len(input.Analysis.Files))
	for _, file := range input.Analysis.Files {
		packageKey, known := packageByCrate[file.Crate.Name]
		if !known {
			// The analyzer indexed a file whose crate no manifest of this
			// workspace declares: a path dependency into a directory the
			// discovery does not walk, which is how the standard library reaches
			// its vendored `compiler-builtins`. A file with no package is not a
			// fact this set can hold, and publishing one aborts the generation
			// after the whole corpus has been analysed. Its declarations are
			// dropped with it and its uses are declared, which is what the
			// contract says about a vendored crate nobody registered.
			report.FilesWithoutPackage++
			continue
		}
		key := FileKey(name, file.Path)
		files[key] = File{
			Key:           key,
			RepositoryKey: repositoryKey,
			PackageKey:    packageKey,
			Path:          file.Path,
			Language:      LanguageRust,
		}
		packageOfFile[file.Path] = packageKey
	}

	symbols := make(map[string]struct{}, len(input.Analysis.Definitions))
	// keyByQualifiedName and methodOwners defer the METHOD_OF pairing to a
	// second pass, because a member can be read before the type it is
	// declared on. The crate is part of the key: two crates of one workspace
	// may both declare the same path.
	keyByQualifiedName := make(map[string]string, len(input.Analysis.Definitions))
	type methodOwner struct {
		methodKey, fileKey, crate, owner string
		definition                       rustloader.Definition
	}
	methodOwners := make([]methodOwner, 0, len(input.Analysis.Definitions))
	for _, definition := range input.Analysis.Definitions {
		if err := ctx.Err(); err != nil {
			return Set{}, RustReport{}, err
		}
		packageKey, known := packageByCrate[definition.Crate.Name]
		if !known {
			// The index named a crate no manifest of this workspace
			// declares. Publishing it would invent a package.
			report.DefinitionsWithoutCrate++
			continue
		}
		fileKey := FileKey(name, definition.File)
		if _, exists := files[fileKey]; !exists {
			files[fileKey] = File{
				Key:           fileKey,
				RepositoryKey: repositoryKey,
				PackageKey:    packageKey,
				Path:          definition.File,
				Language:      LanguageRust,
			}
			packageOfFile[definition.File] = packageKey
		}
		key := string(definition.StableKey)
		set.Symbols = append(set.Symbols, Symbol{
			Key:               key,
			CanonicalIdentity: definition.CanonicalIdentity,
			RepositoryKey:     repositoryKey,
			PackageKey:        packageKey,
			FileKey:           fileKey,
			Language:          LanguageRust,
			Name:              definition.Name,
			QualifiedName:     definition.QualifiedName,
			Kind:              definition.Kind,
			Exported:          definition.Exported,
			Signature:         definition.Signature,
			Start: Position{
				Line:   definition.StartLine,
				Column: definition.StartColumn,
				Offset: definition.StartOffset,
			},
			End: Position{Line: definition.EndLine, Offset: definition.EndOffset},
		})
		symbols[key] = struct{}{}
		set.Edges = append(set.Edges, Edge{
			Kind:       Defines,
			SourceKey:  fileKey,
			TargetKey:  key,
			Confidence: StructuralCertain,
			Provenance: RustAnalyzerDefinition,
		})
		keyByQualifiedName[definition.Crate.Name+"\x00"+definition.QualifiedName] = key
		if definition.Owner != "" {
			methodOwners = append(methodOwners, methodOwner{
				methodKey:  key,
				fileKey:    fileKey,
				crate:      definition.Crate.Name,
				owner:      definition.Owner,
				definition: definition,
			})
		}
	}

	// An `impl` block lives in the crate of the type it names, so this
	// lookup is local. A member whose type is not a published symbol -- an
	// `impl` on a foreign type, or on one the index never declared -- pairs
	// with nothing rather than with a guess.
	//
	// The observation is the member's own declaration, which is what sits
	// inside the block naming the type.
	for _, pairing := range methodOwners {
		ownerKey, declared := keyByQualifiedName[pairing.crate+"\x00"+pairing.owner]
		if !declared {
			continue
		}
		evidence := Evidence{
			Key:           EvidenceKey(pairing.fileKey, pairing.definition.StartOffset, pairing.definition.EndOffset),
			RepositoryKey: repositoryKey,
			FileKey:       pairing.fileKey,
			Start: Position{
				Line:   pairing.definition.StartLine,
				Column: pairing.definition.StartColumn,
				Offset: pairing.definition.StartOffset,
			},
			End:  Position{Line: pairing.definition.EndLine, Offset: pairing.definition.EndOffset},
			Text: pairing.definition.Name,
		}
		set.Evidence = append(set.Evidence, evidence)
		set.Edges = append(set.Edges, Edge{
			Kind:        MethodOf,
			SourceKey:   pairing.methodKey,
			TargetKey:   ownerKey,
			Confidence:  StructuralCertain,
			Provenance:  RustAnalyzerDefinition,
			EvidenceKey: evidence.Key,
		})
	}

	for _, entry := range packages {
		set.Packages = append(set.Packages, entry)
		set.Edges = append(set.Edges, Edge{
			Kind:       ContainsPackage,
			SourceKey:  repositoryKey,
			TargetKey:  entry.Key,
			Confidence: StructuralCertain,
			Provenance: PackageManifest,
		})
	}
	for _, file := range files {
		set.Files = append(set.Files, file)
		if file.PackageKey == "" {
			continue
		}
		set.Edges = append(set.Edges, Edge{
			Kind:       ContainsFile,
			SourceKey:  file.PackageKey,
			TargetKey:  file.Key,
			Confidence: StructuralCertain,
			Provenance: PackageManifest,
		})
	}

	for _, reference := range input.Analysis.References {
		if err := ctx.Err(); err != nil {
			return Set{}, RustReport{}, err
		}
		fileKey := FileKey(name, reference.File)
		if _, exists := files[fileKey]; !exists {
			report.EdgesWithoutTarget++
			continue
		}
		if _, exists := symbols[reference.SourceKey]; !exists {
			report.EdgesWithoutSource++
			continue
		}
		if _, exists := symbols[reference.TargetKey]; !exists && reference.TargetRepository == "" {
			// The target is this repository's own and this Set does not
			// publish it, so the edge has no second end anywhere: a target
			// of another repository is absent on purpose and resolves when
			// the provider's Set is merged in, but this one never would.
			report.EdgesWithoutTarget++
			continue
		}
		evidence := Evidence{
			Key:           EvidenceKey(fileKey, reference.StartOffset, reference.EndOffset),
			RepositoryKey: repositoryKey,
			FileKey:       fileKey,
			Start: Position{
				Line:   reference.StartLine,
				Column: reference.StartColumn,
				Offset: reference.StartOffset,
			},
			End:  Position{Line: reference.StartLine, Offset: reference.EndOffset},
			Text: reference.Text,
		}
		set.Evidence = append(set.Evidence, evidence)
		confidence, provenance := rustReferenceTrust(reference)
		set.Edges = append(set.Edges, Edge{
			Kind:        rustEdgeKind(reference),
			SourceKey:   reference.SourceKey,
			TargetKey:   reference.TargetKey,
			Confidence:  confidence,
			Provenance:  provenance,
			EvidenceKey: evidence.Key,
		})
	}

	for _, relation := range input.Analysis.Relations {
		if err := ctx.Err(); err != nil {
			return Set{}, RustReport{}, err
		}
		fileKey := FileKey(name, relation.File)
		if _, exists := files[fileKey]; !exists {
			report.EdgesWithoutTarget++
			continue
		}
		if _, exists := symbols[relation.SourceKey]; !exists {
			// A relation whose source this pass did not publish -- an
			// implementation of a type from another crate -- belongs to the
			// repository that declares that type.
			report.EdgesWithoutSource++
			continue
		}
		if _, exists := symbols[relation.TargetKey]; !exists && relation.TargetRepository == "" {
			report.EdgesWithoutTarget++
			continue
		}
		evidence := Evidence{
			Key:           EvidenceKey(fileKey, relation.StartOffset, relation.EndOffset),
			RepositoryKey: repositoryKey,
			FileKey:       fileKey,
			Start: Position{
				Line:   relation.StartLine,
				Column: relation.StartColumn,
				Offset: relation.StartOffset,
			},
			End:  Position{Line: relation.StartLine, Offset: relation.EndOffset},
			Text: relation.Text,
		}
		set.Evidence = append(set.Evidence, evidence)
		confidence := ExactTypechecked
		if relation.TargetRepository != "" {
			confidence = ExactPackageMapped
		}
		set.Edges = append(set.Edges, Edge{
			Kind:        rustRelationKind(relation.Kind),
			SourceKey:   relation.SourceKey,
			TargetKey:   relation.TargetKey,
			Confidence:  confidence,
			Provenance:  RustSyntaxImplementation,
			EvidenceKey: evidence.Key,
		})
	}

	for _, dependency := range input.Analysis.Dependencies {
		sourceKey, known := packageByCrate[dependency.SourceCrate.Name]
		if !known {
			report.EdgesWithoutSource++
			continue
		}
		targetRepository := name
		if dependency.TargetRepository != "" {
			targetRepository = dependency.TargetRepository
		}
		targetKey := PackageKey(LanguageRust, targetRepository, dependency.TargetCrate.Name)
		if _, exists := packages[targetKey]; !exists && dependency.TargetRepository == "" {
			// A crate of this repository that this pass did not publish as a
			// package: no manifest of the workspace declares it.
			report.EdgesWithoutTarget++
			continue
		}
		fileKey := FileKey(name, dependency.File)
		if _, exists := files[fileKey]; !exists {
			report.EdgesWithoutTarget++
			continue
		}
		evidence := Evidence{
			Key:           EvidenceKey(fileKey, dependency.StartOffset, dependency.EndOffset),
			RepositoryKey: repositoryKey,
			FileKey:       fileKey,
			Start:         Position{Line: dependency.StartLine, Offset: dependency.StartOffset},
			End:           Position{Line: dependency.StartLine, Offset: dependency.EndOffset},
			Text:          dependency.Text,
		}
		set.Evidence = append(set.Evidence, evidence)
		confidence, provenance := ExactTypechecked, RustAnalyzerUse
		if dependency.TargetRepository != "" {
			confidence, provenance = ExactPackageMapped, RustAnalyzerMoniker
		}
		set.Edges = append(set.Edges, Edge{
			Kind:        PackageDependsOn,
			SourceKey:   sourceKey,
			TargetKey:   targetKey,
			Confidence:  confidence,
			Provenance:  provenance,
			EvidenceKey: evidence.Key,
		})
		if dependency.CrossWorkspace {
			set.Edges = append(set.Edges, Edge{
				Kind:        ModuleDependsOn,
				SourceKey:   sourceKey,
				TargetKey:   targetKey,
				Confidence:  confidence,
				Provenance:  provenance,
				EvidenceKey: evidence.Key,
			})
		}
	}

	fallbackPackage := fallbackCrateName(input.Analysis.Crates, name)
	for _, entry := range input.Analysis.Unresolved {
		record := UnresolvedReference{
			RepositoryKey:    repositoryKey,
			Language:         LanguageRust,
			RequestedPackage: firstNonEmpty(entry.RequestedCrate, entry.Crate.Name, fallbackPackage),
			RequestedSymbol:  entry.RequestedSymbol,
			Reason:           string(entry.Reason),
			Detail:           entry.Detail,
			Start: Position{
				Line:   entry.StartLine,
				Column: entry.StartColumn,
				Offset: entry.StartOffset,
			},
		}
		if entry.File != "" {
			fileKey := FileKey(name, entry.File)
			if _, exists := files[fileKey]; exists {
				record.FileKey = fileKey
			}
		}
		if record.FileKey == "" {
			report.UnresolvedWithoutFile++
		}
		if _, exists := symbols[entry.SourceKey]; exists {
			record.SourceSymbolKey = entry.SourceKey
		}
		set.Unresolved = append(set.Unresolved, record)
	}

	set.Sort()
	return set, report, nil
}

// rustEdgeKind answers the relation a use establishes. A `use` declaration
// binds a name, and a `pub use` offers it again: both are different from
// mentioning a symbol inside an expression.
func rustEdgeKind(reference rustloader.Reference) EdgeKind {
	switch reference.Use {
	case rustloader.UseReexport:
		return Reexports
	case rustloader.UseImport:
		return ImportsSymbol
	}
	switch reference.Kind {
	case rustloader.ReferenceCall:
		return CallsDirect
	case rustloader.ReferenceType:
		return TypeUses
	case rustloader.ReferenceCallback:
		return PassesAsCallback
	case rustloader.ReferenceAssign:
		return AssignsFunction
	case rustloader.ReferenceReturn:
		return ReturnsFunction
	default:
		return References
	}
}

// rustRelationKind maps a structural relation onto the graph vocabulary. The
// loader derives two, and IMPLEMENTS covers both granularities it reports: the
// `impl Trait for Type` header and each member of that block paired with the
// trait declaration it answers for. Rust publishes no OVERRIDES; the kind means
// a hidden promoted method, which is a Go relation with no Rust counterpart.
func rustRelationKind(kind rustloader.RelationKind) EdgeKind {
	switch kind {
	case rustloader.RelationExtends:
		return Extends
	default:
		return Implements
	}
}

// rustReferenceTrust answers how much an edge is worth and what proved it.
//
// The analyzer resolved both ends, so the confidence is the one a type checker
// earns. What the grammar decided is the class of the relation, which is why a
// call carries a syntax provenance over a type-checked resolution -- the same
// division GO_AST_CALL keeps.
func rustReferenceTrust(reference rustloader.Reference) (Confidence, Provenance) {
	confidence := ExactTypechecked
	if reference.TargetRepository != "" {
		confidence = ExactPackageMapped
	}
	switch {
	case reference.Use != rustloader.UseNone:
		if reference.TargetRepository != "" {
			return confidence, RustAnalyzerMoniker
		}
		return confidence, RustAnalyzerUse
	case reference.Kind == rustloader.ReferenceCall:
		return confidence, RustSyntaxCall
	case reference.Kind == rustloader.ReferenceCallback:
		return confidence, RustSyntaxCallback
	case reference.Kind == rustloader.ReferenceType:
		return confidence, RustSyntaxType
	case reference.TargetRepository != "":
		return confidence, RustAnalyzerMoniker
	default:
		return confidence, RustAnalyzerUse
	}
}

// workspaceContainer answers the repository relative root of the Cargo
// workspace a crate belongs to, which is the Rust analogue of a Go module.
func workspaceContainer(repositoryRoot, workspaceRoot string) string {
	if strings.TrimSpace(workspaceRoot) == "" {
		return ""
	}
	relative, err := filepath.Rel(repositoryRoot, workspaceRoot)
	if err != nil || strings.HasPrefix(relative, "..") {
		return ""
	}
	if relative == "." {
		return "."
	}
	return filepath.ToSlash(relative)
}

func fallbackCrateName(crates []workspace.CargoCrate, repository string) string {
	if len(crates) != 0 {
		return crates[0].Name
	}
	return repository
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

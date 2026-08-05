// Package facts defines the canonical semantic facts every language produces.
//
// TypeScript and Go resolve code with different engines and different
// vocabularies. This package is where both converge: after normalisation a
// fact carries no trace of the engine that produced it beyond its declared
// provenance, so the graph can store one model instead of two.
package facts

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Language identifies the engine domain a fact belongs to.
type Language string

const (
	// LanguageTypeScript marks facts produced by the TypeScript worker.
	LanguageTypeScript Language = "typescript"
	// LanguageGo marks facts produced by the Go loader.
	LanguageGo Language = "go"
)

// Confidence is the trust level of an edge, as defined by the plan.
type Confidence string

const (
	// ExactTypechecked is backed by a type checker resolution.
	ExactTypechecked Confidence = "EXACT_TYPECHECKED"
	// ExactDeclarationMapped is backed by a declaration map.
	ExactDeclarationMapped Confidence = "EXACT_DECLARATION_MAPPED"
	// ExactPackageMapped is backed by a registered package provider.
	ExactPackageMapped Confidence = "EXACT_PACKAGE_MAPPED"
	// StructuralCertain is backed by an unambiguous structural rule.
	StructuralCertain Confidence = "STRUCTURAL_CERTAIN"
	// Candidate is plausible but not proven.
	Candidate Confidence = "CANDIDATE"
	// Unresolved carries no target identity.
	Unresolved Confidence = "UNRESOLVED"
)

// Exact reports whether a confidence level may take part in exact results.
func (confidence Confidence) Exact() bool {
	switch confidence {
	case ExactTypechecked, ExactDeclarationMapped, ExactPackageMapped, StructuralCertain:
		return true
	default:
		return false
	}
}

// Provenance names the mechanism that produced a fact.
type Provenance string

const (
	TypeScriptChecker          Provenance = "TYPESCRIPT_CHECKER"
	TypeScriptModuleResolution Provenance = "TYPESCRIPT_MODULE_RESOLUTION"
	TypeScriptDeclarationMap   Provenance = "TYPESCRIPT_DECLARATION_MAP"
	TypeScriptProjectReference Provenance = "TYPESCRIPT_PROJECT_REFERENCE"

	GoTypesDefinition Provenance = "GO_TYPES_DEF"
	GoTypesUse        Provenance = "GO_TYPES_USE"
	GoTypesSelection  Provenance = "GO_TYPES_SELECTION"
	GoASTCall         Provenance = "GO_AST_CALL"
	GoASTCallback     Provenance = "GO_AST_CALLBACK"
	GoObjectPath      Provenance = "GO_OBJECT_PATH"

	TreeSitterSyntax Provenance = "TREE_SITTER_SYNTAX"
	PackageManifest  Provenance = "PACKAGE_MANIFEST"
)

// Exact reports whether a provenance can support an exact edge. A relation
// produced only by Tree-sitter never is, as the plan requires.
func (provenance Provenance) Exact() bool {
	return provenance != TreeSitterSyntax
}

// EdgeKind is the relation vocabulary of the graph.
type EdgeKind string

const (
	ContainsPackage  EdgeKind = "CONTAINS_PACKAGE"
	ContainsFile     EdgeKind = "CONTAINS_FILE"
	Defines          EdgeKind = "DEFINES"
	PackageDependsOn EdgeKind = "PACKAGE_DEPENDS_ON"
	ModuleDependsOn  EdgeKind = "MODULE_DEPENDS_ON"

	ImportsSymbol EdgeKind = "IMPORTS_SYMBOL"
	Exports       EdgeKind = "EXPORTS"
	Reexports     EdgeKind = "REEXPORTS"

	References       EdgeKind = "REFERENCES"
	CallsDirect      EdgeKind = "CALLS_DIRECT"
	PassesAsCallback EdgeKind = "PASSES_AS_CALLBACK"
	AssignsFunction  EdgeKind = "ASSIGNS_FUNCTION"
	ReturnsFunction  EdgeKind = "RETURNS_FUNCTION"

	TypeUses   EdgeKind = "TYPE_USES"
	Implements EdgeKind = "IMPLEMENTS"
	Extends    EdgeKind = "EXTENDS"
	Embeds     EdgeKind = "EMBEDS"
	Overrides  EdgeKind = "OVERRIDES"
)

var edgeKinds = map[EdgeKind]struct{}{
	ContainsPackage: {}, ContainsFile: {}, Defines: {},
	PackageDependsOn: {}, ModuleDependsOn: {},
	ImportsSymbol: {}, Exports: {}, Reexports: {},
	References: {}, CallsDirect: {}, PassesAsCallback: {},
	AssignsFunction: {}, ReturnsFunction: {},
	TypeUses: {}, Implements: {}, Extends: {}, Embeds: {}, Overrides: {},
}

// Valid reports whether the kind belongs to the graph vocabulary.
func (kind EdgeKind) Valid() bool {
	_, exists := edgeKinds[kind]
	return exists
}

// Position is a source location. Lines are one based, columns zero based.
type Position struct {
	Line   int `json:"line"`
	Column int `json:"column"`
	Offset int `json:"offset"`
}

// Repository is one indexed repository.
type Repository struct {
	Key       string     `json:"key"`
	Name      string     `json:"name"`
	RootPath  string     `json:"root_path"`
	Commit    string     `json:"commit,omitempty"`
	Branch    string     `json:"branch,omitempty"`
	Dirty     bool       `json:"dirty,omitempty"`
	Languages []Language `json:"languages,omitempty"`
}

// Package is one package or module of a repository.
type Package struct {
	Key           string   `json:"key"`
	RepositoryKey string   `json:"repository_key"`
	Language      Language `json:"language"`
	Name          string   `json:"name"`
	Version       string   `json:"version,omitempty"`
	RootPath      string   `json:"root_path"`
	ManifestPath  string   `json:"manifest_path,omitempty"`
	// Container is the module a Go package belongs to, empty otherwise.
	Container string `json:"container,omitempty"`
}

// File is one indexed source file.
type File struct {
	Key           string   `json:"key"`
	RepositoryKey string   `json:"repository_key"`
	PackageKey    string   `json:"package_key,omitempty"`
	Path          string   `json:"path"`
	Language      Language `json:"language"`
	ContentHash   string   `json:"content_hash,omitempty"`
	Generated     bool     `json:"generated,omitempty"`
}

// Symbol is one addressable declaration.
type Symbol struct {
	// Key is the durable stable key of LUQUE-0303.
	Key string `json:"key"`
	// CanonicalIdentity is the auditable text the key derives from.
	CanonicalIdentity string   `json:"canonical_identity"`
	RepositoryKey     string   `json:"repository_key"`
	PackageKey        string   `json:"package_key"`
	FileKey           string   `json:"file_key"`
	Language          Language `json:"language"`
	Name              string   `json:"name"`
	QualifiedName     string   `json:"qualified_name"`
	Kind              string   `json:"kind"`
	Exported          bool     `json:"exported"`
	Signature         string   `json:"signature,omitempty"`
	Start             Position `json:"start"`
	End               Position `json:"end"`
}

// Evidence is the observation that supports one edge.
type Evidence struct {
	Key           string   `json:"key"`
	RepositoryKey string   `json:"repository_key"`
	FileKey       string   `json:"file_key"`
	Start         Position `json:"start"`
	End           Position `json:"end"`
	// Text is the source excerpt when it is small enough to retain.
	Text string `json:"text,omitempty"`
}

// Edge is one relation between two symbols or containers.
type Edge struct {
	Kind       EdgeKind   `json:"kind"`
	SourceKey  string     `json:"source_key"`
	TargetKey  string     `json:"target_key"`
	Confidence Confidence `json:"confidence"`
	Provenance Provenance `json:"provenance"`
	// EvidenceKey links the edge to its observation, when it has one.
	EvidenceKey string `json:"evidence_key,omitempty"`
}

// UnresolvedReference is a fact that could not become an exact edge.
type UnresolvedReference struct {
	RepositoryKey string   `json:"repository_key"`
	FileKey       string   `json:"file_key,omitempty"`
	Language      Language `json:"language"`
	// SourceSymbolKey is the declaration that contains the reference.
	SourceSymbolKey  string   `json:"source_symbol_key,omitempty"`
	RequestedPackage string   `json:"requested_package,omitempty"`
	RequestedSymbol  string   `json:"requested_symbol,omitempty"`
	Reason           string   `json:"reason"`
	Detail           string   `json:"detail,omitempty"`
	Start            Position `json:"start"`
}

// Set is the normalised output of one indexing pass.
type Set struct {
	Repositories []Repository          `json:"repositories"`
	Packages     []Package             `json:"packages"`
	Files        []File                `json:"files"`
	Symbols      []Symbol              `json:"symbols"`
	Evidence     []Evidence            `json:"evidence"`
	Edges        []Edge                `json:"edges"`
	Unresolved   []UnresolvedReference `json:"unresolved"`
}

// ErrInvalidFacts reports a fact set that cannot be stored.
var ErrInvalidFacts = errors.New("invalid fact set")

// RepositoryKey builds the durable key of a repository.
func RepositoryKey(name string) string {
	return "repository:" + strings.TrimSpace(name)
}

// PackageKey builds the durable key of a package or module.
func PackageKey(language Language, repository, name string) string {
	return strings.Join([]string{"package", string(language), strings.TrimSpace(repository), strings.TrimSpace(name)}, ":")
}

// FileKey builds the durable key of a file, from its repository relative path.
func FileKey(repository, path string) string {
	return strings.Join([]string{"file", strings.TrimSpace(repository), strings.TrimSpace(path)}, ":")
}

// EvidenceKey builds the durable key of one observation.
func EvidenceKey(fileKey string, start, end int) string {
	return fmt.Sprintf("evidence:%s:%d:%d", fileKey, start, end)
}

// Sort orders every collection by its durable key, so two runs over the same
// sources produce byte identical output.
func (set *Set) Sort() {
	sort.Slice(set.Repositories, func(left, right int) bool {
		return set.Repositories[left].Key < set.Repositories[right].Key
	})
	sort.Slice(set.Packages, func(left, right int) bool {
		return set.Packages[left].Key < set.Packages[right].Key
	})
	sort.Slice(set.Files, func(left, right int) bool {
		return set.Files[left].Key < set.Files[right].Key
	})
	sort.Slice(set.Symbols, func(left, right int) bool {
		return set.Symbols[left].Key < set.Symbols[right].Key
	})
	sort.Slice(set.Evidence, func(left, right int) bool {
		return set.Evidence[left].Key < set.Evidence[right].Key
	})
	sort.Slice(set.Edges, func(left, right int) bool {
		return compareEdges(set.Edges[left], set.Edges[right])
	})
	sort.Slice(set.Unresolved, func(left, right int) bool {
		return compareUnresolved(set.Unresolved[left], set.Unresolved[right])
	})
}

func compareEdges(left, right Edge) bool {
	if left.SourceKey != right.SourceKey {
		return left.SourceKey < right.SourceKey
	}
	if left.TargetKey != right.TargetKey {
		return left.TargetKey < right.TargetKey
	}
	if left.Kind != right.Kind {
		return left.Kind < right.Kind
	}
	return left.EvidenceKey < right.EvidenceKey
}

func compareUnresolved(left, right UnresolvedReference) bool {
	if left.FileKey != right.FileKey {
		return left.FileKey < right.FileKey
	}
	if left.Start.Offset != right.Start.Offset {
		return left.Start.Offset < right.Start.Offset
	}
	if left.Reason != right.Reason {
		return left.Reason < right.Reason
	}
	return left.RequestedSymbol < right.RequestedSymbol
}

// Validate rejects a fact set that would corrupt the graph.
//
// Every reference must resolve inside the set: a dangling edge is not a
// partial fact, it is a wrong one.
func (set Set) Validate() error {
	repositories := make(map[string]struct{}, len(set.Repositories))
	for _, repository := range set.Repositories {
		if repository.Key == "" || repository.Name == "" || repository.RootPath == "" {
			return fmt.Errorf("%w: repository %q is incomplete", ErrInvalidFacts, repository.Key)
		}
		if _, duplicate := repositories[repository.Key]; duplicate {
			return fmt.Errorf("%w: duplicate repository %q", ErrInvalidFacts, repository.Key)
		}
		repositories[repository.Key] = struct{}{}
	}

	packages := make(map[string]struct{}, len(set.Packages))
	for _, entry := range set.Packages {
		if entry.Key == "" || entry.Name == "" || entry.RootPath == "" {
			return fmt.Errorf("%w: package %q is incomplete", ErrInvalidFacts, entry.Key)
		}
		if _, known := repositories[entry.RepositoryKey]; !known {
			return fmt.Errorf("%w: package %q references unknown repository %q", ErrInvalidFacts, entry.Key, entry.RepositoryKey)
		}
		if _, duplicate := packages[entry.Key]; duplicate {
			return fmt.Errorf("%w: duplicate package %q", ErrInvalidFacts, entry.Key)
		}
		packages[entry.Key] = struct{}{}
	}

	files := make(map[string]struct{}, len(set.Files))
	for _, file := range set.Files {
		if file.Key == "" || file.Path == "" {
			return fmt.Errorf("%w: file %q is incomplete", ErrInvalidFacts, file.Key)
		}
		if _, known := repositories[file.RepositoryKey]; !known {
			return fmt.Errorf("%w: file %q references unknown repository %q", ErrInvalidFacts, file.Key, file.RepositoryKey)
		}
		if file.PackageKey != "" {
			if _, known := packages[file.PackageKey]; !known {
				return fmt.Errorf("%w: file %q references unknown package %q", ErrInvalidFacts, file.Key, file.PackageKey)
			}
		}
		if _, duplicate := files[file.Key]; duplicate {
			return fmt.Errorf("%w: duplicate file %q", ErrInvalidFacts, file.Key)
		}
		files[file.Key] = struct{}{}
	}

	symbols := make(map[string]struct{}, len(set.Symbols))
	for _, symbol := range set.Symbols {
		if symbol.Key == "" || symbol.CanonicalIdentity == "" || symbol.QualifiedName == "" {
			return fmt.Errorf("%w: symbol %q is incomplete", ErrInvalidFacts, symbol.QualifiedName)
		}
		if _, known := files[symbol.FileKey]; !known {
			return fmt.Errorf("%w: symbol %q references unknown file %q", ErrInvalidFacts, symbol.Key, symbol.FileKey)
		}
		if _, known := packages[symbol.PackageKey]; !known {
			return fmt.Errorf("%w: symbol %q references unknown package %q", ErrInvalidFacts, symbol.Key, symbol.PackageKey)
		}
		symbols[symbol.Key] = struct{}{}
	}

	evidence := make(map[string]struct{}, len(set.Evidence))
	for _, entry := range set.Evidence {
		if entry.Key == "" {
			return fmt.Errorf("%w: evidence without key", ErrInvalidFacts)
		}
		if _, known := files[entry.FileKey]; !known {
			return fmt.Errorf("%w: evidence %q references unknown file %q", ErrInvalidFacts, entry.Key, entry.FileKey)
		}
		evidence[entry.Key] = struct{}{}
	}

	for _, edge := range set.Edges {
		if !edge.Kind.Valid() {
			return fmt.Errorf("%w: unknown edge kind %q", ErrInvalidFacts, edge.Kind)
		}
		if edge.Confidence.Exact() && !edge.Provenance.Exact() {
			return fmt.Errorf("%w: edge %s claims %s from %s", ErrInvalidFacts, edge.Kind, edge.Confidence, edge.Provenance)
		}
		if !known(edge.SourceKey, symbols, packages, files, repositories) {
			return fmt.Errorf("%w: edge %s has unknown source %q", ErrInvalidFacts, edge.Kind, edge.SourceKey)
		}
		if !known(edge.TargetKey, symbols, packages, files, repositories) {
			return fmt.Errorf("%w: edge %s has unknown target %q", ErrInvalidFacts, edge.Kind, edge.TargetKey)
		}
		if edge.EvidenceKey != "" {
			if _, exists := evidence[edge.EvidenceKey]; !exists {
				return fmt.Errorf("%w: edge %s references unknown evidence %q", ErrInvalidFacts, edge.Kind, edge.EvidenceKey)
			}
		}
	}

	for _, entry := range set.Unresolved {
		if entry.Reason == "" {
			return fmt.Errorf("%w: unresolved reference without reason", ErrInvalidFacts)
		}
		if _, exists := repositories[entry.RepositoryKey]; !exists {
			return fmt.Errorf("%w: unresolved reference in unknown repository %q", ErrInvalidFacts, entry.RepositoryKey)
		}
		if entry.FileKey != "" {
			if _, exists := files[entry.FileKey]; !exists {
				return fmt.Errorf("%w: unresolved reference in unknown file %q", ErrInvalidFacts, entry.FileKey)
			}
		}
		if entry.SourceSymbolKey != "" {
			if _, exists := symbols[entry.SourceSymbolKey]; !exists {
				return fmt.Errorf("%w: unresolved reference from unknown symbol %q", ErrInvalidFacts, entry.SourceSymbolKey)
			}
		}
	}
	return nil
}

func known(key string, indexes ...map[string]struct{}) bool {
	for _, index := range indexes {
		if _, exists := index[key]; exists {
			return true
		}
	}
	return false
}

// Merge appends another set, keeping the result deduplicated by durable key.
//
// Two repositories indexed in the same pass share provider symbols; merging by
// key is what keeps one symbol from becoming two nodes.
func (set *Set) Merge(other Set) {
	set.Repositories = mergeBy(set.Repositories, other.Repositories, func(value Repository) string { return value.Key })
	set.Packages = mergeBy(set.Packages, other.Packages, func(value Package) string { return value.Key })
	set.Files = mergeBy(set.Files, other.Files, func(value File) string { return value.Key })
	set.Symbols = mergeBy(set.Symbols, other.Symbols, func(value Symbol) string { return value.Key })
	set.Evidence = mergeBy(set.Evidence, other.Evidence, func(value Evidence) string { return value.Key })
	set.Edges = mergeBy(set.Edges, other.Edges, func(value Edge) string {
		return strings.Join([]string{string(value.Kind), value.SourceKey, value.TargetKey, value.EvidenceKey}, "\x00")
	})
	set.Unresolved = mergeBy(set.Unresolved, other.Unresolved, func(value UnresolvedReference) string {
		return strings.Join([]string{
			value.FileKey, value.Reason, value.RequestedPackage, value.RequestedSymbol,
			fmt.Sprint(value.Start.Offset),
		}, "\x00")
	})
	set.Sort()
}

func mergeBy[T any](left, right []T, key func(T) string) []T {
	seen := make(map[string]struct{}, len(left)+len(right))
	merged := make([]T, 0, len(left)+len(right))
	for _, collection := range [][]T{left, right} {
		for _, value := range collection {
			identifier := key(value)
			if _, exists := seen[identifier]; exists {
				continue
			}
			seen[identifier] = struct{}{}
			merged = append(merged, value)
		}
	}
	return merged
}

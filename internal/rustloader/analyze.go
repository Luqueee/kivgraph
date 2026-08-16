package rustloader

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/rustloader/scipwire"
	"github.com/Luqueee/kivgraph/internal/syntax"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// RustLanguage is the language name every Rust stable key carries.
const RustLanguage = "rust"

// UnresolvedReason classifies a Rust fact that did not become an exact edge.
type UnresolvedReason string

const (
	// UnresolvedCrateProviderNotFound means no registered repository
	// declares the crate that owns the target.
	UnresolvedCrateProviderNotFound UnresolvedReason = "CRATE_PROVIDER_NOT_FOUND"
	// UnresolvedAmbiguousCrateProvider means several repositories declare it.
	UnresolvedAmbiguousCrateProvider UnresolvedReason = "AMBIGUOUS_CRATE_PROVIDER"
	// UnresolvedCrateVersionUnknown means the analyzer named no version for
	// the crate, so no provider can be proven to be its code.
	UnresolvedCrateVersionUnknown UnresolvedReason = "CRATE_VERSION_UNKNOWN"
	// UnresolvedCrateVersionMismatch means the crate is registered at
	// another version than the one this code was compiled against.
	UnresolvedCrateVersionMismatch UnresolvedReason = "CRATE_VERSION_MISMATCH"
	// UnresolvedCrateSymbolNotMatched means the provider is registered but
	// the target's identity could not be rebuilt from it.
	UnresolvedCrateSymbolNotMatched UnresolvedReason = "CRATE_SYMBOL_NOT_MATCHED"
	// UnresolvedDefinitionNotIndexed means the grammar sees a declaration
	// the analyzer did not index.
	UnresolvedDefinitionNotIndexed UnresolvedReason = "DEFINITION_NOT_INDEXED"
	// UnresolvedTargetNotBuildable means a crate of the workspace produced
	// no indexed file at all under this build configuration.
	UnresolvedTargetNotBuildable UnresolvedReason = "TARGET_NOT_BUILDABLE"
	// UnresolvedMacroExpansionDisabled declares an index built without
	// expanding macros, so generated items are absent by configuration.
	UnresolvedMacroExpansionDisabled UnresolvedReason = "MACRO_EXPANSION_DISABLED"
	// UnresolvedWorkspaceNotLoaded means the analyzer could not read the
	// workspace.
	UnresolvedWorkspaceNotLoaded UnresolvedReason = "WORKSPACE_NOT_LOADED"
	// UnresolvedAnalyzerUnavailable means the external analyzer is missing.
	UnresolvedAnalyzerUnavailable UnresolvedReason = "ANALYZER_UNAVAILABLE"
)

// Definition is one indexed Rust declaration with durable identity.
type Definition struct {
	StableKey         hotsnapshot.StableKey
	CanonicalIdentity string
	Symbol            string
	Crate             CrateRef
	// File is the repository relative path of the declaring file.
	File          string
	Name          string
	QualifiedName string
	Kind          string
	Exported      bool
	Signature     string

	StartLine   int
	StartColumn int
	StartOffset int
	EndLine     int
	EndOffset   int
}

// Reference is one classified use with both ends identified.
type Reference struct {
	SourceKey string
	TargetKey string
	// TargetRepository is empty for a target of this repository.
	TargetRepository string
	// TargetQualifiedName describes a target of another repository, whose key
	// this pass composed without reading its declaration.
	TargetQualifiedName string
	Kind                ReferenceKind
	Use                 UseKind
	SourceCrate         CrateRef
	TargetCrate         CrateRef

	File        string
	StartLine   int
	StartColumn int
	StartOffset int
	EndOffset   int
	Text        string
}

// CrateDependency is one crate boundary a reference crossed, with the single
// occurrence that proves it.
type CrateDependency struct {
	SourceCrate CrateRef
	TargetCrate CrateRef
	// TargetRepository is empty when the provider is this repository.
	TargetRepository string
	// CrossWorkspace reports a dependency that leaves this Cargo workspace.
	CrossWorkspace bool

	File        string
	StartLine   int
	StartOffset int
	EndOffset   int
	Text        string
}

// UnresolvedReference is one classified failure with the evidence it has.
type UnresolvedReference struct {
	Crate           CrateRef
	File            string
	SourceKey       string
	RequestedCrate  string
	RequestedSymbol string
	Reason          UnresolvedReason
	Detail          string
	StartLine       int
	StartColumn     int
	StartOffset     int
}

// IndexedFile is one repository file the analyzer indexed.
type IndexedFile struct {
	Path  string
	Crate CrateRef
}

// Analysis is everything one workspace contributed.
type Analysis struct {
	Workspace    workspace.CargoWorkspace
	Crates       []workspace.CargoCrate
	Files        []IndexedFile
	Definitions  []Definition
	References   []Reference
	Dependencies []CrateDependency
	// Relations are the structural facts the grammar establishes over ends
	// the analyzer resolved: implementations, supertraits and overrides.
	Relations   []Relation
	Unresolved  []UnresolvedReference
	Diagnostics []string
	// ReferencesWithoutSource counts uses with no enclosing declaration, so
	// a dropped edge is visible instead of silently missing.
	ReferencesWithoutSource int
}

// AnalyzeOptions is one workspace's index plus what the pass knows about the
// repositories around it.
type AnalyzeOptions struct {
	Repository workspace.Repository
	Workspace  workspace.CargoWorkspace
	// Crates are the crates this workspace resolves.
	Crates []workspace.CargoCrate
	Index  scipwire.Index
	// Registry attributes a crate to the repository that provides it. A nil
	// registry resolves nothing, which is what a single repository pass is.
	Registry *CrateRegistry
	Parsers  *syntax.ParserManager
	// ProcMacros and BuildScripts record how the index was produced, because
	// an index built without them is incomplete by configuration.
	ProcMacros   bool
	BuildScripts bool
	Diagnostics  []string
}

// Analyze turns one decoded index into the facts of one Cargo workspace.
func Analyze(ctx context.Context, options AnalyzeOptions) (Analysis, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Analysis{}, err
	}
	repositoryName := strings.TrimSpace(options.Repository.Name)
	if repositoryName == "" {
		return Analysis{}, fmt.Errorf("rust analysis: repository name must not be empty")
	}
	repositoryRoot := strings.TrimSpace(options.Repository.RealPath)
	if repositoryRoot == "" {
		repositoryRoot = strings.TrimSpace(options.Repository.Path)
	}
	if repositoryRoot == "" {
		return Analysis{}, fmt.Errorf("rust analysis: repository %q has no path", repositoryName)
	}

	analysis := Analysis{
		Workspace:   options.Workspace,
		Crates:      options.Crates,
		Diagnostics: append([]string(nil), options.Diagnostics...),
	}
	symbols := symbolInformation(options.Index)
	observed := observedSymbolStrings(options.Index)
	crateOf := crateLocator(repositoryRoot, options.Crates)

	documents := make([]*documentAnalysis, 0, len(options.Index.Documents))
	defer func() {
		for _, document := range documents {
			document.source.Close()
		}
	}()

	definitions := make(map[string]Definition)
	byKey := make(map[string]string)
	// duplicated records every extra document that defines a symbol another
	// document already defined. One analyzer symbol is one node, so only one
	// of them can be published and the rest are a hole the graph has to name.
	duplicated := make(map[string][]string)
	// ranks remembers the Cargo target the published definition came from,
	// which is what a later document has to beat to replace it.
	ranks := make(map[string]int)
	for _, document := range options.Index.Documents {
		if err := ctx.Err(); err != nil {
			return Analysis{}, err
		}
		prepared, err := prepareDocument(ctx, options, repositoryRoot, document)
		if err != nil {
			return Analysis{}, err
		}
		if prepared == nil {
			continue
		}
		documents = append(documents, prepared)
		crate, cratePrefix := crateOf(prepared.path)
		analysis.Files = append(analysis.Files, IndexedFile{Path: prepared.path, Crate: crate})
		rank := cargoTargetRank(prepared.path, cratePrefix)
		collected, err := collectDefinitions(repositoryName, prepared, symbols)
		if err != nil {
			return Analysis{}, err
		}
		for symbol, definition := range collected {
			previous, exists := definitions[symbol]
			if !exists {
				definitions[symbol] = definition
				ranks[symbol] = rank
				continue
			}
			if previous.File == definition.File {
				// One file compiled into two crates of the same package
				// arrives as two documents. Both name the same declaration,
				// so there is nothing to choose and nothing missing.
				continue
			}
			// The analyzer defines one symbol in two documents: a package
			// compiles into several crates -- a library, a binary, a build
			// script, one per integration test -- and its moniker names the
			// package, so every root module of it arrives as the same
			// symbol. Only one node can carry the key: it is the one Cargo
			// compiles into the most reachable target, and the path breaks a
			// tie so the graph never depends on the order the analyzer
			// happened to emit its documents in.
			loser := definition
			if rank < ranks[symbol] || (rank == ranks[symbol] && definition.File < previous.File) {
				loser = previous
				definitions[symbol] = definition
				ranks[symbol] = rank
			}
			duplicated[symbol] = append(duplicated[symbol], loser.File)
		}
		analysis.Diagnostics = append(analysis.Diagnostics, collidingKeys(collected, byKey)...)
	}
	analysis.Diagnostics = append(analysis.Diagnostics, duplicateDefinitions(duplicated, definitions)...)
	collected := make([]Definition, 0, len(definitions))
	for _, definition := range definitions {
		collected = append(collected, definition)
	}
	sort.Slice(collected, func(left, right int) bool {
		return collected[left].Symbol < collected[right].Symbol
	})
	// Two analyzer symbols can share one stable key: the key carries the crate
	// name, the descriptor path and the kind, never the crate version, and
	// rust-analyzer has bugs that emit the same declaration twice under
	// different versions -- the standard library reports several of them about
	// its own `std` crate root. `collidingKeys` names them; this decides which
	// one wins, and it has to be the same one on every pass. Chosen by walking
	// the map it was not: iteration order picked the winner, and with it whether
	// the 171 re-exports at the root of `std` had a source symbol at all. Two
	// passes over one toolchain published different graphs.
	//
	// Only the winner is published. A key is one node, and a graph carrying two
	// declarations of it asserts that one symbol lives in two files -- which the
	// canonical schema rejects when the generation is built, after the whole
	// corpus has been analysed.
	byDefinitionKey := make(map[string]Definition, len(collected))
	analysis.Definitions = make([]Definition, 0, len(collected))
	for _, definition := range collected {
		if _, exists := byDefinitionKey[string(definition.StableKey)]; exists {
			continue
		}
		byDefinitionKey[string(definition.StableKey)] = definition
		analysis.Definitions = append(analysis.Definitions, definition)
	}

	resolver := &targetResolver{
		repository:  repositoryName,
		definitions: definitions,
		byKey:       byDefinitionKey,
		registry:    options.Registry,
	}
	dependencies := make(map[string]CrateDependency)
	unresolved := make(map[string]UnresolvedReference)
	for _, document := range documents {
		if err := ctx.Err(); err != nil {
			return Analysis{}, err
		}
		relations, consumed := collectRelations(document, resolver, observed)
		analysis.Relations = append(analysis.Relations, relations...)
		collectReferences(document, resolver, &analysis, dependencies, unresolved, options, consumed)
		collectMissingDefinitions(document, unresolved)
	}
	collectEmptyCrates(options, analysis.Files, unresolved)
	if !options.ProcMacros {
		key := string(UnresolvedMacroExpansionDisabled)
		unresolved[key] = UnresolvedReference{
			Reason: UnresolvedMacroExpansionDisabled,
			Detail: "the index was built with procedural macro expansion disabled",
		}
	}

	analysis.Dependencies = sortedDependencies(dependencies)
	analysis.Unresolved = sortedUnresolved(unresolved)
	sort.SliceStable(analysis.References, func(left, right int) bool {
		if analysis.References[left].File != analysis.References[right].File {
			return analysis.References[left].File < analysis.References[right].File
		}
		return analysis.References[left].StartOffset < analysis.References[right].StartOffset
	})
	sort.Slice(analysis.Files, func(left, right int) bool {
		return analysis.Files[left].Path < analysis.Files[right].Path
	})
	return analysis, nil
}

// documentAnalysis is one indexed file with its parsed source and the ranges
// its definitions own.
type documentAnalysis struct {
	path     string
	document scipwire.Document
	source   *Source
	// bodies are the definition spans of this document, innermost last.
	bodies []definitionBody
}

type definitionBody struct {
	start int
	end   int
	key   string
}

func prepareDocument(
	ctx context.Context,
	options AnalyzeOptions,
	repositoryRoot string,
	document scipwire.Document,
) (*documentAnalysis, error) {
	absolute := filepath.Join(options.Workspace.RootPath, filepath.FromSlash(document.RelativePath))
	relative, err := filepath.Rel(repositoryRoot, absolute)
	if err != nil || strings.HasPrefix(relative, "..") {
		// A document outside the repository is the sysroot or a dependency
		// checkout: it is not a file of this repository and contributes no
		// node to the graph.
		return nil, nil
	}
	if workspace.CargoExcludes(repositoryRoot, absolute, options.Repository.Exclusions) {
		// Cargo discovery never walked this directory, so no manifest below
		// it was read and no crate it declares belongs to this repository. A
		// vendored crate patched into the build is real code the analyzer
		// indexes and this repository does not provide: publishing its
		// declarations would invent a package, and publishing only the uses
		// of them would leave every one of those edges dangling.
		return nil, nil
	}
	code, err := os.ReadFile(absolute)
	if err != nil {
		return nil, fmt.Errorf("read indexed file %q: %w", absolute, err)
	}
	source, err := NewSource(ctx, options.Parsers, filepath.ToSlash(relative), code)
	if err != nil {
		return nil, err
	}
	return &documentAnalysis{path: filepath.ToSlash(relative), document: document, source: source}, nil
}

// symbolInformation indexes everything the analyzer said about each symbol,
// from the documents and from the symbols it referenced without indexing.
func symbolInformation(index scipwire.Index) map[string]scipwire.SymbolInformation {
	symbols := make(map[string]scipwire.SymbolInformation)
	for _, document := range index.Documents {
		for _, symbol := range document.Symbols {
			if _, exists := symbols[symbol.Symbol]; !exists {
				symbols[symbol.Symbol] = symbol
			}
		}
	}
	for _, symbol := range index.ExternalSymbols {
		if _, exists := symbols[symbol.Symbol]; !exists {
			symbols[symbol.Symbol] = symbol
		}
	}
	return symbols
}

// crateLocator answers which crate owns a repository relative path and where
// that crate is rooted, choosing the deepest crate root that contains it. A
// file below no crate root belongs to no crate, which is a fact the caller
// reports rather than guesses around.
func crateLocator(repositoryRoot string, crates []workspace.CargoCrate) func(string) (CrateRef, string) {
	type entry struct {
		prefix string
		crate  CrateRef
	}
	entries := make([]entry, 0, len(crates))
	for _, crate := range crates {
		relative, err := filepath.Rel(repositoryRoot, crate.RootPath)
		if err != nil || strings.HasPrefix(relative, "..") {
			continue
		}
		prefix := filepath.ToSlash(relative)
		if prefix == "." {
			prefix = ""
		}
		entries = append(entries, entry{
			prefix: prefix,
			crate:  CrateRef{Name: crate.Name, Version: crate.Version},
		})
	}
	sort.Slice(entries, func(left, right int) bool {
		if len(entries[left].prefix) != len(entries[right].prefix) {
			return len(entries[left].prefix) > len(entries[right].prefix)
		}
		return entries[left].crate.Name < entries[right].crate.Name
	})
	return func(path string) (CrateRef, string) {
		for _, candidate := range entries {
			if candidate.prefix == "" || strings.HasPrefix(path, candidate.prefix+"/") {
				return candidate.crate, candidate.prefix
			}
		}
		return CrateRef{}, ""
	}
}

// Cargo target ranks order the crates one package compiles into. Only the
// library is linkable, so it is the target another repository can name; a
// binary is next, and a build script, a test, a bench or an example is a leaf
// nothing outside the package ever reaches.
const (
	cargoTargetLibrary = iota
	cargoTargetBinary
	cargoTargetBuildScript
	cargoTargetTest
	cargoTargetBench
	cargoTargetExample
	cargoTargetUnknown
)

// cargoTargetRank classifies a repository relative path by the Cargo target
// that compiles it, following Cargo's own directory conventions.
//
// The rank decides which document publishes a symbol every target of a package
// declares -- their root modules share one moniker, because a moniker names
// the package -- so a manifest that renames a target with an explicit `path`
// moves a node between two documents that both define it, and never invents
// or loses a fact.
func cargoTargetRank(path, cratePrefix string) int {
	relative := path
	if cratePrefix != "" {
		relative = strings.TrimPrefix(path, cratePrefix+"/")
	}
	switch {
	case relative == "build.rs":
		return cargoTargetBuildScript
	case relative == "src/main.rs" || strings.HasPrefix(relative, "src/bin/"):
		return cargoTargetBinary
	case strings.HasPrefix(relative, "src/"):
		return cargoTargetLibrary
	case strings.HasPrefix(relative, "tests/"):
		return cargoTargetTest
	case strings.HasPrefix(relative, "benches/"):
		return cargoTargetBench
	case strings.HasPrefix(relative, "examples/"):
		return cargoTargetExample
	}
	return cargoTargetUnknown
}

func collectDefinitions(
	repository string,
	document *documentAnalysis,
	symbols map[string]scipwire.SymbolInformation,
) (map[string]Definition, error) {
	definitions := make(map[string]Definition)
	for _, occurrence := range document.document.Occurrences {
		if !occurrence.Definition() {
			continue
		}
		identity, err := ParseSymbol(occurrence.Symbol)
		if err != nil || !identity.Addressable() {
			continue
		}
		startOffset, ok := document.source.Offset(int(occurrence.Range.StartLine), int(occurrence.Range.StartCharacter))
		if !ok {
			continue
		}
		endOffset, ok := document.source.Offset(int(occurrence.Range.EndLine), int(occurrence.Range.EndCharacter))
		if !ok {
			continue
		}
		information := symbols[occurrence.Symbol]
		definition := Definition{
			Symbol:        occurrence.Symbol,
			Crate:         identity.Crate,
			File:          document.path,
			Name:          PublishedName(identity, information.DisplayName),
			QualifiedName: identity.QualifiedName(),
			Kind:          PublishedKind(identity, information.Kind),
			Signature:     information.Signature,
			StartLine:     int(occurrence.Range.StartLine) + 1,
			StartColumn:   int(occurrence.Range.StartCharacter),
			StartOffset:   startOffset,
			EndLine:       int(occurrence.Range.EndLine) + 1,
			EndOffset:     endOffset,
		}
		key, canonical, err := StableKeyFor(repository, identity)
		if err != nil {
			return nil, err
		}
		definition.StableKey = key
		definition.CanonicalIdentity = canonical

		body := occurrence.Range
		if occurrence.EnclosingRange.Present {
			body = occurrence.EnclosingRange
		}
		bodyStart, startOK := document.source.Offset(int(body.StartLine), int(body.StartCharacter))
		bodyEnd, endOK := document.source.Offset(int(body.EndLine), int(body.EndCharacter))
		if startOK && endOK {
			definition.EndLine = int(body.EndLine) + 1
			definition.EndOffset = bodyEnd
			document.bodies = append(document.bodies, definitionBody{
				start: bodyStart, end: bodyEnd, key: string(key),
			})
		}
		if identity.Kind() == SuffixNamespace && len(identity.Descriptors) == 1 {
			// The crate root module has no declaration of its own to read a
			// visibility from, and every crate offers it.
			definition.Exported = true
		} else {
			definition.Exported = document.source.Exported(startOffset, endOffset)
		}
		definitions[occurrence.Symbol] = definition
	}
	// The innermost body wins when a use falls inside several: a call inside
	// a method belongs to the method, not to the module that holds it.
	sort.SliceStable(document.bodies, func(left, right int) bool {
		return document.bodies[left].end-document.bodies[left].start <
			document.bodies[right].end-document.bodies[right].start
	})

	return definitions, nil
}

// StableKeyFor derives the durable identity of one Rust symbol.
//
// The identity is the analyzer's own symbol string: crate, descriptor path and
// the suffix that separates a type from a term of the same name. The signature
// is deliberately not part of it: rust-analyzer emits no SymbolInformation for
// a declaration outside the workspace root it was pointed at, so a consumer
// that keyed on the signature could never name the key its provider publishes.
func StableKeyFor(repository string, identity SymbolIdentity) (hotsnapshot.StableKey, string, error) {
	stable := hotsnapshot.StableKeyIdentity{
		FormatVersion: hotsnapshot.StableKeyFormatVersion,
		Language:      RustLanguage,
		Repository:    repository,
		Package:       identity.Crate.Name,
		QualifiedName: "scip:" + identity.QualifiedName(),
		Kind:          string(identity.Kind()),
		Discriminator: rustDiscriminator(identity),
	}
	canonical, err := stable.Canonical()
	if err != nil {
		return "", "", fmt.Errorf("symbol %q identity: %w", identity.Raw, err)
	}
	key, err := stable.Key()
	if err != nil {
		return "", "", fmt.Errorf("symbol %q key: %w", identity.Raw, err)
	}
	return key, canonical, nil
}

// rustDiscriminator separates symbols that share every other identity field.
// SCIP spells that difference in the descriptor itself: a method descriptor
// carries a disambiguator when a crate declares the same path twice.
func rustDiscriminator(identity SymbolIdentity) string {
	if len(identity.Descriptors) == 0 {
		return "none"
	}
	last := identity.Descriptors[len(identity.Descriptors)-1]
	if disambiguator := strings.TrimSpace(last.Disambiguator); disambiguator != "" {
		return disambiguator
	}
	return "none"
}

// collidingKeys names two distinct analyzer symbols that would publish one
// node.
//
// The merge keeps the first record of every key, so a collision silently drops
// a declaration. rust-analyzer has known bugs that emit duplicate symbols; a
// graph missing a symbol for that reason has to say so.
func collidingKeys(collected map[string]Definition, byKey map[string]string) []string {
	symbols := make([]string, 0, len(collected))
	for symbol := range collected {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	collisions := make([]string, 0)
	for _, symbol := range symbols {
		key := string(collected[symbol].StableKey)
		previous, exists := byKey[key]
		if !exists {
			byKey[key] = symbol
			continue
		}
		if previous == symbol {
			continue
		}
		collisions = append(collisions, "two symbols share one identity: "+previous+" and "+symbol)
	}
	return collisions
}

// duplicateDefinitions names one analyzer symbol defined in several
// documents.
//
// One package compiles into several crates -- a library, a binary, one per
// integration test -- and a moniker names the package, so the root module of
// each of them, and any item they declare under the same path, arrive as one
// symbol defined more than once. Only one of them can be the node, and the
// declarations left out are a hole the graph has to name.
func duplicateDefinitions(duplicated map[string][]string, definitions map[string]Definition) []string {
	if len(duplicated) == 0 {
		return nil
	}
	symbols := make([]string, 0, len(duplicated))
	for symbol := range duplicated {
		symbols = append(symbols, symbol)
	}
	sort.Strings(symbols)
	reported := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		files := append([]string(nil), duplicated[symbol]...)
		sort.Strings(files)
		reported = append(reported, fmt.Sprintf(
			"the analyzer defines %q in more than one document: published from %s, not from %s",
			symbol, definitions[symbol].File, strings.Join(files, ", ")))
	}
	return reported
}

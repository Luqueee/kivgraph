package rustloader

import (
	"fmt"
	"sort"

	"github.com/Luqueee/ladygraph/internal/workspace"
)

// callableKinds are the published kinds that can stand behind a function
// value. A constant or a type written where a value goes is not a callback,
// however the grammar shapes the expression.
var callableKinds = map[string]bool{
	"function":      true,
	"method":        true,
	"static_method": true,
	"trait_method":  true,
}

// valueClass keeps a value-position class only when the target is provably
// callable.
//
// The grammar decides the shape and the analyzer decides the target, but
// neither alone proves the edge: `takes(LIMIT)` is an argument that is not a
// callback. A target this pass did not index carries no kind -- it belongs to
// another repository -- and the class degrades to a plain reference rather
// than claiming what was never read.
func valueClass(kind ReferenceKind, targetKind string) ReferenceKind {
	switch kind {
	case ReferenceCallback, ReferenceAssign, ReferenceReturn:
		if !callableKinds[targetKind] {
			return ReferenceUse
		}
	}
	return kind
}

// targetResolver attributes one referenced symbol to the identity it will have
// in the published graph.
type targetResolver struct {
	repository  string
	definitions map[string]Definition
	// byKey indexes the same definitions by the key they publish, which is
	// what an enclosing body is named by.
	byKey    map[string]Definition
	registry *CrateRegistry
}

// resolution is what the resolver could prove about a referenced symbol.
type resolution struct {
	key        string
	repository string
	crate      CrateRef
	// kind is the published kind of the target when this pass indexed it. A
	// target resolved through the registry belongs to another repository and
	// this pass never read its declaration, so the field stays empty.
	kind string
	// qualifiedName is how the target spells its path. A target of another
	// repository is composed, not read, so this is the only description of it
	// this pass can carry to whoever finds out the provider never published it.
	qualifiedName string
	reason        UnresolvedReason
	detail        string
}

// resolve answers the durable identity of a referenced symbol.
//
// A symbol indexed in this same pass answers itself. Everything else has to be
// attributed to a registered provider and rebuilt from the identity the
// analyzer gave it, which is the only way a consumer can name the key its
// provider publishes without guessing.
func (resolver *targetResolver) resolve(identity SymbolIdentity) resolution {
	if definition, exists := resolver.definitions[identity.Raw]; exists {
		return resolution{key: string(definition.StableKey), crate: definition.Crate, kind: definition.Kind}
	}
	if !identity.Crate.Known() {
		return resolution{
			crate: identity.Crate, reason: UnresolvedCrateVersionUnknown,
			detail: fmt.Sprintf("crate %q carries no version", identity.Crate.Name),
		}
	}
	provider, status := resolver.registry.Resolve(identity.Crate.Name, identity.Crate.Version)
	switch status {
	case CrateResolved:
	case AmbiguousCrateProvider:
		return resolution{crate: identity.Crate, reason: UnresolvedAmbiguousCrateProvider}
	case CrateVersionMismatch:
		return resolution{
			crate: identity.Crate, reason: UnresolvedCrateVersionMismatch,
			detail: fmt.Sprintf("no registered repository provides %s at %s", identity.Crate.Name, identity.Crate.Version),
		}
	case CrateVersionUnknown:
		return resolution{crate: identity.Crate, reason: UnresolvedCrateVersionUnknown}
	default:
		return resolution{crate: identity.Crate, reason: UnresolvedCrateProviderNotFound}
	}

	key, _, err := StableKeyFor(provider.Repository, identity)
	if err != nil {
		// An identity this build cannot render is not an edge it may guess.
		return resolution{
			crate: identity.Crate, reason: UnresolvedCrateSymbolNotMatched, detail: err.Error(),
		}
	}
	if provider.Repository == resolver.repository {
		// The crate is this repository's own, and this pass did not index
		// the declaration: an `impl` block, which SCIP mentions but never
		// defines, or an item the build configuration left out. Naming a key
		// nobody publishes is a dangling edge, so it is declared instead.
		return resolution{
			crate:  identity.Crate,
			reason: UnresolvedDefinitionNotIndexed,
			detail: "the crate belongs to this repository and the index defines no such declaration",
		}
	}
	return resolution{
		key:           string(key),
		repository:    provider.Repository,
		crate:         identity.Crate,
		qualifiedName: identity.QualifiedName(),
	}
}

// collectReferences turns the non-definition occurrences of one document into
// references, dependencies and classified failures.
func collectReferences(
	document *documentAnalysis,
	resolver *targetResolver,
	analysis *Analysis,
	dependencies map[string]CrateDependency,
	unresolved map[string]UnresolvedReference,
	options AnalyzeOptions,
	consumed map[int]struct{},
) {
	for _, occurrence := range document.document.Occurrences {
		if occurrence.Definition() {
			continue
		}
		identity, err := ParseSymbol(occurrence.Symbol)
		if err != nil || !identity.Addressable() {
			// A local binding has no durable identity, so a use of one is
			// not an edge of the graph.
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
		if _, taken := consumed[startOffset]; taken {
			// The occurrence already travels as an implementation or a
			// supertrait: publishing it again as a plain type mention would
			// count one observation twice.
			continue
		}
		sourceKey, sourceCrate := document.enclosing(startOffset, endOffset, resolver)
		resolved := resolver.resolve(identity)
		if resolved.key == "" {
			// A failure to resolve is an observation of this file, not of
			// the declaration that happens to contain it: the record travels
			// with its file and position, and names a source only when this
			// document publishes one.
			record := UnresolvedReference{
				Crate:           sourceCrate,
				File:            document.path,
				SourceKey:       sourceKey,
				RequestedCrate:  identity.Crate.Name,
				RequestedSymbol: identity.QualifiedName(),
				Reason:          resolved.reason,
				Detail:          resolved.detail,
				StartLine:       int(occurrence.Range.StartLine) + 1,
				StartColumn:     int(occurrence.Range.StartCharacter),
				StartOffset:     startOffset,
			}
			key := string(record.Reason) + "\x00" + record.RequestedCrate + "\x00" + record.RequestedSymbol
			if _, exists := unresolved[key]; !exists {
				unresolved[key] = record
			}
			continue
		}
		if sourceKey == "" {
			// The use resolves and no declaration this document publishes
			// contains it, so the graph has no end to hang the edge on.
			analysis.ReferencesWithoutSource++
			continue
		}
		if resolved.key == sourceKey {
			// A declaration mentioning itself -- the name of a recursive
			// function, say -- is not a relation between two symbols.
			continue
		}

		reference := Reference{
			SourceKey:           sourceKey,
			TargetKey:           resolved.key,
			TargetRepository:    resolved.repository,
			TargetQualifiedName: resolved.qualifiedName,
			Kind:                valueClass(document.source.Reference(startOffset, endOffset), resolved.kind),
			Use:                 document.source.Use(startOffset, endOffset),
			SourceCrate:         sourceCrate,
			TargetCrate:         resolved.crate,
			File:                document.path,
			StartLine:           int(occurrence.Range.StartLine) + 1,
			StartColumn:         int(occurrence.Range.StartCharacter),
			StartOffset:         startOffset,
			EndOffset:           endOffset,
			Text:                document.source.Text(startOffset, endOffset),
		}
		analysis.References = append(analysis.References, reference)

		if sourceCrate.Name == "" || resolved.crate.Name == "" || sourceCrate.Name == resolved.crate.Name {
			continue
		}
		dependencyKey := sourceCrate.Name + "\x00" + resolved.crate.Name
		if _, exists := dependencies[dependencyKey]; exists {
			continue
		}
		dependencies[dependencyKey] = CrateDependency{
			SourceCrate:      sourceCrate,
			TargetCrate:      resolved.crate,
			TargetRepository: resolved.repository,
			CrossWorkspace:   resolved.repository != "" || !localCrate(options.Crates, resolved.crate.Name),
			File:             document.path,
			StartLine:        reference.StartLine,
			StartOffset:      startOffset,
			EndOffset:        endOffset,
			Text:             reference.Text,
		}
	}
}

// enclosing answers the declaration that contains a span, which is the source
// of every reference edge.
//
// SCIP attaches an enclosing range to the occurrences of a definition, so the
// innermost definition body that contains the use is the declaration the use
// belongs to. The analyzer never states this relation directly.
//
// A body whose symbol the graph publishes in another document names no
// declaration of this file: the analyzer defines that symbol in several
// documents and only one of them is the node. The use belongs to the next
// declaration out that this file does publish -- a use inside a module is a
// use inside everything that contains it -- and to none when there is no such
// declaration.
func (document *documentAnalysis) enclosing(start, end int, resolver *targetResolver) (string, CrateRef) {
	for _, body := range document.bodies {
		if body.key == "" || start < body.start || end > body.end {
			continue
		}
		definition, published := resolver.byKey[body.key]
		if !published || definition.File != document.path {
			continue
		}
		return body.key, definition.Crate
	}
	return "", CrateRef{}
}

// collectMissingDefinitions compares what the grammar sees with what the
// analyzer indexed. A declaration with no definition occurrence is a hole in
// the graph, and the only way to see it is to look for it.
func collectMissingDefinitions(document *documentAnalysis, unresolved map[string]UnresolvedReference) {
	indexed := make([]int, 0, len(document.document.Occurrences))
	for _, occurrence := range document.document.Occurrences {
		if !occurrence.Definition() {
			continue
		}
		offset, ok := document.source.Offset(int(occurrence.Range.StartLine), int(occurrence.Range.StartCharacter))
		if !ok {
			continue
		}
		indexed = append(indexed, offset)
	}
	for _, declaration := range document.source.Declarations() {
		if declaration.Name == "" {
			continue
		}
		covered := false
		for _, offset := range indexed {
			if offset >= declaration.StartByte && offset < declaration.EndByte {
				covered = true
				break
			}
		}
		if covered {
			continue
		}
		key := string(UnresolvedDefinitionNotIndexed) + "\x00" + document.path + "\x00" + declaration.Name
		if _, exists := unresolved[key]; exists {
			continue
		}
		unresolved[key] = UnresolvedReference{
			File:            document.path,
			RequestedSymbol: declaration.Name,
			Reason:          UnresolvedDefinitionNotIndexed,
			Detail:          "the grammar sees a " + declaration.NodeKind + " the analyzer did not index",
			StartLine:       declaration.StartLine + 1,
			StartOffset:     declaration.StartByte,
		}
	}
}

// collectEmptyCrates declares a crate whose files the build configuration
// selected none of. Its symbols are absent by construction, not by failure.
func collectEmptyCrates(options AnalyzeOptions, files []IndexedFile, unresolved map[string]UnresolvedReference) {
	indexed := make(map[string]struct{}, len(files))
	for _, file := range files {
		if file.Crate.Name != "" {
			indexed[file.Crate.Name] = struct{}{}
		}
	}
	for _, crate := range options.Crates {
		if _, exists := indexed[crate.Name]; exists {
			continue
		}
		key := string(UnresolvedTargetNotBuildable) + "\x00" + crate.Name
		unresolved[key] = UnresolvedReference{
			Crate:          CrateRef{Name: crate.Name, Version: crate.Version},
			RequestedCrate: crate.Name,
			Reason:         UnresolvedTargetNotBuildable,
			Detail:         "the build configuration of this index selected no file of the crate",
		}
	}
}

func localCrate(crates []workspace.CargoCrate, name string) bool {
	for _, crate := range crates {
		if crate.Name == name {
			return true
		}
	}
	return false
}

func sortedDependencies(dependencies map[string]CrateDependency) []CrateDependency {
	result := make([]CrateDependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		result = append(result, dependency)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].SourceCrate.Name != result[right].SourceCrate.Name {
			return result[left].SourceCrate.Name < result[right].SourceCrate.Name
		}
		return result[left].TargetCrate.Name < result[right].TargetCrate.Name
	})
	return result
}

func sortedUnresolved(unresolved map[string]UnresolvedReference) []UnresolvedReference {
	result := make([]UnresolvedReference, 0, len(unresolved))
	for _, entry := range unresolved {
		result = append(result, entry)
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Reason != result[right].Reason {
			return result[left].Reason < result[right].Reason
		}
		if result[left].File != result[right].File {
			return result[left].File < result[right].File
		}
		if result[left].RequestedCrate != result[right].RequestedCrate {
			return result[left].RequestedCrate < result[right].RequestedCrate
		}
		return result[left].RequestedSymbol < result[right].RequestedSymbol
	})
	return result
}

package rustloader

import (
	"sort"
	"strings"

	"github.com/Luqueee/ladygraph/internal/rustloader/scipwire"
)

// RelationKind is a structural relation between two declarations.
//
// The analyzer states none of these: `SymbolInformation.relationships` travels
// empty. What it does state is which symbol every token of an `impl` header or
// a trait bound resolves to, and the grammar says which token is the trait and
// which is the type. Both ends are the analyzer's; only the shape is the
// grammar's, exactly as a call is.
type RelationKind string

const (
	// RelationImplements is `impl Trait for Type`.
	RelationImplements RelationKind = "implements"
	// RelationExtends is the supertrait of `trait A: B`.
	RelationExtends RelationKind = "extends"
	// RelationOverrides is a method of a trait implementation and the trait
	// method it answers for.
	RelationOverrides RelationKind = "overrides"
)

// Relation is one structural relation with the occurrence that proves it.
type Relation struct {
	Kind      RelationKind
	SourceKey string
	TargetKey string
	// TargetRepository is empty for a target of this repository.
	TargetRepository string
	// TargetCrate and TargetQualifiedName describe a target of another
	// repository, whose key this pass composed without reading its
	// declaration.
	TargetCrate         CrateRef
	TargetQualifiedName string

	File        string
	StartLine   int
	StartColumn int
	StartOffset int
	EndOffset   int
	Text        string
}

// occurrenceIndex answers the occurrences of a document by byte span, which is
// how a grammar node is joined with what the analyzer resolved there.
type occurrenceIndex struct {
	entries []occurrenceEntry
}

type occurrenceEntry struct {
	start    int
	end      int
	line     int
	column   int
	symbol   string
	identity SymbolIdentity
}

func newOccurrenceIndex(document *documentAnalysis) occurrenceIndex {
	entries := make([]occurrenceEntry, 0, len(document.document.Occurrences))
	for _, occurrence := range document.document.Occurrences {
		identity, err := ParseSymbol(occurrence.Symbol)
		if err != nil || !identity.Addressable() {
			continue
		}
		start, ok := document.source.Offset(int(occurrence.Range.StartLine), int(occurrence.Range.StartCharacter))
		if !ok {
			continue
		}
		end, ok := document.source.Offset(int(occurrence.Range.EndLine), int(occurrence.Range.EndCharacter))
		if !ok {
			continue
		}
		entries = append(entries, occurrenceEntry{
			start: start, end: end,
			line:   int(occurrence.Range.StartLine) + 1,
			column: int(occurrence.Range.StartCharacter),
			symbol: occurrence.Symbol, identity: identity,
		})
	}
	sort.Slice(entries, func(left, right int) bool { return entries[left].start < entries[right].start })
	return occurrenceIndex{entries: entries}
}

// within answers the last occurrence inside a span, which for `impl A for B`
// and for a bound list is the name the span was written for.
func (index occurrenceIndex) within(start, end int) (occurrenceEntry, bool) {
	found := occurrenceEntry{}
	ok := false
	for _, entry := range index.entries {
		if entry.start < start || entry.end > end {
			continue
		}
		found, ok = entry, true
	}
	return found, ok
}

// collectRelations derives the structural relations of one document and
// answers the occurrences they consumed, so the same token is not also
// published as a plain type reference.
func collectRelations(
	document *documentAnalysis,
	resolver *targetResolver,
	observed map[string]struct{},
) ([]Relation, map[int]struct{}) {
	index := newOccurrenceIndex(document)
	relations := make([]Relation, 0, 4)
	consumed := make(map[int]struct{})

	for _, header := range document.source.Implementations() {
		typeEntry, hasType := index.within(header.TypeStart, header.TypeEnd)
		if !hasType {
			continue
		}
		if header.TraitEnd == 0 {
			// An inherent implementation relates a type to nothing.
			continue
		}
		traitEntry, hasTrait := index.within(header.TraitStart, header.TraitEnd)
		if !hasTrait {
			continue
		}
		typeTarget := resolver.resolve(typeEntry.identity)
		traitTarget := resolver.resolve(traitEntry.identity)
		if typeTarget.key == "" || traitTarget.key == "" {
			continue
		}
		consumed[traitEntry.start] = struct{}{}
		consumed[typeEntry.start] = struct{}{}
		relations = append(relations, Relation{
			Kind:                RelationImplements,
			SourceKey:           typeTarget.key,
			TargetKey:           traitTarget.key,
			TargetRepository:    traitTarget.repository,
			TargetCrate:         traitTarget.crate,
			TargetQualifiedName: traitTarget.qualifiedName,
			File:                document.path,
			StartLine:           traitEntry.line,
			StartColumn:         traitEntry.column,
			StartOffset:         traitEntry.start,
			EndOffset:           traitEntry.end,
			Text:                document.source.Text(traitEntry.start, traitEntry.end),
		})
		relations = append(relations,
			overridesOfImplementation(document, index, resolver, observed, header, traitEntry)...)
	}

	for _, bound := range document.source.TraitBounds() {
		traitEntry, hasTrait := index.within(bound.TraitStart, bound.TraitEnd)
		superEntry, hasSuper := index.within(bound.NameStart, bound.NameEnd)
		if !hasTrait || !hasSuper {
			continue
		}
		source := resolver.resolve(traitEntry.identity)
		target := resolver.resolve(superEntry.identity)
		if source.key == "" || target.key == "" || source.key == target.key {
			continue
		}
		consumed[superEntry.start] = struct{}{}
		relations = append(relations, Relation{
			Kind:                RelationExtends,
			SourceKey:           source.key,
			TargetKey:           target.key,
			TargetRepository:    target.repository,
			TargetCrate:         target.crate,
			TargetQualifiedName: target.qualifiedName,
			File:                document.path,
			StartLine:           superEntry.line,
			StartColumn:         superEntry.column,
			StartOffset:         superEntry.start,
			EndOffset:           superEntry.end,
			Text:                document.source.Text(superEntry.start, superEntry.end),
		})
	}
	return relations, consumed
}

// overridesOfImplementation pairs every method of a trait implementation with
// the trait method it answers for.
//
// The trait method's symbol is composed, not guessed: SCIP identities are the
// trait's own descriptor path followed by the member's, so appending the
// member descriptor of `impl#[Type][Trait]name().` to the trait's symbol
// yields exactly what the trait's own document publishes. A composition the
// index never observed is dropped rather than published.
func overridesOfImplementation(
	document *documentAnalysis,
	index occurrenceIndex,
	resolver *targetResolver,
	observed map[string]struct{},
	header ImplementationHeader,
	traitEntry occurrenceEntry,
) []Relation {
	relations := make([]Relation, 0, 2)
	traitSymbol := strings.TrimSpace(traitEntry.symbol)
	if traitSymbol == "" {
		return relations
	}
	for _, entry := range index.entries {
		if entry.start < header.BodyStart || entry.end > header.BodyEnd {
			continue
		}
		definition, defined := resolver.definitions[entry.symbol]
		if !defined || definition.File != document.path {
			continue
		}
		if implementedTrait(entry.identity) == "" {
			continue
		}
		member := entry.identity.Descriptors[len(entry.identity.Descriptors)-1]
		composed := traitSymbol + renderDescriptor(member)
		if _, exists := observed[composed]; !exists {
			continue
		}
		identity, err := ParseSymbol(composed)
		if err != nil || !identity.Addressable() {
			continue
		}
		target := resolver.resolve(identity)
		if target.key == "" || target.key == string(definition.StableKey) {
			continue
		}
		relations = append(relations, Relation{
			Kind:                RelationOverrides,
			SourceKey:           string(definition.StableKey),
			TargetKey:           target.key,
			TargetRepository:    target.repository,
			TargetCrate:         target.crate,
			TargetQualifiedName: target.qualifiedName,
			File:                document.path,
			StartLine:           entry.line,
			StartColumn:         entry.column,
			StartOffset:         entry.start,
			EndOffset:           entry.end,
			Text:                document.source.Text(entry.start, entry.end),
		})
	}
	return relations
}

// renderDescriptor writes one descriptor back in the SCIP grammar, which is
// what makes a composed symbol byte identical to the one the provider emits.
func renderDescriptor(descriptor Descriptor) string {
	name := descriptor.Name
	if needsDescriptorEscape(name) {
		name = "`" + strings.ReplaceAll(name, "`", "``") + "`"
	}
	switch descriptor.Suffix {
	case SuffixNamespace:
		return name + "/"
	case SuffixType:
		return name + "#"
	case SuffixTerm:
		return name + "."
	case SuffixMethod:
		return name + "(" + descriptor.Disambiguator + ")."
	case SuffixTypeParameter:
		return "[" + name + "]"
	case SuffixParameter:
		return "(" + name + ")"
	case SuffixMeta:
		return name + ":"
	case SuffixMacro:
		return name + "!"
	default:
		return ""
	}
}

func needsDescriptorEscape(name string) bool {
	return strings.ContainsAny(name, " `/#.()[]:!")
}

// observedSymbolStrings answers every symbol the index mentions, defined or
// merely referenced. A composed identity is only trusted when it appears here:
// composing one the analyzer never saw would be inventing a declaration.
func observedSymbolStrings(index scipwire.Index) map[string]struct{} {
	symbols := make(map[string]struct{})
	for _, document := range index.Documents {
		for _, symbol := range document.Symbols {
			symbols[symbol.Symbol] = struct{}{}
		}
		for _, occurrence := range document.Occurrences {
			symbols[occurrence.Symbol] = struct{}{}
		}
	}
	for _, symbol := range index.ExternalSymbols {
		symbols[symbol.Symbol] = struct{}{}
	}
	return symbols
}

package scip

import (
	"strings"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
)

// hierarchyReferences turns SCIP relationships into IMPLEMENTS, EXTENDS and
// OVERRIDES.
//
// A relationship is the only place SCIP states a type hierarchy, and it is the
// only fact in the format that carries no position: it is a property of the
// declaration, so the evidence is the declaration's own name range.
//
// Two things make this harder than reading a flag.
//
// **The relation is declared from both ends.** scip-java writes
// `Models#name() -> Person#name()` on the abstract method and
// `Person#name() -> Models#name()` on the implementation, both with
// `is_implementation`. They are one relation, and the graph wants it pointing
// up: the overriding method overrides the overridden one. So the pair is
// deduplicated and oriented, never emitted twice.
//
// **Orientation cannot come from the flags**, which are identical on both
// rows. It comes from the type hierarchy, which is unambiguous: scip-java
// writes a type's relationship only once, from the subtype. So the supertype
// graph is built first, and a method relation points from the method whose
// owner is lower.
func hierarchyReferences(
	declarations []declaration,
	path string,
	offsets *offsetTable,
	declared map[string]bool,
	information map[string]scipwire.SymbolInformation,
) []facts.SemanticReference {
	supertypes := supertypeGraph(declarations)

	var references []facts.SemanticReference
	emitted := map[string]bool{}
	for _, entry := range declarations {
		info, present := information[entry.symbol]
		if !present {
			continue
		}
		for _, relationship := range info.Relationships {
			if !relationship.IsImplementation || relationship.Symbol == entry.symbol {
				continue
			}
			// A target this graph does not declare produces nothing. An enum
			// implements java.lang.Enum and Comparable without a word of it
			// appearing in the source, so an unresolved row at the enum's
			// name would claim the author wrote something they did not.
			if !declared[relationship.Symbol] {
				continue
			}
			source, target, ok := orient(entry.symbol, relationship.Symbol, supertypes)
			if !ok || source != entry.symbol {
				// The other end of the pair carries this relation, or the two
				// are unrelated types and nothing here can say which way it
				// goes. Either way this row is not the one to publish.
				continue
			}
			pair := source + "\x00" + target
			if emitted[pair] {
				continue
			}
			emitted[pair] = true

			kind := hierarchyKind(source, target, information)
			start := offsets.position(entry.selection.StartLine, entry.selection.StartCharacter)
			end := offsets.position(entry.selection.EndLine, entry.selection.EndCharacter)
			references = append(references, facts.SemanticReference{
				File:        path,
				SourceID:    source,
				TargetID:    target,
				Kind:        string(kind),
				StartLine:   int(entry.selection.StartLine),
				StartColumn: int(entry.selection.StartCharacter),
				Start:       start,
				EndLine:     int(entry.selection.EndLine),
				EndColumn:   int(entry.selection.EndCharacter),
				End:         end,
			})
		}
	}
	return references
}

// supertypeGraph maps a type onto the types it declares itself below.
//
// It reads only type declarations, and it is reliable because scip-java writes
// a type's `is_implementation` relationship once, from the subtype. A member's
// relationship is written twice and cannot be read this way.
func supertypeGraph(declarations []declaration) map[string][]string {
	graph := map[string][]string{}
	for _, entry := range declarations {
		if !isTypeSymbol(entry.symbol) {
			continue
		}
		for _, relationship := range entry.info.Relationships {
			if relationship.IsImplementation && isTypeSymbol(relationship.Symbol) {
				graph[entry.symbol] = append(graph[entry.symbol], relationship.Symbol)
			}
		}
	}
	return graph
}

// orient decides which end of a relation is the source.
//
// For two types the answer is the graph itself. For two members it is the
// owners: a member of a subtype overrides a member of its supertype, never the
// other way. When neither owner reaches the other -- a supertype outside this
// index, so the graph has no path -- the fallback is the producer's own kind:
// an abstract declaration is what gets overridden, not what overrides.
func orient(left, right string, supertypes map[string][]string) (string, string, bool) {
	if isTypeSymbol(left) && isTypeSymbol(right) {
		switch {
		case reaches(supertypes, left, right):
			return left, right, true
		case reaches(supertypes, right, left):
			return right, left, true
		default:
			return "", "", false
		}
	}
	leftOwner, rightOwner := ownerType(left), ownerType(right)
	if leftOwner != "" && rightOwner != "" && leftOwner != rightOwner {
		switch {
		case reaches(supertypes, leftOwner, rightOwner):
			return left, right, true
		case reaches(supertypes, rightOwner, leftOwner):
			return right, left, true
		}
	}
	return "", "", false
}

// reaches reports whether `from` is at or below `to` in the supertype graph.
func reaches(supertypes map[string][]string, from, to string) bool {
	if from == to {
		return false
	}
	seen := map[string]bool{from: true}
	queue := append([]string(nil), supertypes[from]...)
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current == to {
			return true
		}
		if seen[current] {
			continue
		}
		seen[current] = true
		queue = append(queue, supertypes[current]...)
	}
	return false
}

// hierarchyKind names the relation the way the canonical model does.
//
// EXTENDS and IMPLEMENTS differ only by what the target is, which the producer
// states: an interface is implemented and a class is extended. A target whose
// kind the producer did not classify falls to EXTENDS, which is the weaker
// claim -- it says the two are related in the hierarchy without asserting an
// interface contract.
func hierarchyKind(source, target string, information map[string]scipwire.SymbolInformation) facts.EdgeKind {
	if !isTypeSymbol(source) || !isTypeSymbol(target) {
		return facts.Overrides
	}
	if information[target].Kind == scipKindInterface {
		return facts.Implements
	}
	return facts.Extends
}

// scipKindInterface is SymbolInformation.Kind for an interface, read from
// scip-java rather than from the schema. See scipKindNames.
const scipKindInterface int32 = 21

// isTypeSymbol reports whether a SCIP symbol names a type, which its last
// descriptor says: a type descriptor ends in `#`.
func isTypeSymbol(symbol string) bool {
	identity, err := parseSymbol(symbol)
	if err != nil || identity.local {
		return false
	}
	return strings.HasSuffix(strings.TrimSpace(identity.descriptors), "#")
}

// ownerType is the type a member belongs to: the symbol with its last
// descriptor removed. A member declared directly in a package has none.
func ownerType(symbol string) string {
	identity, err := parseSymbol(symbol)
	if err != nil || identity.local {
		return ""
	}
	segments := splitDescriptors(strings.TrimSpace(identity.descriptors))
	if len(segments) < 2 {
		return ""
	}
	owner := strings.Join(segments[:len(segments)-1], "")
	if !strings.HasSuffix(owner, "#") {
		return ""
	}
	return strings.TrimSuffix(symbol, identity.descriptors) + owner
}

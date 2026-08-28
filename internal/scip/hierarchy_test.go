package scip

import (
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
)

// hierarchyEdges is every relation the payload publishes that is not a plain
// reference, keyed by the pair it connects.
func hierarchyEdges(t *testing.T, payload facts.SemanticPayload) map[string]string {
	t.Helper()
	edges := map[string]string{}
	for _, reference := range payload.References {
		if reference.Kind == "" || reference.Kind == string(facts.References) {
			continue
		}
		key := lastDescriptor(reference.SourceID) + " -> " + lastDescriptor(reference.TargetID)
		if previous, clash := edges[key]; clash {
			t.Errorf("%s published twice: %q and %q", key, previous, reference.Kind)
		}
		edges[key] = reference.Kind
	}
	return edges
}

func lastDescriptor(symbol string) string {
	identity, err := parseSymbol(symbol)
	if err != nil {
		return symbol
	}
	return identity.qualifiedName()
}

func TestHierarchyPublishesImplementsExtendsAndOverrides(t *testing.T) {
	edges := hierarchyEdges(t, convertCoverage(t))
	for pair, want := range map[string]string{
		"com.example.coverage.Shapes.Base -> com.example.coverage.Shapes":                  "IMPLEMENTS",
		"com.example.coverage.Shapes.Circle -> com.example.coverage.Shapes.Base":           "EXTENDS",
		"com.example.coverage.Shapes.Circle -> com.example.coverage.Shapes":                "IMPLEMENTS",
		"com.example.coverage.Shapes.Circle.kind -> com.example.coverage.Shapes.Base.kind": "OVERRIDES",
		"com.example.coverage.Shapes.Circle.area -> com.example.coverage.Shapes.area":      "OVERRIDES",
	} {
		got, present := edges[pair]
		if !present {
			t.Errorf("%s is missing", pair)
			continue
		}
		if got != want {
			t.Errorf("%s is %s, want %s", pair, got, want)
		}
	}
}

// TestHierarchyIsNeverPublishedInBothDirections is the defect this orientation
// exists to prevent. scip-java writes a member relation from both ends with
// identical flags, so reading the flag alone yields A overrides B and B
// overrides A -- a cycle in the graph, and an answer that says a base class
// overrides its own subclass.
func TestHierarchyIsNeverPublishedInBothDirections(t *testing.T) {
	edges := hierarchyEdges(t, convertCoverage(t))
	for pair := range edges {
		parts := strings.SplitN(pair, " -> ", 2)
		reversed := parts[1] + " -> " + parts[0]
		if _, present := edges[reversed]; present {
			t.Errorf("%q and its reverse are both published", pair)
		}
	}
}

// TestHierarchyOrientsFromTheSubtype checks the direction rather than the
// presence: an override points from the overriding member up to the one it
// overrides, which is the direction a caller asking "what breaks if I change
// this interface" has to follow.
func TestHierarchyOrientsFromTheSubtype(t *testing.T) {
	edges := hierarchyEdges(t, convertCoverage(t))
	if _, wrong := edges["com.example.coverage.Shapes.area -> com.example.coverage.Shapes.Circle.area"]; wrong {
		t.Error("the abstract declaration was published as overriding its implementation")
	}
	if _, wrong := edges["com.example.coverage.Shapes -> com.example.coverage.Shapes.Base"]; wrong {
		t.Error("the interface was published as implementing its implementor")
	}
}

// TestHierarchySkipsTargetsTheGraphDoesNotHold is the honesty half. An enum
// implements java.lang.Enum, Comparable, Constable and Serializable without a
// word of it appearing in the source; the JDK is not in this graph, and an
// unresolved row anchored at the enum's name would claim the author wrote
// something they did not.
func TestHierarchySkipsTargetsTheGraphDoesNotHold(t *testing.T) {
	payload := convertCoverage(t)
	for _, reference := range payload.References {
		if strings.Contains(reference.TargetID, " jdk ") {
			t.Errorf("a hierarchy edge points at the JDK: %+v", reference)
		}
	}
	for _, unresolved := range payload.Unresolved {
		for _, invented := range []string{"java/lang/Enum#", "java/lang/constant/Constable#"} {
			if strings.Contains(unresolved.Detail, invented) {
				t.Errorf("an implicit supertype was reported as an unresolved reference: %s", invented)
			}
		}
	}
}

func TestHierarchyEdgesSurviveTheNormalizer(t *testing.T) {
	set, err := facts.NormalizeSemantic(t.Context(), coverageRepository(), convertCoverage(t))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	counts := map[facts.EdgeKind]int{}
	for _, edge := range set.Edges {
		counts[edge.Kind]++
		switch edge.Kind {
		case facts.Implements, facts.Extends, facts.Overrides:
			// A hierarchy relation resolved by javac is type-checked, and its
			// evidence is the declaration that states it.
			if edge.Confidence != facts.ExactTypechecked {
				t.Errorf("%s is %s, want EXACT_TYPECHECKED", edge.Kind, edge.Confidence)
			}
			if edge.EvidenceKey == "" {
				t.Errorf("%s carries no evidence", edge.Kind)
			}
		}
	}
	for _, kind := range []facts.EdgeKind{facts.Implements, facts.Extends, facts.Overrides} {
		if counts[kind] == 0 {
			t.Errorf("the graph holds no %s edge", kind)
		}
	}
}

func TestOwnerTypeAndTypeSymbol(t *testing.T) {
	const prefix = "semanticdb maven p 1 "
	for symbol, want := range map[string]string{
		prefix + "com/example/Shapes#Circle#area().": prefix + "com/example/Shapes#Circle#",
		prefix + "com/example/Shapes#area().":        prefix + "com/example/Shapes#",
		prefix + "com/example/Shapes#":               "",
	} {
		if got := ownerType(symbol); got != want {
			t.Errorf("ownerType(%q) = %q, want %q", symbol, got, want)
		}
	}
	if !isTypeSymbol(prefix + "com/example/Shapes#") {
		t.Error("a type descriptor is not recognised as a type")
	}
	if isTypeSymbol(prefix + "com/example/Shapes#area().") {
		t.Error("a method descriptor is recognised as a type")
	}
	if isTypeSymbol("local 3") {
		t.Error("a local is recognised as a type")
	}
}

func TestReachesFollowsTheSupertypeChain(t *testing.T) {
	graph := map[string][]string{"C": {"B"}, "B": {"A"}}
	if !reaches(graph, "C", "A") {
		t.Error("a transitive supertype is not reachable")
	}
	if reaches(graph, "A", "C") {
		t.Error("the supertype graph is walked downwards")
	}
	if reaches(graph, "A", "A") {
		t.Error("a type reaches itself")
	}
	// A cycle must not hang the walk: a malformed index is a bad index, not a
	// reason for the pass to stop responding.
	cyclic := map[string][]string{"A": {"B"}, "B": {"A"}}
	if reaches(cyclic, "A", "Z") {
		t.Error("a cycle produced a false reachability")
	}
}

package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
)

// TestTraceWitnessAnswersWithTheRouteInOrder is the contract of `to`: the rows
// are the hops, in the order the route crosses them, each one naming the symbol
// it came from and the edge it crossed. The fixture chain is
// root.Root -> root.Level1 -> root.Level2 -> other.Level3, so a route to the
// last one is three hops and crosses a repository boundary.
func TestTraceWitnessAnswersWithTheRouteInOrder(t *testing.T) {
	store := traceDependenciesStore(t, 71)

	_, response, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
		StableKey: "sym-root", To: "Level3",
	}, store)
	if err != nil {
		t.Fatalf("traceDependencies() error = %v", err)
	}
	if response.Total != 3 || response.Returned != 3 || response.Truncated || response.NextCursor != nil {
		t.Fatalf("envelope = %#v, want three unpaged rows", response)
	}
	trace := response.Results
	if trace.WitnessTo != "other.Level3" || trace.WitnessHops != 3 {
		t.Fatalf("witness = %#v, want the resolved target and its hop count", trace)
	}
	wantNames := []string{"root.Level1", "root.Level2", "other.Level3"}
	for index, node := range trace.Nodes {
		if node.QualifiedName != wantNames[index] || node.Depth != index+1 {
			t.Fatalf("hop %d = %#v, want %q at depth %d", index, node, wantNames[index], index+1)
		}
	}
	if first := trace.Nodes[0]; first.ReachedFrom != "root.Root" || first.ViaKind != string(facts.CallsDirect) {
		t.Fatalf("first hop = %#v, want the call from the root", first)
	}
	if last := trace.Nodes[2]; last.ReachedFrom != "root.Level2" || last.Repository != "other" {
		t.Fatalf("last hop = %#v, want the import from level two", last)
	}
	// The route is the same edges the fan-out page reports, so it counts the
	// same coverage: guessing a route would be a fourth category.
	if response.Coverage != (Coverage{Exact: 2, Candidate: 1}) {
		t.Fatalf("coverage = %#v, want the two exact hops and the candidate one", response.Coverage)
	}
	// The count does not mislead when a route was found, so nothing is said.
	if response.Guidance != "" {
		t.Fatalf("guidance = %q, want silence on a found route", response.Guidance)
	}
	// A route is spelled row per fact whatever the default is: grouping rows by
	// file would lose the order, and the order is the answer.
	if trace.View != ViewFull {
		t.Fatalf("view = %q, want the full shape", trace.View)
	}
}

// TestTraceWitnessNamesTheBoundItStoppedAt keeps a bounded search from reading
// as an absence. The chain is three hops long, so a depth of one holds no
// route, and the answer must say which bound produced the emptiness.
func TestTraceWitnessNamesTheBoundItStoppedAt(t *testing.T) {
	store := traceDependenciesStore(t, 72)

	_, response, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
		StableKey: "sym-root", To: "Level3", Depth: 1,
	}, store)
	if err != nil {
		t.Fatalf("traceDependencies() error = %v", err)
	}
	if response.Total != 0 || len(response.Results.Nodes) != 0 {
		t.Fatalf("rows = %#v, want none within the bound", response.Results.Nodes)
	}
	if response.Results.WitnessTo != "other.Level3" || response.Results.WitnessHops != 0 {
		t.Fatalf("witness = %#v, want the target named and no hops", response.Results)
	}
	if !strings.Contains(response.Guidance, "bound and not an absence") {
		t.Fatalf("guidance = %q, want the bound named rather than an absence", response.Guidance)
	}

	// The budget is the other bound, and it is the one a caller cannot infer
	// from the request: max_nodes of one discovers the seed and nothing else.
	_, response, err = traceDependencies(context.Background(), nil, TraceDependenciesInput{
		StableKey: "sym-root", To: "Level3", MaxNodes: 1,
	}, store)
	if err != nil {
		t.Fatalf("traceDependencies() error = %v", err)
	}
	if response.Total != 0 || !response.Results.TraversalTruncated {
		t.Fatalf("budget-bounded = %#v, want no rows and truncation reported", response.Results)
	}
	if !strings.Contains(response.Guidance, "node budget") {
		t.Fatalf("guidance = %q, want the budget named", response.Guidance)
	}
}

// TestTraceWitnessRefusesWhatWouldHoleTheRoute pins the refusals. Each of these
// would answer a different question under the same name: the row filters can
// drop a link the route needs, paging can spell half of it, and the compact
// view groups the rows by file and loses the order.
func TestTraceWitnessRefusesWhatWouldHoleTheRoute(t *testing.T) {
	store := traceDependenciesStore(t, 73)

	for name, arguments := range map[string]TraceDependenciesInput{
		"repo":            {StableKey: "sym-root", To: "Level3", Repo: "root"},
		"language":        {StableKey: "sym-root", To: "Level3", Language: "go"},
		"include_derived": {StableKey: "sym-root", To: "Level3", IncludeDerived: true},
		"limit":           {StableKey: "sym-root", To: "Level3", Limit: 2},
		"cursor":          {StableKey: "sym-root", To: "Level3", Cursor: "whatever"},
		"view":            {StableKey: "sym-root", To: "Level3", View: ViewCompact},
		"to_path alone":   {StableKey: "sym-root", ToPath: "root.go"},
	} {
		if _, _, err := traceDependencies(context.Background(), nil, arguments, store); err == nil {
			t.Fatalf("%s combined with to was accepted, want a refusal", name)
		}
	}

	// A target nobody declares is not an empty route: the request itself was
	// unanswerable, and saying "no route" would blame the graph for a typo.
	if _, _, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
		StableKey: "sym-root", To: "LevelNine",
	}, store); err == nil {
		t.Fatal("an undeclared target was accepted, want a refusal")
	}
}

// TestTraceWitnessLeavesTheFanOutPageAlone is the regression guard for the
// branch: without `to`, every field the witness added is absent from the wire
// and the page is the one that shipped.
func TestTraceWitnessLeavesTheFanOutPageAlone(t *testing.T) {
	store := traceDependenciesStore(t, 74)

	_, response, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
		StableKey: "sym-root",
	}, store)
	if err != nil {
		t.Fatalf("traceDependencies() error = %v", err)
	}
	if response.Results.WitnessTo != "" || response.Results.WitnessHops != 0 {
		t.Fatalf("fan-out trace = %#v, want the witness fields empty", response.Results)
	}
	if response.Results.View != ViewCompact {
		t.Fatalf("view = %q, want the compact default untouched", response.Results.View)
	}
}

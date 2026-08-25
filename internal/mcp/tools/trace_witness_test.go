package tools

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
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

// TestTraceWitnessClassifiesATimeout keeps a deadline from reading as an
// absence. The client's own context bounds the walk, so a request that ran out
// of time has to say so rather than answer "no route".
func TestTraceWitnessClassifiesATimeout(t *testing.T) {
	store := traceDependenciesStore(t, 81)
	expired, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, _, err := traceDependencies(expired, nil, TraceDependenciesInput{
		StableKey: "sym-root", To: "Level3",
	}, store)
	if err == nil {
		t.Fatal("an expired deadline was answered, want a timeout")
	}
	if strings.Contains(strings.ToLower(err.Error()), "no route") {
		t.Fatalf("a timeout was reported as an absence: %v", err)
	}
}

// TestWitnessGuidanceNamesEachBound is a direct unit test of the sentence,
// because the third case needs a graph whose resolver recorded a failure that
// bounds this very question -- a fixture that would prove less than the table
// below states.
func TestWitnessGuidanceNamesEachBound(t *testing.T) {
	if guidance := witnessGuidance(2, 3, false, VerdictComplete); guidance != "" {
		t.Errorf("a found route produced %q, want silence", guidance)
	}
	if guidance := witnessGuidance(0, 3, true, VerdictComplete); !strings.Contains(guidance, "node budget") {
		t.Errorf("a truncated search produced %q, want the budget named", guidance)
	}
	if guidance := witnessGuidance(0, 3, false, VerdictLowerBound); !strings.Contains(guidance, "blind_spots") {
		t.Errorf("a lower-bound answer produced %q, want the blind spots named", guidance)
	}
	if guidance := witnessGuidance(0, 3, false, VerdictComplete); !strings.Contains(guidance, "bound and not an absence") {
		t.Errorf("a depth-bounded answer produced %q, want the bound named", guidance)
	}
	// The budget outranks the blind spots: a search that never finished cannot
	// claim anything about what it did not read.
	if guidance := witnessGuidance(0, 3, true, VerdictLowerBound); !strings.Contains(guidance, "node budget") {
		t.Errorf("a truncated lower-bound answer produced %q, want the budget first", guidance)
	}
}

// corruptWitnessSnapshot builds a two-symbol route by hand so one field on the
// far end can carry a value no writer produces. The route has to be walkable --
// the target is found by name -- and only the spelling is broken, which is the
// shape a truncated or edited snapshot file actually has.
func corruptWitnessSnapshot(t *testing.T, reachedPath hotsnapshot.InternedString, unresolvedName hotsnapshot.InternedString) *hotsnapshot.GraphSnapshot {
	t.Helper()
	interner := hotsnapshot.NewStringInterner()
	intern := func(value string) hotsnapshot.InternedString {
		id, err := interner.Intern(value)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	repositoryName, path, language := intern("repo"), intern("internal/pkg/route.go"), intern("go")
	secondPath := intern("internal/pkg/reached.go")
	rootName, reachedName := intern("Root"), intern("Reached")
	rootQualified, reachedQualified := intern("pkg.Root"), intern("pkg.Reached")
	validKind := intern("func")
	repositoryKey, packageKey, fileKey := intern("repo"), intern("pkg"), intern("file")
	reachedFileKey := intern("file-reached")
	evidenceKind, provenance := intern("call"), intern("GoTypesUse")
	table := interner.Freeze()
	if reachedPath == 0 {
		reachedPath = secondPath
	}
	stableKeys, err := hotsnapshot.NewStableKeyTable([]hotsnapshot.StableKey{"sym-a-root", "sym-b-reached"})
	if err != nil {
		t.Fatal(err)
	}
	input := hotsnapshot.GraphSnapshotInput{
		ID: 1, CreatedAt: time.Unix(1, 0).UTC(), Version: 1, SchemaVersion: 4, ResolverVersion: "test",
		Strings:      table,
		Repositories: []hotsnapshot.RepositoryRecord{{Key: repositoryKey, Name: repositoryName, Path: path, Languages: language}},
		Packages:     []hotsnapshot.PackageRecord{{Key: packageKey, Repository: 0, Language: language, Name: repositoryName, ModulePath: repositoryName}},
		Files: []hotsnapshot.FileRecord{
			{Key: fileKey, Repository: 0, Package: 0, Path: path, Language: language},
			{Key: reachedFileKey, Repository: 0, Package: 0, Path: reachedPath, Language: language},
		},
		Symbols: []hotsnapshot.SymbolRecord{
			{StableKey: 0, CanonicalIdentity: rootQualified, File: 0, Language: language, Name: rootName, QualifiedName: rootQualified, Kind: validKind, StartLine: 1, EndLine: 2},
			{StableKey: 1, CanonicalIdentity: reachedQualified, File: 1, Language: language, Name: reachedName, QualifiedName: reachedQualified, Kind: validKind, StartLine: 4, EndLine: 5},
		},
		Evidence:       []hotsnapshot.EvidenceRecord{{Key: evidenceKind, SourceFile: 0, TargetFile: 0, Kind: evidenceKind, Provenance: provenance}},
		ForwardOffsets: []uint32{0, 1, 1},
		ForwardEdges: []hotsnapshot.PackedEdge{{
			Target: 1, Evidence: 0, Kind: facts.CodeCallsDirect,
			Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse,
		}},
		ReverseOffsets: []uint32{0, 0, 1},
		ReverseEdges: []hotsnapshot.PackedEdge{{
			Target: 0, Evidence: 0, Kind: facts.CodeCallsDirect,
			Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse,
		}},
		StableKeys: stableKeys,
	}
	if unresolvedName != 0 {
		input.Unresolved = []hotsnapshot.UnresolvedReferenceRecord{{
			Key: unresolvedName, Repository: 0, File: 0, Source: 0, Language: language,
			RequestedPackage: unresolvedName, RequestedSymbol: unresolvedName,
			Reason: unresolvedName, Detail: unresolvedName, StartLine: 1,
		}}
	}
	snapshot, buildErr := hotsnapshot.NewGraphSnapshot(input)
	if buildErr != nil {
		t.Skipf("the constructor rejects this input, so the tool cannot receive it: %v", buildErr)
	}
	return snapshot
}

// TestTraceWitnessRefusesASnapshotItCannotSpell covers the two guards between a
// walked route and its answer: the rows it would return, and the blind spots it
// would report. Either one half-spelled would put a caller in a file the graph
// never named, so both refuse instead.
func TestTraceWitnessRefusesASnapshotItCannotSpell(t *testing.T) {
	const pastTheTable = hotsnapshot.InternedString(9_999)
	for name, snapshot := range map[string]*hotsnapshot.GraphSnapshot{
		"invalid path on the reached symbol": corruptWitnessSnapshot(t, pastTheTable, 0),
		"invalid unresolved reference":       corruptWitnessSnapshot(t, 0, pastTheTable),
	} {
		_, _, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
			StableKey: "sym-a-root", To: "Reached",
		}, hotsnapshot.NewSnapshotStore(snapshot))
		if err == nil {
			t.Errorf("%s: a snapshot it cannot spell was answered", name)
			continue
		}
		if !strings.Contains(err.Error(), "invalid") {
			t.Errorf("%s: message %q does not say the snapshot is at fault", name, err.Error())
		}
	}
}

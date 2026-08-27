package tools

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// TestGetBlastRadiusListsAffectedSymbolsAndGroupsThem checks the direction and
// the aggregation contract at once: the traversal walks incoming edges, the
// root is never counted as affected by its own change, the affected symbols are
// listed, and by_repository partitions them.
func TestGetBlastRadiusListsAffectedSymbolsAndGroupsThem(t *testing.T) {
	store := blastRadiusStore(t, 41)

	_, response, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core"}, store)
	if err != nil {
		t.Fatalf("getBlastRadius() error = %v", err)
	}
	radius := response.Results
	if radius.RootKey != "sym-core" || radius.RootRepository != "core" {
		t.Fatalf("root = %#v", radius)
	}
	if radius.Affected != 3 || radius.DeepestDepth != 2 || radius.TraversalTruncated {
		t.Fatalf("impact metadata = %#v, want three affected symbols across two depths", radius)
	}
	if response.Total != 3 || response.Returned != 3 || response.Truncated {
		t.Fatalf("pagination = %#v, want the three affected symbols", response)
	}
	wantSymbols := []string{"app.Direct", "core.Loose", "app.Indirect"}
	for index, symbol := range radius.Symbols {
		if symbol.QualifiedName != wantSymbols[index] {
			t.Fatalf("symbols = %#v, want %v", radius.Symbols, wantSymbols)
		}
	}
	// The traversal runs incoming, so the symbol a consumer was reached from is
	// the target of its own edge.
	if direct := radius.Symbols[0]; direct.ReachedFrom != "core.Core" || direct.Depth != 1 {
		t.Fatalf("direct consumer = %#v", direct)
	}
	if indirect := radius.Symbols[2]; indirect.ReachedFrom != "app.Direct" || indirect.Depth != 2 {
		t.Fatalf("indirect consumer = %#v", indirect)
	}
	for index, symbol := range radius.Symbols {
		if symbol.FilePath == "" || symbol.EndLine < symbol.StartLine {
			t.Fatalf("symbol %d = %#v, want a file path and a declaration range", index, symbol)
		}
		if symbol.FileKey != "" || symbol.ReachedFromKey != "" {
			t.Fatalf("symbol %d = %#v, want derived identifiers withheld from the concise format", index, symbol)
		}
	}
	wantRepositories := []BlastRadiusGroup{{Key: "app", Count: 2}, {Key: "core", Count: 1}}
	if len(radius.ByRepository) != 2 || radius.ByRepository[0] != wantRepositories[0] || radius.ByRepository[1] != wantRepositories[1] {
		t.Fatalf("by_repository = %#v, want %#v", radius.ByRepository, wantRepositories)
	}
	wantDepths := []BlastRadiusDepthGroup{{Depth: 1, Count: 2}, {Depth: 2, Count: 1}}
	if len(radius.ByDepth) != 2 || radius.ByDepth[0] != wantDepths[0] || radius.ByDepth[1] != wantDepths[1] {
		t.Fatalf("by_depth = %#v, want %#v", radius.ByDepth, wantDepths)
	}
	if total := blastRadiusGroupTotal(radius); total != radius.Affected {
		t.Fatalf("by_repository accounts for %d symbols, want %d", total, radius.Affected)
	}
	if response.Coverage != (Coverage{Exact: 2, Candidate: 1}) {
		t.Fatalf("coverage = %#v", response.Coverage)
	}
}

// TestGetBlastRadiusCountsEveryRelationKindReachingTheSubgraph is the reason
// by_kind is not read off the discovering edge: sym-direct both calls and uses
// the type of sym-core, and a reviewer needs to see both reasons.
func TestGetBlastRadiusCountsEveryRelationKindReachingTheSubgraph(t *testing.T) {
	store := blastRadiusStore(t, 45)

	_, response, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core"}, store)
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []BlastRadiusGroup{
		{Key: string(facts.CallsDirect), Count: 2},
		{Key: string(facts.References), Count: 1},
		{Key: string(facts.TypeUses), Count: 1},
	}
	radius := response.Results
	if len(radius.ByKind) != len(wantKinds) {
		t.Fatalf("by_kind = %#v, want %#v", radius.ByKind, wantKinds)
	}
	for index := range wantKinds {
		if radius.ByKind[index] != wantKinds[index] {
			t.Fatalf("by_kind = %#v, want %#v", radius.ByKind, wantKinds)
		}
	}
	// sym-direct contributes to two kinds but is still one affected symbol.
	if radius.Affected != 3 {
		t.Fatalf("affected = %d, want 3 despite four kind memberships", radius.Affected)
	}
	// A confidence gate applies to the kind axis too, not only to reachability.
	_, exactOnly, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", Confidence: string(facts.ExactTypechecked),
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	for _, group := range exactOnly.Results.ByKind {
		if group.Key == string(facts.References) {
			t.Fatalf("exact-only by_kind = %#v, want the candidate relation excluded", exactOnly.Results.ByKind)
		}
	}
}

// TestGetBlastRadiusPagesSymbolsAndKeepsAxesComplete fixes what the envelope
// pages over: the affected symbols. Aggregates cover the whole traversal, so
// they do not shrink page by page.
func TestGetBlastRadiusPagesSymbolsAndKeepsAxesComplete(t *testing.T) {
	store := blastRadiusStore(t, 42)

	_, first, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core", Limit: 2}, store)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 3 || first.Returned != 2 || !first.Truncated || first.NextCursor == nil {
		t.Fatalf("first page = %#v, want two of three affected symbols", first)
	}
	if len(first.Results.ByRepository) != 2 || len(first.Results.ByDepth) != 2 ||
		len(first.Results.ByPackage) != 2 || first.Results.Affected != 3 {
		t.Fatalf("aggregates on first page = %#v, want the complete traversal", first.Results)
	}

	_, second, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", Limit: 2, Cursor: *first.NextCursor,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if second.Returned != 1 || second.Truncated || second.NextCursor != nil {
		t.Fatalf("second page = %#v, want the final affected symbol", second)
	}
	if second.Results.Symbols[0].QualifiedName != "app.Indirect" || second.Results.Affected != 3 {
		t.Fatalf("second page = %#v", second.Results)
	}
	if len(second.Results.ByPackage) != 2 {
		t.Fatalf("by_package on second page = %#v, want the complete aggregate", second.Results.ByPackage)
	}
}

// TestGetBlastRadiusBoundsReachability keeps depth, confidence and max_nodes
// honest: each one changes what is reachable, not only what is displayed.
func TestGetBlastRadiusBoundsReachability(t *testing.T) {
	store := blastRadiusStore(t, 43)

	_, shallow, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core", Depth: 1}, store)
	if err != nil {
		t.Fatal(err)
	}
	if shallow.Results.Affected != 2 || shallow.Results.DeepestDepth != 1 {
		t.Fatalf("depth-1 impact = %#v, want only direct consumers", shallow.Results)
	}

	_, exactOnly, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", Confidence: string(facts.ExactTypechecked),
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if exactOnly.Results.Affected != 2 {
		t.Fatalf("exact-only impact = %#v, want the candidate consumer excluded with its subtree", exactOnly.Results)
	}

	_, bounded, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core", MaxNodes: 2}, store)
	if err != nil {
		t.Fatal(err)
	}
	if !bounded.Results.TraversalTruncated || bounded.Results.Affected != 1 {
		t.Fatalf("node-bounded impact = %#v, want one affected symbol and a truncated traversal", bounded.Results)
	}
}

func TestGetBlastRadiusClassifiesFailures(t *testing.T) {
	store := blastRadiusStore(t, 44)
	expired, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()

	cases := []struct {
		name      string
		ctx       context.Context
		arguments GetBlastRadiusInput
		store     *hotsnapshot.SnapshotStore
		wantCode  string
	}{
		{name: "empty key", ctx: context.Background(), arguments: GetBlastRadiusInput{}, store: store, wantCode: CodeInvalidArgument},
		{name: "depth above maximum", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core", Depth: MaximumBlastRadiusDepth + 1}, store: store, wantCode: CodeInvalidArgument},
		{name: "max_nodes above maximum", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core", MaxNodes: MaximumBlastRadiusMaxNodes + 1}, store: store, wantCode: CodeInvalidArgument},
		{name: "limit above maximum", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core", Limit: MaximumBlastRadiusLimit + 1}, store: store, wantCode: CodeInvalidArgument},
		{name: "unsupported edge kind", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core", EdgeKinds: []string{"CONTAINS_FILE"}}, store: store, wantCode: CodeInvalidArgument},
		{name: "unsupported symbol kind", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core", Kinds: []string{"callable"}}, store: store, wantCode: CodeInvalidArgument},
		{name: "unsupported view", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core", View: "files"}, store: store, wantCode: CodeInvalidArgument},
		{name: "missing symbol", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-missing"}, store: store, wantCode: CodeSymbolNotFound},
		{name: "invalid cursor", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core", Cursor: "not-a-cursor"}, store: store, wantCode: CodeCursorInvalid},
		{name: "unpublished snapshot", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core"}, store: hotsnapshot.NewSnapshotStore(nil), wantCode: CodeIndexNotReady},
		{name: "missing store", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core"}, wantCode: CodeIndexNotReady},
		{name: "expired request deadline", ctx: expired, arguments: GetBlastRadiusInput{StableKey: "sym-core"}, store: store, wantCode: CodeTraversalLimitReached},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := getBlastRadius(test.ctx, nil, test.arguments, test.store)
			if got := ErrorCode(err); got != test.wantCode {
				t.Fatalf("error code = %q, want %q (err=%v)", got, test.wantCode, err)
			}
		})
	}
}

func TestGetBlastRadiusIsRegisteredReadOnly(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterGetBlastRadius(server)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	listed, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == blastRadiusToolName {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("get_blast_radius annotations = %#v, want read-only", tool.Annotations)
			}
			return
		}
	}
	t.Fatal("get_blast_radius is not registered")
}

func blastRadiusGroupTotal(radius BlastRadius) int {
	total := 0
	for _, group := range radius.ByRepository {
		total += group.Count
	}
	return total
}

// blastRadiusStore builds an impact fan-in on sym-core: two direct consumers,
// one of them candidate-confidence, and one indirect consumer behind the exact
// one. Consumers live in a second repository so the repository axis is real.
func blastRadiusStore(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "core", Name: "core", Languages: "go"},
			{Key: "app", Name: "app", Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-core", RepositoryKey: "core", Language: "go", Name: "core", ModulePath: "example.com/core"},
			{Key: "pkg-app", RepositoryKey: "app", Language: "go", Name: "app", ModulePath: "example.com/app"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-core", RepositoryKey: "core", PackageKey: "pkg-core", Path: "core.go", Language: "go"},
			{Key: "file-app", RepositoryKey: "app", PackageKey: "pkg-app", Path: "app.go", Language: "go"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-core", CanonicalIdentity: "go:core.Core", FileKey: "file-core", Language: "go", Name: "Core", QualifiedName: "core.Core", Kind: "func", StartLine: 20, EndLine: 26},
			{StableKey: "sym-direct", CanonicalIdentity: "go:app.Direct", FileKey: "file-app", Language: "go", Name: "Direct", QualifiedName: "app.Direct", Kind: "func", StartLine: 30, EndLine: 36},
			{StableKey: "sym-loose", CanonicalIdentity: "go:core.Loose", FileKey: "file-core", Language: "go", Name: "Loose", QualifiedName: "core.Loose", Kind: "func", StartLine: 40, EndLine: 46},
			{StableKey: "sym-indirect", CanonicalIdentity: "go:app.Indirect", FileKey: "file-app", Language: "go", Name: "Indirect", QualifiedName: "app.Indirect", Kind: "func", StartLine: 50, EndLine: 56},
		},
		Edges: []hotsnapshot.EdgeRow{
			{SourceKey: "sym-direct", TargetKey: "sym-core", Kind: facts.CodeCallsDirect, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-app", EvidenceTargetFileKey: "file-core"},
			// The same consumer also depends on sym-core's type: one affected
			// symbol, two reasons.
			{SourceKey: "sym-direct", TargetKey: "sym-core", Kind: facts.CodeTypeUses, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-app", EvidenceTargetFileKey: "file-core"},
			{SourceKey: "sym-loose", TargetKey: "sym-core", Kind: facts.CodeReferences, Confidence: facts.CodeCandidate, Provenance: facts.CodeTreeSitterSyntax, EvidenceKind: "syntax", EvidenceSourceFileKey: "file-core", EvidenceTargetFileKey: "file-core"},
			{SourceKey: "sym-indirect", TargetKey: "sym-direct", Kind: facts.CodeCallsDirect, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-app", EvidenceTargetFileKey: "file-app"},
		},
	}, id, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// blastRadiusFanInStore is the shape that surfaced a real regression: four
// depth-1 callers of the root, each with its own depth-2 consumer. Every
// depth-1 row shares one tuple and every depth-2 row shares another, but each
// depth-2 row's `reached_from` names a different depth-1 parent -- on `workspace`,
// 26 of 29 rows shared one tuple while `reached_from` alone had 26 distinct
// values, and folding it into the grouping key fragmented every group back
// down to one row.
func blastRadiusFanInStore(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	symbols := []hotsnapshot.SymbolRow{
		{StableKey: "sym-core", CanonicalIdentity: "go:core.Core", FileKey: "file-core", Language: "go", Name: "Core", QualifiedName: "core.Core", Kind: "func", StartLine: 1, EndLine: 3},
	}
	edges := make([]hotsnapshot.EdgeRow, 0, 8)
	for index := 1; index <= 4; index++ {
		caller := "sym-caller" + strconv.Itoa(index)
		consumer := "sym-consumer" + strconv.Itoa(index)
		callerQN := "app.Caller" + strconv.Itoa(index)
		consumerQN := "app.Consumer" + strconv.Itoa(index)
		symbols = append(symbols,
			hotsnapshot.SymbolRow{StableKey: hotsnapshot.StableKey(caller), CanonicalIdentity: "go:" + callerQN, FileKey: "file-app", Language: "go", Name: "Caller" + strconv.Itoa(index), QualifiedName: callerQN, Kind: "func", StartLine: uint32(10 * index), EndLine: uint32(10*index + 2)},
			hotsnapshot.SymbolRow{StableKey: hotsnapshot.StableKey(consumer), CanonicalIdentity: "go:" + consumerQN, FileKey: "file-app", Language: "go", Name: "Consumer" + strconv.Itoa(index), QualifiedName: consumerQN, Kind: "func", StartLine: uint32(100 * index), EndLine: uint32(100*index + 2)},
		)
		edges = append(edges,
			hotsnapshot.EdgeRow{SourceKey: hotsnapshot.StableKey(caller), TargetKey: "sym-core", Kind: facts.CodeCallsDirect, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-app", EvidenceTargetFileKey: "file-core"},
			hotsnapshot.EdgeRow{SourceKey: hotsnapshot.StableKey(consumer), TargetKey: hotsnapshot.StableKey(caller), Kind: facts.CodeReferences, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-app", EvidenceTargetFileKey: "file-app"},
		)
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "core", Name: "core", Languages: "go"},
			{Key: "app", Name: "app", Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-core", RepositoryKey: "core", Language: "go", Name: "core", ModulePath: "example.com/core"},
			{Key: "pkg-app", RepositoryKey: "app", Language: "go", Name: "app", ModulePath: "example.com/app"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-core", RepositoryKey: "core", PackageKey: "pkg-core", Path: "core.go", Language: "go"},
			{Key: "file-app", RepositoryKey: "app", PackageKey: "pkg-app", Path: "app.go", Language: "go"},
		},
		Symbols: symbols,
		Edges:   edges,
	}, id, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// TestGetBlastRadiusReachesAMethodsCallersWithoutItsPairing is the same defect
// as TestTraceDependenciesWalksPastAMethodWithoutFollowingItsPairing, incoming:
// both tools build their traversal options in dependencyTraversalOptions, so one
// missing default failed both. A Go type is reached from its own method by
// METHOD_OF, which is containment and not a use, and the impact row builder
// refuses a non-reference kind -- so asking what breaks if a type changes died
// on the published graph with SNAPSHOT_UNAVAILABLE, which is the question this
// server exists to answer.
//
// The method itself is not impact: what it declares changing does not break the
// method, and the seeds already treat a member as content.
func TestGetBlastRadiusReachesAMethodsCallersWithoutItsPairing(t *testing.T) {
	store := methodPairingStore(t, 72)
	_, response, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-type", Depth: 2, Limit: 500,
	}, store)
	if err != nil {
		t.Fatalf("getBlastRadius() error = %v", err)
	}
	reached := make([]string, 0, len(response.Results.Symbols))
	for _, symbol := range response.Results.Symbols {
		reached = append(reached, symbol.QualifiedName)
	}
	if len(reached) != 0 {
		t.Fatalf("reached = %v, want nothing: the only edge into the type is its own method's pairing", reached)
	}
}

// TestGetBlastRadiusGroupsFanInDespiteDivergingReachedFrom is the regression
// guard for the bug found measuring ADR 0046 against a real 29-row page: with
// `reached_from` folded into the grouping tuple, every one of the four
// depth-2 consumers here -- which agree on everything else -- got its own
// group of one, costing more than not grouping at all. `reached_from` must
// stay off the tuple so the shared columns still group.
func TestGetBlastRadiusGroupsFanInDespiteDivergingReachedFrom(t *testing.T) {
	store := blastRadiusFanInStore(t, 82)
	_, response, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", Kinds: []string{"*"}, Limit: 500,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if response.Results.Affected != 8 {
		t.Fatalf("affected = %d, want 8 (4 callers + 4 consumers)", response.Results.Affected)
	}

	wire, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal response: %v", err)
	}
	var payload struct {
		Results struct {
			Files  []any `json:"files"`
			Groups []struct {
				Kind          string `json:"kind"`
				HopDepth      int    `json:"hop_depth"`
				ReachedFrom   string `json:"reached_from"`
				ViaKind       string `json:"via_kind"`
				ViaConfidence string `json:"via_confidence"`
				ViaProvenance string `json:"via_provenance"`
				Files         []struct {
					File string `json:"file"`
					At   []any  `json:"at"`
				} `json:"files"`
			} `json:"groups"`
		} `json:"results"`
	}
	if err := json.Unmarshal(wire, &payload); err != nil {
		t.Fatalf("unmarshal compact page: %v (%s)", err, wire)
	}
	if payload.Results.Files != nil {
		t.Fatalf("page stayed flat instead of grouping the two real tuples: %#v", payload.Results.Files)
	}
	if len(payload.Results.Groups) != 2 {
		t.Fatalf("groups = %#v, want exactly two: depth-1 callers and depth-2 consumers", payload.Results.Groups)
	}
	rowsIn := func(group int) int {
		total := 0
		for _, file := range payload.Results.Groups[group].Files {
			total += len(file.At)
		}
		return total
	}
	for _, group := range payload.Results.Groups {
		if group.HopDepth == 1 && rowsIn(0) != 4 && rowsIn(1) != 4 {
			t.Fatalf("no group of 4 rows found for depth 1: %#v", payload.Results.Groups)
		}
	}
	total := rowsIn(0) + rowsIn(1)
	if total != 8 {
		t.Fatalf("rows across both groups = %d, want 8", total)
	}
	// The depth-1 group's four callers all share `reached_from: core.Core`,
	// which the group hoists on its own even though it was excluded from the
	// grouping key: those rows are bare labels. The depth-2 group's four
	// consumers each name a different depth-1 parent, so that one field stays
	// on the row -- nothing else does, since kind, hop_depth, via_kind,
	// via_confidence and via_provenance are every one of them on the group.
	for _, group := range payload.Results.Groups {
		wantTail := group.ReachedFrom == ""
		for _, file := range group.Files {
			for _, entry := range file.At {
				array, isArray := entry.([]any)
				if isArray != wantTail {
					t.Fatalf("group reached_from=%q entry = %#v, want a tail=%t", group.ReachedFrom, entry, wantTail)
				}
				if isArray && len(array) != 2 {
					t.Fatalf("row tail = %#v, want exactly [label, reached_from]", array)
				}
			}
		}
	}
}

// TestGetBlastRadiusDetailedFormatRestoresDerivedIdentifiers mirrors the same
// contract on the impact tool: nothing the concise row withholds is lost.
func TestGetBlastRadiusDetailedFormatRestoresDerivedIdentifiers(t *testing.T) {
	store := blastRadiusStore(t, 71)
	_, response, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", ResponseFormat: ResponseFormatDetailed,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results.Symbols) == 0 {
		t.Fatal("detailed blast radius returned no symbols")
	}
	first := response.Results.Symbols[0]
	if first.FileKey == "" || first.ReachedFromKey != "sym-core" {
		t.Fatalf("detailed symbol = %#v, want the derived identifiers back", first)
	}
	if first.ReachedFrom != "core.Core" || first.EndLine < first.StartLine {
		t.Fatalf("detailed symbol = %#v, want the concise fields kept as well", first)
	}
}

// TestGetBlastRadiusExcludesLocalBindingsByDefault is ADR 0046's third stage:
// the frontier of an impact traversal is mostly local bindings, and paying for
// them pushed the real callers to page two. The filter must move what is
// reported, say so, and leave the traversal itself alone -- sym-handler is only
// reachable through the excluded variable and is still reported.
func TestGetBlastRadiusExcludesLocalBindingsByDefault(t *testing.T) {
	store := blastRadiusNoiseStore(t, 81)

	_, response, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core"}, store)
	if err != nil {
		t.Fatal(err)
	}
	radius := response.Results
	reported := make([]string, 0, len(radius.Symbols))
	for _, symbol := range radius.Symbols {
		reported = append(reported, symbol.QualifiedName)
	}
	if len(reported) != 2 || reported[0] != "app.Service.Run" || reported[1] != "app.handleBan" {
		t.Fatalf("default page = %v, want the two callable consumers", reported)
	}
	// total, affected and every axis count the filtered set, not the frontier.
	if radius.Affected != 2 || response.Total != 2 || response.Truncated {
		t.Fatalf("default impact = %#v (total %d), want two affected symbols", radius, response.Total)
	}
	wantDepths := []BlastRadiusDepthGroup{{Depth: 1, Count: 1}, {Depth: 2, Count: 1}}
	if len(radius.ByDepth) != 2 || radius.ByDepth[0] != wantDepths[0] || radius.ByDepth[1] != wantDepths[1] {
		t.Fatalf("default by_depth = %#v, want %#v", radius.ByDepth, wantDepths)
	}
	if len(radius.ByKind) != 1 || radius.ByKind[0] != (BlastRadiusGroup{Key: string(facts.CallsDirect), Count: 2}) {
		t.Fatalf("default by_kind = %#v, want the two callable consumers only", radius.ByKind)
	}
	if len(radius.ByRepository) != 1 || radius.ByRepository[0] != (BlastRadiusGroup{Key: "app", Count: 2}) {
		t.Fatalf("default by_repository = %#v", radius.ByRepository)
	}
	// A filtered count that does not say it is filtered is a lie about impact.
	if len(radius.Kinds) != 0 {
		t.Fatalf("kinds = %v, want the default filter reported as an exclusion", radius.Kinds)
	}
	if len(radius.KindsExcluded) != 2 || radius.KindsExcluded[0] != "field" || radius.KindsExcluded[1] != "variable" {
		t.Fatalf("kinds_default_excluded = %v, want field and variable", radius.KindsExcluded)
	}

	_, everything, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", Kinds: []string{"*"},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	all := everything.Results
	if all.Affected != 4 || everything.Total != 4 {
		t.Fatalf("kinds=* impact = %#v (total %d), want the bindings back", all, everything.Total)
	}
	if len(all.Kinds) != 1 || all.Kinds[0] != "*" || len(all.KindsExcluded) != 0 {
		t.Fatalf("kinds=* filter statement = %v / %v, want the wildcard echoed", all.Kinds, all.KindsExcluded)
	}
	wantAllDepths := []BlastRadiusDepthGroup{{Depth: 1, Count: 3}, {Depth: 2, Count: 1}}
	if len(all.ByDepth) != 2 || all.ByDepth[0] != wantAllDepths[0] || all.ByDepth[1] != wantAllDepths[1] {
		t.Fatalf("kinds=* by_depth = %#v, want %#v", all.ByDepth, wantAllDepths)
	}

	_, onlyBindings, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", Kinds: []string{"variable"},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	selected := onlyBindings.Results
	if selected.Affected != 1 || selected.Symbols[0].QualifiedName != "app.handleBan.userId" {
		t.Fatalf("kinds=[variable] impact = %#v, want the binding alone", selected)
	}
	if len(selected.Kinds) != 1 || selected.Kinds[0] != "variable" || len(selected.KindsExcluded) != 0 {
		t.Fatalf("kinds=[variable] filter statement = %v / %v", selected.Kinds, selected.KindsExcluded)
	}
}

// TestGetBlastRadiusRejectsAnUnknownKind keeps a typo from understating an
// impact: a kind nothing publishes would silently match nothing.
func TestGetBlastRadiusRejectsAnUnknownKind(t *testing.T) {
	store := blastRadiusNoiseStore(t, 82)
	_, _, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", Kinds: []string{"functions"},
	}, store)
	if ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("unknown kind error code = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
	message := err.Error()
	for _, named := range []string{"\"*\"", "function", "variable"} {
		if !strings.Contains(message, named) {
			t.Fatalf("error %q does not name the accepted value %q", message, named)
		}
	}
	// The wildcard is a filter or it is not; asking for both is a mistake.
	if _, _, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", Kinds: []string{"*", "function"},
	}, store); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("wildcard mixed with a list = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
}

// TestGetBlastRadiusKindFilterBindsTheCursor pins the filter to the query
// identity: resuming a page of one reported set inside another would page over
// a list that no longer has the same members.
func TestGetBlastRadiusKindFilterBindsTheCursor(t *testing.T) {
	store := blastRadiusNoiseStore(t, 83)
	_, first, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", Kinds: []string{"*"}, Limit: 1,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if first.NextCursor == nil {
		t.Fatal("kinds=* first page returned no cursor")
	}
	if _, _, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", Limit: 1, Cursor: *first.NextCursor,
	}, store); ErrorCode(err) != CodeCursorInvalid {
		t.Fatalf("cursor reused under another filter = %q, want %q", ErrorCode(err), CodeCursorInvalid)
	}
}

// TestGetBlastRadiusCompactViewHoistsSharedColumns is the payload contract of
// ADR 0046: the confidence and the provenance every row shares are written
// once, the page is grouped by file, and nothing the full view stated is lost.
func TestGetBlastRadiusCompactViewHoistsSharedColumns(t *testing.T) {
	store := blastRadiusNoiseStore(t, 84)
	_, response, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core"}, store)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Total         int              `json:"total"`
		Returned      int              `json:"returned"`
		SnapshotAgeMS *int64           `json:"snapshot_age_ms"`
		Truncated     *bool            `json:"truncated"`
		Results       *json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Total != 2 || envelope.Returned != 2 {
		t.Fatalf("compact envelope = %s", encoded)
	}
	if envelope.SnapshotAgeMS != nil || envelope.Truncated != nil {
		t.Fatalf("compact envelope keeps fields that carry nothing: %s", encoded)
	}
	var results struct {
		Root          string         `json:"root"`
		Depth         int            `json:"depth"`
		MaxNodes      int            `json:"max_nodes"`
		KindsExcluded []string       `json:"kinds_default_excluded"`
		Affected      int            `json:"affected"`
		DeepestDepth  int            `json:"deepest_depth"`
		ByDepth       map[string]int `json:"by_depth"`
		ByKind        map[string]int `json:"by_kind"`
		ByRepository  map[string]int `json:"by_repository"`
		ByPackage     []struct {
			Package    string `json:"package"`
			Repository string `json:"repository"`
			Count      int    `json:"count"`
		} `json:"by_package"`
		Repository    string `json:"repository"`
		Kind          string `json:"kind"`
		HopDepth      int    `json:"hop_depth"`
		ReachedFrom   string `json:"reached_from"`
		ViaKind       string `json:"via_kind"`
		ViaConfidence string `json:"via_confidence"`
		ViaProvenance string `json:"via_provenance"`
		Files         []struct {
			File string    `json:"file"`
			Repo string    `json:"repo"`
			At   []([]any) `json:"at"`
		} `json:"files"`
		RootKey string           `json:"root_key"`
		Symbols *json.RawMessage `json:"symbols"`
	}
	if err := json.Unmarshal(*envelope.Results, &results); err != nil {
		t.Fatalf("compact results = %s: %v", *envelope.Results, err)
	}
	// The root is the triple every tool accepts as a selector, not a key.
	if results.Root != "core:core.go:20" || results.RootKey != "" || results.Symbols != nil {
		t.Fatalf("compact header = %s", *envelope.Results)
	}
	if results.ViaConfidence != string(facts.ExactTypechecked) ||
		results.ViaProvenance != string(facts.GoTypesUse) ||
		results.ViaKind != string(facts.CallsDirect) ||
		results.Repository != "app" {
		t.Fatalf("compact header hoisted %#v, want the columns every row shares", results)
	}
	// kind, depth and reached_from disagree across the two rows, so they stay
	// on the rows and the header must not claim them.
	if results.Kind != "" || results.HopDepth != 0 || results.ReachedFrom != "" {
		t.Fatalf("compact header = %#v, want the varying columns left on the rows", results)
	}
	if results.Affected != 2 || results.DeepestDepth != 2 || results.Depth != DefaultBlastRadiusDepth ||
		results.MaxNodes != DefaultBlastRadiusMaxNodes {
		t.Fatalf("compact aggregates = %#v", results)
	}
	if len(results.KindsExcluded) != 2 {
		t.Fatalf("compact filter statement = %v, want the default exclusion named", results.KindsExcluded)
	}
	wantDepths := map[string]int{"1": 1, "2": 1}
	if len(results.ByDepth) != 2 || results.ByDepth["1"] != wantDepths["1"] || results.ByDepth["2"] != wantDepths["2"] {
		t.Fatalf("compact by_depth = %v, want %v", results.ByDepth, wantDepths)
	}
	if results.ByKind[string(facts.CallsDirect)] != 2 || results.ByRepository["app"] != 2 {
		t.Fatalf("compact axes = %v / %v", results.ByKind, results.ByRepository)
	}
	if len(results.ByPackage) != 1 || results.ByPackage[0].Package != "app" ||
		results.ByPackage[0].Count != 2 || results.ByPackage[0].Repository != "" {
		t.Fatalf("compact by_package = %#v, want the hoisted repository omitted", results.ByPackage)
	}
	if len(results.Files) != 2 || results.Files[0].File != "app.go" || results.Files[1].File != "handlers.go" {
		t.Fatalf("compact files = %#v, want the page grouped by file", results.Files)
	}
	for _, group := range results.Files {
		if group.Repo != "" {
			t.Fatalf("group %#v repeats the hoisted repository", group)
		}
		if len(group.At) != 1 {
			t.Fatalf("group %#v, want one entry per reported symbol", group)
		}
	}
	// The tail spells what did not hoist, in the shared order: kind, depth and
	// the symbol the row was reached from.
	wantTails := [][]any{
		{"app.Service.Run@50-58", "method", "1", "core.Core"},
		{"app.handleBan@30-40", "function", "2", "app.handleBan.userId"},
	}
	for index, group := range results.Files {
		entry := group.At[0]
		if len(entry) != len(wantTails[index]) {
			t.Fatalf("entry %v, want %v", entry, wantTails[index])
		}
		for position, want := range wantTails[index] {
			if entry[position] != want {
				t.Fatalf("entry %v, want %v", entry, wantTails[index])
			}
		}
	}
}

// TestGetBlastRadiusFullViewKeepsTodaysShape is the declared escape hatch of
// ADR 0046: a client that depends on the row-per-field payload asks for it and
// gets exactly the fields it had, plus the statement of which kinds the answer
// counted -- a filter that does not announce itself is the one thing the full
// view may not keep.
func TestGetBlastRadiusFullViewKeepsTodaysShape(t *testing.T) {
	store := blastRadiusStore(t, 85)
	_, response, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", View: ViewFull,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &envelope); err != nil {
		t.Fatal(err)
	}
	// `guidance` has always been omitted when the whole answer fits one page.
	wantEnvelope := []string{
		"completeness", "coverage", "next_cursor", "results",
		"returned", "snapshot_age_ms", "snapshot_id", "total", "truncated",
	}
	if got := sortedJSONKeys(envelope); !equalStrings(got, wantEnvelope) {
		t.Fatalf("full envelope keys = %v, want %v", got, wantEnvelope)
	}
	var results map[string]json.RawMessage
	if err := json.Unmarshal(envelope["results"], &results); err != nil {
		t.Fatal(err)
	}
	wantResults := []string{
		"affected", "by_depth", "by_kind", "by_package", "by_repository",
		"deepest_depth", "depth", "kinds_default_excluded", "max_nodes",
		"root_key", "root_repository", "symbols", "traversal_truncated",
	}
	if got := sortedJSONKeys(results); !equalStrings(got, wantResults) {
		t.Fatalf("full results keys = %v, want %v", got, wantResults)
	}
	var symbols []map[string]json.RawMessage
	if err := json.Unmarshal(results["symbols"], &symbols); err != nil {
		t.Fatal(err)
	}
	if len(symbols) != 3 {
		t.Fatalf("full symbols = %d rows, want 3", len(symbols))
	}
	wantRow := []string{
		"depth", "end_line", "file_path", "kind", "language", "name",
		"qualified_name", "reached_from", "repository", "start_line",
		"via_confidence", "via_kind", "via_provenance",
	}
	if got := sortedJSONKeys(symbols[0]); !equalStrings(got, wantRow) {
		t.Fatalf("full row keys = %v, want %v", got, wantRow)
	}
}

// TestGetBlastRadiusCompactPageCostsLessThanFull measures the payload of one
// full page against the same page compacted, and against the same page with the
// default kind filter. The proportions are ADR 0046's: 48 of the 50 rows a real
// impact page carried were local bindings.
func TestGetBlastRadiusCompactPageCostsLessThanFull(t *testing.T) {
	store := blastRadiusPageStore(t, 86, 48, 2)

	sizes := make(map[string]int, 3)
	rows := make(map[string]int, 3)
	for _, mode := range []struct {
		name      string
		arguments GetBlastRadiusInput
	}{
		{name: "full", arguments: GetBlastRadiusInput{StableKey: "sym-core", Kinds: []string{"*"}, View: ViewFull}},
		{name: "compact", arguments: GetBlastRadiusInput{StableKey: "sym-core", Kinds: []string{"*"}}},
		{name: "compact+default kinds", arguments: GetBlastRadiusInput{StableKey: "sym-core"}},
	} {
		_, response, err := getBlastRadius(context.Background(), nil, mode.arguments, store)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(response)
		if err != nil {
			t.Fatal(err)
		}
		sizes[mode.name] = len(encoded)
		rows[mode.name] = response.Returned
		t.Logf("%s: %d bytes, %d of %d rows", mode.name, len(encoded), response.Returned, response.Total)
	}
	if rows["full"] != 50 || rows["compact"] != 50 || rows["compact+default kinds"] != 2 {
		t.Fatalf("measured pages = %v, want fifty rows in both views", rows)
	}
	if sizes["compact"] >= sizes["full"] {
		t.Fatalf("compact page is %d bytes against %d full, want the repeated columns gone", sizes["compact"], sizes["full"])
	}
	if sizes["compact+default kinds"] >= sizes["compact"] {
		t.Fatalf("filtered page is %d bytes against %d unfiltered", sizes["compact+default kinds"], sizes["compact"])
	}
}

func sortedJSONKeys(object map[string]json.RawMessage) []string {
	keys := make([]string, 0, len(object))
	for key := range object {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// blastRadiusNoiseStore reproduces what ADR 0046 measured on `workspace`: the
// frontier of an impact traversal is mostly local bindings, and the callers a
// reviewer came for sit behind them. sym-handler is reachable only through the
// excluded variable, so a filter on what is reported must not shorten the walk.
func blastRadiusNoiseStore(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	exact := func(source, target hotsnapshot.StableKey, fileKey string, kind uint8) hotsnapshot.EdgeRow {
		return hotsnapshot.EdgeRow{
			SourceKey: source, TargetKey: target, Kind: kind,
			Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse,
			EvidenceKind: "types", EvidenceSourceFileKey: fileKey, EvidenceTargetFileKey: "file-core",
		}
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "core", Name: "core", Languages: "go"},
			{Key: "app", Name: "app", Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-core", RepositoryKey: "core", Language: "go", Name: "core", ModulePath: "example.com/core"},
			{Key: "pkg-app", RepositoryKey: "app", Language: "go", Name: "app", ModulePath: "example.com/app"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-core", RepositoryKey: "core", PackageKey: "pkg-core", Path: "core.go", Language: "go"},
			{Key: "file-app", RepositoryKey: "app", PackageKey: "pkg-app", Path: "app.go", Language: "go"},
			{Key: "file-handlers", RepositoryKey: "app", PackageKey: "pkg-app", Path: "handlers.go", Language: "go"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-core", CanonicalIdentity: "go:core.Core", FileKey: "file-core", Language: "go", Name: "Core", QualifiedName: "core.Core", Kind: "func", StartLine: 20, EndLine: 26},
			{StableKey: "sym-binding", CanonicalIdentity: "go:app.handleBan.userId", FileKey: "file-app", Language: "go", Name: "userId", QualifiedName: "app.handleBan.userId", Kind: "variable", StartLine: 12, EndLine: 12},
			{StableKey: "sym-owner", CanonicalIdentity: "go:app.Ban.owner", FileKey: "file-app", Language: "go", Name: "owner", QualifiedName: "app.Ban.owner", Kind: "field", StartLine: 8, EndLine: 8},
			{StableKey: "sym-service", CanonicalIdentity: "go:app.Service.Run", FileKey: "file-app", Language: "go", Name: "Run", QualifiedName: "app.Service.Run", Kind: "method", StartLine: 50, EndLine: 58},
			{StableKey: "sym-handler", CanonicalIdentity: "go:app.handleBan", FileKey: "file-handlers", Language: "go", Name: "handleBan", QualifiedName: "app.handleBan", Kind: "function", StartLine: 30, EndLine: 40},
		},
		Edges: []hotsnapshot.EdgeRow{
			exact("sym-binding", "sym-core", "file-app", facts.CodeCallsDirect),
			exact("sym-owner", "sym-core", "file-app", facts.CodeTypeUses),
			exact("sym-service", "sym-core", "file-app", facts.CodeCallsDirect),
			// The caller reaches sym-core only through the excluded binding.
			exact("sym-handler", "sym-binding", "file-handlers", facts.CodeCallsDirect),
		},
	}, id, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// blastRadiusPageStore fans bindings and callables into sym-core so one page is
// exactly the size the payload budget of ADR 0046 was measured against. The
// rows are spread over four files, because grouping by file is only honest when
// there is more than one group.
func blastRadiusPageStore(t *testing.T, id uint64, bindings, callables int) *hotsnapshot.SnapshotStore {
	t.Helper()
	files := []hotsnapshot.FileRow{
		{Key: "file-core", RepositoryKey: "core", PackageKey: "pkg-core", Path: "src/core.go", Language: "go"},
	}
	for index := range 4 {
		files = append(files, hotsnapshot.FileRow{
			Key:           "file-app-" + strconv.Itoa(index),
			RepositoryKey: "app", PackageKey: "pkg-app",
			Path:     "src/handlers/routes_players_" + strconv.Itoa(index) + ".go",
			Language: "go",
		})
	}
	symbols := []hotsnapshot.SymbolRow{
		{StableKey: "sym-core", CanonicalIdentity: "go:core.getRequiredField", FileKey: "file-core", Language: "go", Name: "getRequiredField", QualifiedName: "core.getRequiredField", Kind: "func", StartLine: 20, EndLine: 26},
	}
	edges := make([]hotsnapshot.EdgeRow, 0, bindings+callables)
	consume := func(index int, kind, name string) {
		key := hotsnapshot.StableKey("sym-" + kind + "-" + strconv.Itoa(index))
		fileKey := "file-app-" + strconv.Itoa(index%4)
		line := uint32(10 + index*3)
		symbols = append(symbols, hotsnapshot.SymbolRow{
			StableKey: key, CanonicalIdentity: "go:app." + name, FileKey: fileKey, Language: "go",
			Name: name, QualifiedName: "app." + name, Kind: kind, StartLine: line, EndLine: line + 1,
		})
		edges = append(edges, hotsnapshot.EdgeRow{
			SourceKey: key, TargetKey: "sym-core",
			Kind: facts.CodeCallsDirect, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse,
			EvidenceKind: "types", EvidenceSourceFileKey: fileKey, EvidenceTargetFileKey: "file-core",
		})
	}
	for index := range bindings {
		consume(index, "variable", "handleBan"+strconv.Itoa(index)+".userId")
	}
	for index := range callables {
		consume(index, "function", "handleBan"+strconv.Itoa(index))
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "core", Name: "core", Languages: "go"},
			{Key: "app", Name: "app", Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-core", RepositoryKey: "core", Language: "go", Name: "core", ModulePath: "example.com/core"},
			{Key: "pkg-app", RepositoryKey: "app", Language: "go", Name: "app", ModulePath: "example.com/app"},
		},
		Files:   files,
		Symbols: symbols,
		Edges:   edges,
	}, id, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// TestTraceDependenciesWalksDepthAndRecordsDiscoveringEdge pins the traversal
// contract: the root is not its own dependency, each node carries the depth and
// the edge it was reached by, and depth is a hard bound.
func TestTraceDependenciesWalksDepthAndRecordsDiscoveringEdge(t *testing.T) {
	store := traceDependenciesStore(t, 31)

	_, response, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{StableKey: "sym-root"}, store)
	if err != nil {
		t.Fatalf("traceDependencies() error = %v", err)
	}
	if response.Total != 3 || response.Returned != 3 || response.Truncated {
		t.Fatalf("pagination = %#v, want three untruncated dependencies", response)
	}
	trace := response.Results
	if trace.RootKey != "sym-root" || trace.RootRepository != "root" {
		t.Fatalf("root = %#v", trace)
	}
	if trace.Depth != DefaultDependencyDepth || trace.Reached != 3 || trace.DeepestDepth != 3 || trace.TraversalTruncated {
		t.Fatalf("traversal metadata = %#v", trace)
	}
	wantNames := []string{"root.Level1", "root.Level2", "other.Level3"}
	for index, node := range trace.Nodes {
		if node.QualifiedName != wantNames[index] || node.Depth != index+1 {
			t.Fatalf("node %d = %#v, want %q at depth %d", index, node, wantNames[index], index+1)
		}
	}
	if first := trace.Nodes[0]; first.ReachedFrom != "root.Root" || first.ViaKind != string(facts.CallsDirect) || first.ViaConfidence != string(facts.ExactTypechecked) {
		t.Fatalf("first hop = %#v, want an exact call from the root", first)
	}
	if third := trace.Nodes[2]; third.ReachedFrom != "root.Level2" || third.Repository != "other" {
		t.Fatalf("third hop = %#v, want discovery from level2 in the other repository", third)
	}
	// A row an agent cannot open costs a second call before it means anything.
	for index, node := range trace.Nodes {
		if node.FilePath == "" || node.EndLine < node.StartLine {
			t.Fatalf("node %d = %#v, want a file path and a declaration range", index, node)
		}
	}
	for index, node := range trace.Nodes {
		if node.FileKey != "" || node.ReachedFromKey != "" {
			t.Fatalf("node %d = %#v, want derived identifiers withheld from the concise format", index, node)
		}
	}
	if response.Coverage != (Coverage{Exact: 2, Candidate: 1}) {
		t.Fatalf("coverage = %#v, want two exact and one candidate hop", response.Coverage)
	}

	_, shallow, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{StableKey: "sym-root", Depth: 1}, store)
	if err != nil {
		t.Fatal(err)
	}
	if shallow.Total != 1 || shallow.Results.DeepestDepth != 1 {
		t.Fatalf("depth-1 trace = %#v, want only the direct dependency", shallow.Results)
	}
}

// TestTraceDependenciesSeparatesReachabilityFromRowFilters is the honest part
// of the contract: confidence gates which edges may be followed, while repo
// only selects rows already reached.
func TestTraceDependenciesSeparatesReachabilityFromRowFilters(t *testing.T) {
	store := traceDependenciesStore(t, 32)

	_, exactOnly, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
		StableKey: "sym-root", Confidence: string(facts.ExactTypechecked),
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if exactOnly.Total != 1 || exactOnly.Results.Nodes[0].QualifiedName != "root.Level1" {
		t.Fatalf("confidence-gated trace = %#v, want the candidate hop to cut the path", exactOnly.Results)
	}

	_, byRepo, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
		StableKey: "sym-root", Repo: "other",
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if byRepo.Total != 1 || byRepo.Results.Nodes[0].QualifiedName != "other.Level3" || byRepo.Results.Reached != 3 {
		t.Fatalf("repo-filtered trace = %#v, want the depth-3 node reached through repo-root", byRepo.Results)
	}

	_, byKind, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
		StableKey: "sym-root", EdgeKinds: []string{string(facts.TypeUses)},
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if byKind.Total != 0 || byKind.Results.Reached != 0 {
		t.Fatalf("kind-gated trace = %#v, want no reachable dependency", byKind.Results)
	}
}

func TestTraceDependenciesPaginatesWithSnapshotCursor(t *testing.T) {
	store := traceDependenciesStore(t, 33)

	_, first, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{StableKey: "sym-root", Limit: 2}, store)
	if err != nil {
		t.Fatal(err)
	}
	if first.Returned != 2 || !first.Truncated || first.NextCursor == nil {
		t.Fatalf("first page = %#v, want two nodes and a cursor", first)
	}
	_, second, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
		StableKey: "sym-root", Limit: 2, Cursor: *first.NextCursor,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if second.Returned != 1 || second.Truncated || second.NextCursor != nil {
		t.Fatalf("second page = %#v, want the final node", second)
	}
	if second.Results.Nodes[0].QualifiedName != "other.Level3" {
		t.Fatalf("second page node = %#v", second.Results.Nodes[0])
	}
}

func TestTraceDependenciesClassifiesFailures(t *testing.T) {
	store := traceDependenciesStore(t, 34)
	expired, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()

	cases := []struct {
		name      string
		ctx       context.Context
		arguments TraceDependenciesInput
		store     *hotsnapshot.SnapshotStore
		wantCode  string
	}{
		{name: "empty key", ctx: context.Background(), arguments: TraceDependenciesInput{}, store: store, wantCode: CodeInvalidArgument},
		{name: "depth above maximum", ctx: context.Background(), arguments: TraceDependenciesInput{StableKey: "sym-root", Depth: MaximumDependencyDepth + 1}, store: store, wantCode: CodeInvalidArgument},
		{name: "max_nodes above maximum", ctx: context.Background(), arguments: TraceDependenciesInput{StableKey: "sym-root", MaxNodes: MaximumDependencyMaxNodes + 1}, store: store, wantCode: CodeInvalidArgument},
		{name: "limit above maximum", ctx: context.Background(), arguments: TraceDependenciesInput{StableKey: "sym-root", Limit: MaximumDependencyLimit + 1}, store: store, wantCode: CodeInvalidArgument},
		{name: "unsupported edge kind", ctx: context.Background(), arguments: TraceDependenciesInput{StableKey: "sym-root", EdgeKinds: []string{"CONTAINS_FILE"}}, store: store, wantCode: CodeInvalidArgument},
		{name: "unsupported confidence", ctx: context.Background(), arguments: TraceDependenciesInput{StableKey: "sym-root", Confidence: "MAYBE"}, store: store, wantCode: CodeInvalidArgument},
		{name: "missing symbol", ctx: context.Background(), arguments: TraceDependenciesInput{StableKey: "sym-missing"}, store: store, wantCode: CodeSymbolNotFound},
		{name: "invalid cursor", ctx: context.Background(), arguments: TraceDependenciesInput{StableKey: "sym-root", Cursor: "not-a-cursor"}, store: store, wantCode: CodeCursorInvalid},
		{name: "unpublished snapshot", ctx: context.Background(), arguments: TraceDependenciesInput{StableKey: "sym-root"}, store: hotsnapshot.NewSnapshotStore(nil), wantCode: CodeIndexNotReady},
		{name: "missing store", ctx: context.Background(), arguments: TraceDependenciesInput{StableKey: "sym-root"}, wantCode: CodeIndexNotReady},
		{name: "expired request deadline", ctx: expired, arguments: TraceDependenciesInput{StableKey: "sym-root"}, store: store, wantCode: CodeTraversalLimitReached},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := traceDependencies(test.ctx, nil, test.arguments, test.store)
			if got := ErrorCode(err); got != test.wantCode {
				t.Fatalf("error code = %q, want %q (err=%v)", got, test.wantCode, err)
			}
		})
	}
}

func TestTraceDependenciesReportsNodeLimitTruncation(t *testing.T) {
	store := traceDependenciesStore(t, 35)
	_, response, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{StableKey: "sym-root", MaxNodes: 2}, store)
	if err != nil {
		t.Fatal(err)
	}
	if !response.Results.TraversalTruncated || response.Results.Reached != 1 {
		t.Fatalf("node-bounded trace = %#v, want one dependency and a truncated traversal", response.Results)
	}
}

func TestTraceDependenciesIsRegisteredReadOnly(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterTraceDependencies(server)
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
		if tool.Name == traceDependenciesToolName {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("trace_dependencies annotations = %#v, want read-only", tool.Annotations)
			}
			return
		}
	}
	t.Fatal("trace_dependencies is not registered")
}

// traceDependenciesStore builds a chain root -> level1 -> level2 -> level3 that
// crosses into a second repository at the last hop, with one candidate edge in
// the middle so confidence filters have something to cut.
func traceDependenciesStore(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-root", Name: "root", Languages: "go"},
			{Key: "other", Name: "other", Languages: "ts"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-root", RepositoryKey: "repo-root", Language: "go", Name: "root", ModulePath: "example.com/root"},
			{Key: "pkg-other", RepositoryKey: "other", Language: "ts", Name: "other", ModulePath: "example.com/other"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-root", RepositoryKey: "repo-root", PackageKey: "pkg-root", Path: "root.go", Language: "go"},
			{Key: "file-other", RepositoryKey: "other", PackageKey: "pkg-other", Path: "other.ts", Language: "ts"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-root", CanonicalIdentity: "go:root.Root", FileKey: "file-root", Language: "go", Name: "Root", QualifiedName: "root.Root", Kind: "func", StartLine: 20, EndLine: 26},
			{StableKey: "sym-level1", CanonicalIdentity: "go:root.Level1", FileKey: "file-root", Language: "go", Name: "Level1", QualifiedName: "root.Level1", Kind: "func", StartLine: 30, EndLine: 36},
			{StableKey: "sym-level2", CanonicalIdentity: "go:root.Level2", FileKey: "file-root", Language: "go", Name: "Level2", QualifiedName: "root.Level2", Kind: "func", StartLine: 40, EndLine: 46},
			{StableKey: "sym-level3", CanonicalIdentity: "ts:other.Level3", FileKey: "file-other", Language: "ts", Name: "Level3", QualifiedName: "other.Level3", Kind: "function", StartLine: 50, EndLine: 56},
		},
		Edges: []hotsnapshot.EdgeRow{
			{SourceKey: "sym-root", TargetKey: "sym-level1", Kind: facts.CodeCallsDirect, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-root", EvidenceTargetFileKey: "file-root"},
			{SourceKey: "sym-level1", TargetKey: "sym-level2", Kind: facts.CodeReferences, Confidence: facts.CodeCandidate, Provenance: facts.CodeTreeSitterSyntax, EvidenceKind: "syntax", EvidenceSourceFileKey: "file-root", EvidenceTargetFileKey: "file-root"},
			{SourceKey: "sym-level2", TargetKey: "sym-level3", Kind: facts.CodeImportsSymbol, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-root", EvidenceTargetFileKey: "file-other"},
		},
	}, id, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// TestTraceDependenciesDetailedFormatRestoresDerivedIdentifiers keeps the cut
// reversible: the concise row drops the file key and the reached-from key
// because a path and a qualified name already name both, but a caller that
// wants the canonical identifiers must still be able to ask for them.
func TestTraceDependenciesDetailedFormatRestoresDerivedIdentifiers(t *testing.T) {
	store := traceDependenciesStore(t, 61)
	_, response, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
		StableKey: "sym-root", ResponseFormat: ResponseFormatDetailed,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if len(response.Results.Nodes) == 0 {
		t.Fatal("detailed trace returned no nodes")
	}
	first := response.Results.Nodes[0]
	if first.FileKey != "file-root" || first.ReachedFromKey != "sym-root" {
		t.Fatalf("detailed node = %#v, want the derived identifiers back", first)
	}
	if first.ReachedFrom != "root.Root" || first.EndLine < first.StartLine {
		t.Fatalf("detailed node = %#v, want the concise fields kept as well", first)
	}
}

// TestTraceDependenciesFullViewKeepsTodaysPayload pins the shape a client that
// asks for `view: "full"` still gets: every envelope field present, including
// the ones that carry nothing, and every column on every row.
func TestTraceDependenciesFullViewKeepsTodaysPayload(t *testing.T) {
	store := traceDependenciesStore(t, 36)
	_, response, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
		StableKey: "sym-root", View: ViewFull,
	}, store)
	if err != nil {
		t.Fatalf("traceDependencies() error = %v", err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal response: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(encoded, &payload); err != nil {
		t.Fatalf("Unmarshal payload: %v", err)
	}
	// The age is the one field a snapshot cannot state twice in a row.
	if _, ok := payload["snapshot_age_ms"].(float64); !ok {
		t.Fatalf("snapshot_age_ms = %#v, want a number", payload["snapshot_age_ms"])
	}
	payload["snapshot_age_ms"] = "measured"

	row := func(name, qualifiedName, kind string, depth int, repository, language, path string,
		startLine, endLine int, reachedFrom string, viaKind facts.EdgeKind,
		viaConfidence facts.Confidence, viaProvenance facts.Provenance,
	) map[string]any {
		return map[string]any{
			"name": name, "qualified_name": qualifiedName, "kind": kind,
			"depth": float64(depth), "repository": repository, "language": language,
			"file_path": path, "start_line": float64(startLine), "end_line": float64(endLine),
			"reached_from": reachedFrom, "via_kind": string(viaKind),
			"via_confidence": string(viaConfidence), "via_provenance": string(viaProvenance),
		}
	}
	want := map[string]any{
		"snapshot_id": float64(36), "snapshot_age_ms": "measured",
		"total": float64(3), "returned": float64(3),
		"truncated": false, "next_cursor": nil,
		"coverage": map[string]any{
			"exact": float64(2), "candidate": float64(1),
			"unresolved_related": float64(0), "package_level": float64(0),
		},
		"results": map[string]any{
			"root_key": "sym-root", "root_repository": "root",
			"depth": float64(DefaultDependencyDepth), "max_nodes": float64(DefaultDependencyMaxNodes),
			"reached": float64(3), "deepest_depth": float64(3), "traversal_truncated": false,
			"nodes": []any{
				row("Level1", "root.Level1", "func", 1, "root", "go", "root.go", 30, 36,
					"root.Root", facts.CallsDirect, facts.ExactTypechecked, facts.GoTypesUse),
				row("Level2", "root.Level2", "func", 2, "root", "go", "root.go", 40, 46,
					"root.Level1", facts.References, facts.Candidate, facts.TreeSitterSyntax),
				row("Level3", "other.Level3", "function", 3, "other", "ts", "other.ts", 50, 56,
					"root.Level2", facts.ImportsSymbol, facts.ExactTypechecked, facts.GoTypesUse),
			},
		},
	}
	if !reflect.DeepEqual(payload, want) {
		t.Fatalf("full payload = %s", encoded)
	}
}

// TestTraceDependenciesCompactViewHoistsWhatEveryRowShares is the default
// answer: the same three hops, the same edges, the same confidence and the
// same provenance, spelled without repeating what the page agrees on.
func TestTraceDependenciesCompactViewHoistsWhatEveryRowShares(t *testing.T) {
	store := traceDependenciesStore(t, 37)

	// One hop: every column is shared, so the header states each of them once
	// and the row is nothing but where the symbol is declared.
	_, single, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
		StableKey: "sym-root", Depth: 1,
	}, store)
	if err != nil {
		t.Fatalf("traceDependencies() error = %v", err)
	}
	encoded, err := json.Marshal(single)
	if err != nil {
		t.Fatalf("Marshal response: %v", err)
	}
	wantSingle := `{"snapshot_id":37,"total":1,"returned":1,"coverage":{"exact":1},` +
		`"results":{"root_key":"sym-root","root_repository":"root","depth":1,"max_nodes":5000,` +
		`"reached":1,"deepest_depth":1,"repository":"root","kind":"func","hop_depth":1,` +
		`"reached_from":"root.Root","via_kind":"CALLS_DIRECT","via_confidence":"EXACT_TYPECHECKED",` +
		`"via_provenance":"GO_TYPES_USE","files":[{"file":"root.go","at":["root.Level1@30-36"]}]}}`
	if string(encoded) != wantSingle {
		t.Fatalf("compact payload =\n%s\nwant\n%s", encoded, wantSingle)
	}

	// Three hops that agree on nothing: every column stays on its row, in the
	// documented order, and the group states the repository it is not in.
	_, full, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{StableKey: "sym-root"}, store)
	if err != nil {
		t.Fatalf("traceDependencies() error = %v", err)
	}
	compact, err := json.Marshal(full)
	if err != nil {
		t.Fatalf("Marshal response: %v", err)
	}
	wantCompact := `{"snapshot_id":37,"total":3,"returned":3,"coverage":{"exact":2,"candidate":1},` +
		`"results":{"root_key":"sym-root","root_repository":"root","depth":3,"max_nodes":5000,` +
		`"reached":3,"deepest_depth":3,"files":[` +
		`{"file":"root.go","repo":"root","at":[` +
		`["root.Level1@30-36","func","1","root.Root","CALLS_DIRECT","EXACT_TYPECHECKED","GO_TYPES_USE"],` +
		`["root.Level2@40-46","func","2","root.Level1","REFERENCES","CANDIDATE","TREE_SITTER_SYNTAX"]]},` +
		`{"file":"other.ts","repo":"other","at":[` +
		`["other.Level3@50-56","function","3","root.Level2","IMPORTS_SYMBOL","EXACT_TYPECHECKED","GO_TYPES_USE"]]}]}}`
	if string(compact) != wantCompact {
		t.Fatalf("compact payload =\n%s\nwant\n%s", compact, wantCompact)
	}

	// Same page, same facts, fewer bytes: that is the whole point of the view.
	_, verbose, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
		StableKey: "sym-root", View: ViewFull,
	}, store)
	if err != nil {
		t.Fatalf("traceDependencies() error = %v", err)
	}
	encodedFull, err := json.Marshal(verbose)
	if err != nil {
		t.Fatalf("Marshal response: %v", err)
	}
	t.Logf("one page of three hops: full %d bytes, compact %d bytes", len(encodedFull), len(compact))
	if len(compact) >= len(encodedFull) {
		t.Fatalf("compact payload is %d bytes and full is %d", len(compact), len(encodedFull))
	}
}

// TestTraceDependenciesRejectsAViewItCannotAnswer keeps the argument honest: a
// traversal is not a set of files, so asking for that granularity is an error
// rather than a compact answer wearing the wrong name.
func TestTraceDependenciesRejectsAViewItCannotAnswer(t *testing.T) {
	store := traceDependenciesStore(t, 38)
	for _, view := range []string{ViewFiles, "brief"} {
		_, _, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
			StableKey: "sym-root", View: view,
		}, store)
		if ErrorCode(err) != CodeInvalidArgument {
			t.Fatalf("view %q error code = %q, want %q", view, ErrorCode(err), CodeInvalidArgument)
		}
	}
}

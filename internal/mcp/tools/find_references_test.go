package tools

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// TestReferenceEdgeKindCodesMatchTheReferenceVocabulary keeps the traversal
// default in step with the row builders. They are two lists of the same thing:
// what a walk may follow, and what a row may carry. When METHOD_OF was added to
// the graph the second one refused it and the first one did not exist, so the
// walk reached an edge no row could describe and the query failed. A kind added
// to one list and not the other brings that back.
func TestReferenceEdgeKindCodesMatchTheReferenceVocabulary(t *testing.T) {
	walked := map[uint8]struct{}{}
	for _, code := range referenceEdgeKindCodes() {
		walked[code] = struct{}{}
	}
	seen := 0
	for code := 0; code <= 255; code++ {
		kind, err := facts.EdgeKindFromCode(uint8(code))
		if err != nil {
			continue
		}
		seen++
		_, isWalked := walked[uint8(code)]
		if isReferenceEdgeKind(kind) != isWalked {
			t.Errorf("kind %q (code %d): isReferenceEdgeKind = %v, walked by default = %v",
				kind, code, isReferenceEdgeKind(kind), isWalked)
		}
	}
	if seen < len(walked) {
		t.Fatalf("the vocabulary decodes %d kinds but the default walks %d", seen, len(walked))
	}
}

func TestFindReferencesDirectionsFiltersAndPagination(t *testing.T) {
	client := newFindReferencesToolClient(t, referenceSnapshot(t, 31))

	first := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming, "limit": 1,
	})
	if first.Total != 2 || first.Returned != 1 || !first.Truncated || first.NextCursor == nil {
		t.Fatalf("first incoming page = %#v", first)
	}
	if first.Coverage != (Coverage{Exact: 1, Candidate: 1}) {
		t.Fatalf("incoming coverage = %#v", first.Coverage)
	}
	// The subject is stated once, not on every row, and each row names the
	// other end so the agent can open it without a second call.
	if first.Results.Subject.QualifiedName != "pkg.Target" || first.Results.Subject.Name != "target" {
		t.Fatalf("subject = %#v", first.Results.Subject)
	}
	if first.Results.Direction != FindReferencesDirectionIncoming {
		t.Fatalf("direction = %q", first.Results.Direction)
	}
	caller := first.Results.References[0]
	if caller.QualifiedName != "pkg.CallerA" || caller.Name != "callerA" || caller.Kind != "function" {
		t.Fatalf("first incoming result = %#v", caller)
	}
	if caller.EdgeKind != string(facts.References) || caller.Confidence != string(facts.ExactTypechecked) {
		t.Fatalf("first incoming relation = %#v", caller)
	}
	// The concise row omits the evidence kind, the stable key and the derived
	// keys: a caller addresses the next call with repository, path and
	// qualified name, all of which the row carries.
	if caller.EvidenceKind != "" || caller.StableKey != "" || caller.FileKey != "" || caller.RepositoryKey != "" {
		t.Fatalf("concise row carries derived fields = %#v", caller)
	}
	if caller.Language != "go" || caller.FilePath != "src/caller.go" || caller.Repository != "repo-a" {
		t.Fatalf("first incoming location = %#v", caller)
	}
	// The row has to be openable as it stands. Without EndLine the agent pays
	// one get_symbol per row before it can read anything: 15 of those over the
	// six questions of benchmarks/mcp-token-cost.
	if caller.StartLine == 0 || caller.EndLine < caller.StartLine {
		t.Fatalf("first incoming range = %d-%d, want a declaration range", caller.StartLine, caller.EndLine)
	}

	detailed := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming, "limit": 1,
		"response_format": ResponseFormatDetailed,
	})
	if detailed.Results.References[0].EvidenceKind != "checker" ||
		detailed.Results.References[0].FileKey != "file-a" ||
		detailed.Results.References[0].RepositoryKey != "repository:repo-a" {
		t.Fatalf("detailed row = %#v", detailed.Results.References[0])
	}

	second := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming,
		"limit": 1, "cursor": *first.NextCursor,
	})
	if second.Total != 2 || second.Returned != 1 || second.Truncated || second.NextCursor != nil {
		t.Fatalf("second incoming page = %#v", second)
	}
	if second.Results.References[0].QualifiedName != "pkg.CallerB" || second.Results.References[0].EdgeKind != string(facts.CallsDirect) {
		t.Fatalf("second incoming result = %#v", second.Results.References[0])
	}

	outgoing := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionOutgoing,
	})
	if outgoing.Total != 1 || outgoing.Returned != 1 || outgoing.Results.References[0].QualifiedName != "pkg.Result" {
		t.Fatalf("outgoing result = %#v", outgoing)
	}
	if outgoing.Results.Subject.QualifiedName != "pkg.Target" {
		t.Fatalf("outgoing subject = %#v", outgoing.Results.Subject)
	}

	filtered := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming,
		"repo": "repo-b", "language": "typescript", "edge_kinds": []string{string(facts.CallsDirect)},
		"confidence": string(facts.Candidate),
	})
	if filtered.Total != 1 || filtered.Returned != 1 || filtered.Results.References[0].QualifiedName != "pkg.CallerB" {
		t.Fatalf("filtered result = %#v", filtered)
	}
}

func TestFindReferencesClassifiesInvalidInputAndMissingSnapshot(t *testing.T) {
	store := referenceSnapshot(t, 32)
	cases := []struct {
		name      string
		arguments FindReferencesInput
		store     *hotsnapshot.SnapshotStore
		wantCode  string
	}{
		{name: "empty key", arguments: FindReferencesInput{}, store: store, wantCode: CodeInvalidArgument},
		{name: "unsupported direction", arguments: FindReferencesInput{StableKey: "symbol-target", Direction: "sideways"}, store: store, wantCode: CodeInvalidArgument},
		{name: "invalid edge kind", arguments: FindReferencesInput{StableKey: "symbol-target", EdgeKinds: []string{"DEFINES"}}, store: store, wantCode: CodeInvalidArgument},
		{name: "invalid confidence", arguments: FindReferencesInput{StableKey: "symbol-target", Confidence: "MAYBE"}, store: store, wantCode: CodeInvalidArgument},
		{name: "invalid limit", arguments: FindReferencesInput{StableKey: "symbol-target", Limit: MaximumReferenceLimit + 1}, store: store, wantCode: CodeInvalidArgument},
		{name: "missing symbol", arguments: FindReferencesInput{StableKey: "symbol-missing"}, store: store, wantCode: CodeSymbolNotFound},
		{name: "missing snapshot", arguments: FindReferencesInput{StableKey: "symbol-target"}, store: hotsnapshot.NewSnapshotStore(nil), wantCode: CodeIndexNotReady},
		{name: "missing store", arguments: FindReferencesInput{StableKey: "symbol-target"}, wantCode: CodeIndexNotReady},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := findReferences(context.Background(), nil, test.arguments, test.store)
			if got := ErrorCode(err); got != test.wantCode {
				t.Fatalf("error code = %q, want %q (err=%v)", got, test.wantCode, err)
			}
		})
	}
}

func TestFindReferencesIsRegisteredReadOnly(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterFindReferences(server)
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
		if tool.Name == findReferencesToolName {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("find_references annotations = %#v, want read-only", tool.Annotations)
			}
			return
		}
	}
	t.Fatal("find_references is not registered")
}

// callFindReferences decodes the typed page, which only the full view
// produces: the compact views are asserted over the wire in
// TestFindReferencesCompactViewHoistsSharedColumns.
func callFindReferences(t *testing.T, client *sdkmcp.ClientSession, arguments map[string]any) Response[ReferenceResult] {
	t.Helper()
	if _, chosen := arguments["view"]; !chosen {
		arguments["view"] = ViewFull
	}
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: findReferencesToolName, Arguments: arguments})
	if err != nil {
		t.Fatalf("find_references CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("find_references CallTool() returned an error: %#v", result.Content)
	}
	response := decodeResponse[ReferenceResult](t, result)
	return response
}

// callFindReferencesWire returns the payload as it travels, which is the only
// way to assert what a view removed.
func callFindReferencesWire(t *testing.T, client *sdkmcp.ClientSession, arguments map[string]any) string {
	t.Helper()
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: findReferencesToolName, Arguments: arguments})
	if err != nil {
		t.Fatalf("find_references CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("find_references CallTool() returned an error: %#v", result.Content)
	}
	return contentText(result)
}

func newFindReferencesToolClient(t *testing.T, store *hotsnapshot.SnapshotStore) *sdkmcp.ClientSession {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterFindReferencesWithSnapshotStore(server, store)
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
	return clientSession
}

func referenceSnapshot(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	code := func(value uint8) uint8 { return value }
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repository:repo-a", Name: "repo-a", Path: "/repo-a", Languages: "go"},
			{Key: "repository:repo-b", Name: "repo-b", Path: "/repo-b", Languages: "typescript"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "package-a", RepositoryKey: "repository:repo-a", Name: "pkg-a", ModulePath: "example.com/a"},
			{Key: "package-b", RepositoryKey: "repository:repo-b", Name: "pkg-b", ModulePath: "example.com/b"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-a", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "src/caller.go", Language: "go"},
			{Key: "file-b", RepositoryKey: "repository:repo-b", PackageKey: "package-b", Path: "src/caller.ts", Language: "typescript"},
			{Key: "file-result", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "src/result.go", Language: "go"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "symbol-caller-a", CanonicalIdentity: "go:caller-a", FileKey: "file-a", Language: "go", Name: "callerA", QualifiedName: "pkg.CallerA", Kind: "function", StartLine: 12, EndLine: 20},
			{StableKey: "symbol-caller-b", CanonicalIdentity: "ts:caller-b", FileKey: "file-b", Language: "typescript", Name: "callerB", QualifiedName: "pkg.CallerB", Kind: "function", StartLine: 4, EndLine: 9},
			{StableKey: "symbol-result", CanonicalIdentity: "go:result", FileKey: "file-result", Language: "go", Name: "result", QualifiedName: "pkg.Result", Kind: "function", StartLine: 30, EndLine: 34},
			{StableKey: "symbol-target", CanonicalIdentity: "go:target", FileKey: "file-a", Language: "go", Name: "target", QualifiedName: "pkg.Target", Kind: "function", StartLine: 40, EndLine: 55},
		},
		Edges: []hotsnapshot.EdgeRow{
			{SourceKey: "symbol-caller-a", TargetKey: "symbol-target", Kind: code(mustFactsEdgeCode(t, facts.References)), Confidence: code(mustFactsConfidenceCode(t, facts.ExactTypechecked)), Provenance: code(mustFactsProvenanceCode(t, facts.GoTypesUse)), EvidenceKind: "checker", EvidenceSourceFileKey: "file-a", EvidenceTargetFileKey: "file-a"},
			{SourceKey: "symbol-caller-b", TargetKey: "symbol-target", Kind: code(mustFactsEdgeCode(t, facts.CallsDirect)), Confidence: code(mustFactsConfidenceCode(t, facts.Candidate)), Provenance: code(mustFactsProvenanceCode(t, facts.GoASTCall)), EvidenceKind: "checker", EvidenceSourceFileKey: "file-b", EvidenceTargetFileKey: "file-a"},
			// An outgoing edge from a function is a call, not an export: a real
			// EXPORTS names an export binding as its source and the declaration
			// as its target, so it only ever reaches a declaration from outside.
			{SourceKey: "symbol-target", TargetKey: "symbol-result", Kind: code(mustFactsEdgeCode(t, facts.CallsDirect)), Confidence: code(mustFactsConfidenceCode(t, facts.ExactTypechecked)), Provenance: code(mustFactsProvenanceCode(t, facts.GoTypesUse)), EvidenceKind: "checker", EvidenceSourceFileKey: "file-a", EvidenceTargetFileKey: "file-result"},
		},
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, id, time.Unix(1_700_000_000+int64(id), 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// manyCallersSnapshot has one target referenced by five callers: four
// functions across two files, and one export in a third. Every edge shares
// direction, confidence and provenance, so only `kind` disagrees -- the shape
// that forces grouping while keeping the rest of the header intact.
func manyCallersSnapshot(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	code := func(value uint8) uint8 { return value }
	symbols := []hotsnapshot.SymbolRow{
		{StableKey: "symbol-target", CanonicalIdentity: "go:target", FileKey: "file-target", Language: "go", Name: "target", QualifiedName: "pkg.Target", Kind: "function", StartLine: 1, EndLine: 3},
		{StableKey: "symbol-caller-1", CanonicalIdentity: "go:caller-1", FileKey: "file-a", Language: "go", Name: "caller1", QualifiedName: "pkg.Caller1", Kind: "function", StartLine: 10, EndLine: 12},
		{StableKey: "symbol-caller-2", CanonicalIdentity: "go:caller-2", FileKey: "file-a", Language: "go", Name: "caller2", QualifiedName: "pkg.Caller2", Kind: "function", StartLine: 20, EndLine: 22},
		{StableKey: "symbol-caller-3", CanonicalIdentity: "go:caller-3", FileKey: "file-a", Language: "go", Name: "caller3", QualifiedName: "pkg.Caller3", Kind: "function", StartLine: 30, EndLine: 32},
		{StableKey: "symbol-caller-4", CanonicalIdentity: "go:caller-4", FileKey: "file-b", Language: "go", Name: "caller4", QualifiedName: "pkg.Caller4", Kind: "function", StartLine: 5, EndLine: 7},
		{StableKey: "symbol-caller-5", CanonicalIdentity: "go:caller-5", FileKey: "file-c", Language: "go", Name: "caller5", QualifiedName: "pkg.Caller5", Kind: "export", StartLine: 1, EndLine: 1},
	}
	edges := make([]hotsnapshot.EdgeRow, 0, 5)
	for _, caller := range []hotsnapshot.StableKey{"symbol-caller-1", "symbol-caller-2", "symbol-caller-3", "symbol-caller-4", "symbol-caller-5"} {
		edges = append(edges, hotsnapshot.EdgeRow{
			SourceKey: caller, TargetKey: "symbol-target",
			Kind:         code(mustFactsEdgeCode(t, facts.CallsDirect)),
			Confidence:   code(mustFactsConfidenceCode(t, facts.ExactTypechecked)),
			Provenance:   code(mustFactsProvenanceCode(t, facts.GoASTCall)),
			EvidenceKind: "checker", EvidenceSourceFileKey: fileOf(symbols, caller), EvidenceTargetFileKey: "file-target",
		})
	}
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repository:repo-a", Name: "repo-a", Path: "/repo-a", Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "package-a", RepositoryKey: "repository:repo-a", Name: "pkg-a", ModulePath: "example.com/a"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-target", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "src/target.go", Language: "go"},
			{Key: "file-a", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "src/a.go", Language: "go"},
			{Key: "file-b", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "src/b.go", Language: "go"},
			{Key: "file-c", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "src/c.go", Language: "go"},
		},
		Symbols: symbols,
		Edges:   edges,
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, id, time.Unix(1_700_000_000+int64(id), 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

func fileOf(symbols []hotsnapshot.SymbolRow, stableKey hotsnapshot.StableKey) string {
	for _, symbol := range symbols {
		if symbol.StableKey == stableKey {
			return symbol.FileKey
		}
	}
	return ""
}

func mustFactsEdgeCode(t *testing.T, kind facts.EdgeKind) uint8 {
	t.Helper()
	code, err := kind.Code()
	if err != nil {
		t.Fatalf("edge kind code: %v", err)
	}
	return code
}

func mustFactsConfidenceCode(t *testing.T, confidence facts.Confidence) uint8 {
	t.Helper()
	code, err := confidence.Code()
	if err != nil {
		t.Fatalf("confidence code: %v", err)
	}
	return code
}

func mustFactsProvenanceCode(t *testing.T, provenance facts.Provenance) uint8 {
	t.Helper()
	code, err := provenance.Code()
	if err != nil {
		t.Fatalf("provenance code: %v", err)
	}
	return code
}

// TestFindReferencesAddressesASymbolWithoutItsKey is the contract that makes it
// affordable for every list response to withhold stable keys: a caller must be
// able to name a symbol with the repository, path and qualified name it just
// read, and get the same answer the key gives.
func TestFindReferencesAddressesASymbolWithoutItsKey(t *testing.T) {
	client := newFindReferencesToolClient(t, referenceSnapshot(t, 33))

	byKey := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming,
	})
	byTriple := callFindReferences(t, client, map[string]any{
		"repository": "repo-a", "path": "src/caller.go", "qualified_name": "pkg.Target",
		"direction": FindReferencesDirectionIncoming,
	})
	if byTriple.Total != byKey.Total || byTriple.Results.Subject.QualifiedName != byKey.Results.Subject.QualifiedName {
		t.Fatalf("triple answered %#v, want the same subject and total as the key: %#v", byTriple.Results, byKey.Results)
	}
	// The qualified name alone is enough when it is unique in the graph.
	byName := callFindReferences(t, client, map[string]any{
		"qualified_name": "pkg.Target", "direction": FindReferencesDirectionIncoming,
	})
	if byName.Total != byKey.Total {
		t.Fatalf("qualified name answered %d references, want %d", byName.Total, byKey.Total)
	}
}

// TestFindReferencesRejectsContradictoryAndAmbiguousSelectors covers the two
// ways a selector fails. Neither may be resolved quietly: picking one of two
// symbols with the same name is the nominal coincidence the graph forbids.
func TestFindReferencesRejectsContradictoryAndAmbiguousSelectors(t *testing.T) {
	client := newFindReferencesToolClient(t, referenceSnapshot(t, 34))

	for name, arguments := range map[string]map[string]any{
		"both selectors": {"stable_key": "symbol-target", "qualified_name": "pkg.Target"},
		"key narrowed":   {"stable_key": "symbol-target", "repository": "repo-a"},
		"path alone":     {"qualified_name": "pkg.Target", "path": "src/caller.go"},
		"no selector":    {"direction": FindReferencesDirectionIncoming},
	} {
		result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name: findReferencesToolName, Arguments: arguments,
		})
		if err != nil {
			t.Fatalf("%s: CallTool() error = %v", name, err)
		}
		if !result.IsError {
			t.Fatalf("%s: result = %#v, want a classified argument error", name, result)
		}
	}

	// A name the narrowing excluded is not the same answer as a name nobody
	// declares, and the message says which one happened.
	missing, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      findReferencesToolName,
		Arguments: map[string]any{"repository": "repo-a", "path": "src/result.go", "qualified_name": "pkg.Target"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !missing.IsError || !strings.Contains(contentText(missing), "search the whole graph") {
		t.Fatalf("narrowed miss = %#v, want a message naming the wider search", missing.Content)
	}
}

func contentText(result *sdkmcp.CallToolResult) string {
	for _, content := range result.Content {
		if text, ok := content.(*sdkmcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}

// TestFindReferencesCompactViewHoistsSharedColumns is the token contract of
// ADR 0046: the default view states what every row shares once. Over `workspace`,
// confidence and provenance alone were 1.200 of the 4.236 tokens of one page.
func TestFindReferencesCompactViewHoistsSharedColumns(t *testing.T) {
	client := newFindReferencesToolClient(t, referenceSnapshot(t, 35))

	// No view argument: compact is the default.
	compact := callFindReferencesWire(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming, "limit": 1,
	})
	var payload struct {
		Total    int  `json:"total"`
		Returned int  `json:"returned"`
		AgeMS    *int `json:"snapshot_age_ms"`
		Results  struct {
			Subject    string `json:"subject"`
			QN         string `json:"qn"`
			Repository string `json:"repository"`
			Kind       string `json:"kind"`
			EdgeKind   string `json:"edge_kind"`
			Confidence string `json:"confidence"`
			Provenance string `json:"provenance"`
			Files      []struct {
				File string `json:"file"`
				At   []any  `json:"at"`
			} `json:"files"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(compact), &payload); err != nil {
		t.Fatalf("unmarshal compact page: %v (%s)", err, compact)
	}
	if payload.Total != 2 || payload.Returned != 1 {
		t.Fatalf("compact counts = %d/%d, want 2/1", payload.Total, payload.Returned)
	}
	// The envelope drops what carried nothing: the age nobody asked for.
	if payload.AgeMS != nil {
		t.Fatalf("compact envelope carries snapshot_age_ms = %v", *payload.AgeMS)
	}
	if payload.Results.Subject != "repo-a:src/caller.go:40" || payload.Results.QN != "pkg.Target" {
		t.Fatalf("compact subject = %q %q", payload.Results.Subject, payload.Results.QN)
	}
	// One row, so every column hoists, and the confidence of the fact stays
	// readable: a compact view spells the same edge, never a weaker one.
	if payload.Results.Repository != "repo-a" || payload.Results.Kind != "function" ||
		payload.Results.EdgeKind != string(facts.References) ||
		payload.Results.Confidence != string(facts.ExactTypechecked) ||
		payload.Results.Provenance != string(facts.GoTypesUse) {
		t.Fatalf("compact header = %#v", payload.Results)
	}
	if len(payload.Results.Files) != 1 || payload.Results.Files[0].File != "src/caller.go" {
		t.Fatalf("compact files = %#v", payload.Results.Files)
	}
	if len(payload.Results.Files[0].At) != 1 || payload.Results.Files[0].At[0] != "pkg.CallerA@12-20" {
		t.Fatalf("compact entries = %#v, want the caller with its declaration range", payload.Results.Files[0].At)
	}
	// The row still has to be openable as it stands: dropping the end of the
	// range costs one get_symbol per row, which is what the range is for.
	resultsOnly, _ := json.Marshal(payload.Results)
	if strings.Contains(string(resultsOnly), "stable_key") || strings.Contains(string(resultsOnly), "language") {
		t.Fatalf("compact page carries derived or deducible fields: %s", resultsOnly)
	}

	// Two rows that disagree on everything: grouping them would cost one
	// object per row for zero repeated tuples, so the page stays flat and each
	// row carries its own tail -- exactly what a page with no shared tuple
	// looked like before grouping existed.
	both := callFindReferencesWire(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming,
	})
	var flat struct {
		Results struct {
			Confidence string                  `json:"confidence"`
			Repository string                  `json:"repository"`
			Files      []compactReferenceFile  `json:"files"`
			Groups     []compactReferenceGroup `json:"groups"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(both), &flat); err != nil {
		t.Fatalf("unmarshal second compact page: %v", err)
	}
	if flat.Results.Confidence != "" || flat.Results.Repository != "" {
		t.Fatalf("disagreeing rows hoisted to the page anyway: %#v", flat.Results)
	}
	if flat.Results.Groups != nil {
		t.Fatalf("page grouped two singleton tuples instead of staying flat: %#v", flat.Results.Groups)
	}
	if len(flat.Results.Files) != 2 {
		t.Fatalf("files = %#v, want one entry per file", flat.Results.Files)
	}
	for _, file := range flat.Results.Files {
		if _, isArray := file.At[0].([]any); !isArray {
			t.Fatalf("file = %#v, want a row tail since nothing hoisted", file)
		}
	}
	if !strings.Contains(both, string(facts.Candidate)) || !strings.Contains(both, string(facts.ExactTypechecked)) {
		t.Fatalf("compact page lost a confidence: %s", both)
	}
}

// TestFindReferencesCompactGroupsTheMajorityTupleOnce is the regression guard
// for the case that made this shape necessary: a page where most rows share
// one (kind, edge_kind) pair and a minority carries a different one. A single
// dissenting row used to keep both columns off the header and repeat them on
// every row; grouping states each tuple once and leaves the majority's rows
// bare.
func TestFindReferencesCompactGroupsTheMajorityTupleOnce(t *testing.T) {
	client := newFindReferencesToolClient(t, manyCallersSnapshot(t, 40))

	wire := callFindReferencesWire(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming, "limit": 500,
	})
	var payload struct {
		Total   int `json:"total"`
		Results struct {
			EdgeKind string                  `json:"edge_kind"`
			Files    []any                   `json:"files"`
			Groups   []compactReferenceGroup `json:"groups"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(wire), &payload); err != nil {
		t.Fatalf("unmarshal compact page: %v (%s)", err, wire)
	}
	if payload.Total != 5 {
		t.Fatalf("total = %d, want 5", payload.Total)
	}
	// edge_kind is uniform (every row calls directly), so it hoists to the
	// page even though kind does not -- the two columns hoist independently.
	if payload.Results.EdgeKind != string(facts.CallsDirect) {
		t.Fatalf("edge_kind = %q, want it hoisted to the page", payload.Results.EdgeKind)
	}
	if payload.Results.Files != nil {
		t.Fatalf("page emitted flat files although kind disagreed: %#v", payload.Results.Files)
	}
	if len(payload.Results.Groups) != 2 {
		t.Fatalf("groups = %#v, want exactly two kinds", payload.Results.Groups)
	}
	var majority, minority compactReferenceGroup
	for _, group := range payload.Results.Groups {
		if group.EdgeKind != "" {
			t.Fatalf("group repeats the page-hoisted edge_kind: %#v", group)
		}
		if group.Kind == "function" {
			majority = group
		} else {
			minority = group
		}
	}
	if majority.Kind != "function" || minority.Kind != "export" {
		t.Fatalf("groups = %+v / %+v, want kinds function and export", majority, minority)
	}
	majorityRows := 0
	for _, file := range majority.Files {
		majorityRows += len(file.At)
		for _, entry := range file.At {
			if _, isArray := entry.([]any); isArray {
				t.Fatalf("a row inside a fully-hoisted group still carries a tail: %#v", entry)
			}
		}
	}
	if majorityRows != 4 {
		t.Fatalf("majority group rows = %d, want 4", majorityRows)
	}
	minorityRows := 0
	for _, file := range minority.Files {
		minorityRows += len(file.At)
	}
	if minorityRows != 1 {
		t.Fatalf("minority group rows = %d, want 1", minorityRows)
	}
}

// TestFindReferencesFilesViewAnswersWhichFiles covers the cheapest granularity:
// which files hold the fact, and how many each holds.
func TestFindReferencesFilesViewAnswersWhichFiles(t *testing.T) {
	client := newFindReferencesToolClient(t, referenceSnapshot(t, 36))

	wire := callFindReferencesWire(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming, "view": ViewFiles,
	})
	var payload struct {
		Total   int `json:"total"`
		Results struct {
			Files []struct {
				File  string `json:"file"`
				Count int    `json:"count"`
			} `json:"files"`
		} `json:"results"`
	}
	if err := json.Unmarshal([]byte(wire), &payload); err != nil {
		t.Fatalf("unmarshal files page: %v (%s)", err, wire)
	}
	if payload.Total != 2 || len(payload.Results.Files) != 2 {
		t.Fatalf("files page = %#v", payload.Results.Files)
	}
	for _, file := range payload.Results.Files {
		if file.Count != 1 || (file.File != "repo-a/src/caller.go" && file.File != "repo-b/src/caller.ts") {
			t.Fatalf("files row = %#v", file)
		}
	}
	// The guard is the per-row column, spelled as its own key. A page-level
	// `edge_kinds_default_excluded` is about the query, not about one edge, and
	// a substring match would have caught it.
	if strings.Contains(wire, `"edge_kind"`) {
		t.Fatalf("files view carries per-edge fields: %s", wire)
	}

	unsupported, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      findReferencesToolName,
		Arguments: map[string]any{"stable_key": "symbol-target", "view": "summary"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !unsupported.IsError {
		t.Fatalf("unknown view = %#v, want a classified argument error", unsupported)
	}
}

// TestFindReferencesResolvesAnUnqualifiedName is the second call this surface
// used to charge for: with one declaration of the name, the answer needs no
// find_symbol first; with several, the candidates are named and nothing is
// resolved quietly.
func TestFindReferencesResolvesAnUnqualifiedName(t *testing.T) {
	client := newFindReferencesToolClient(t, referenceSnapshot(t, 37))

	byName := callFindReferences(t, client, map[string]any{
		"name": "target", "direction": FindReferencesDirectionIncoming,
	})
	byKey := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming,
	})
	if byName.Total != byKey.Total || byName.Results.Subject.QualifiedName != "pkg.Target" {
		t.Fatalf("name answered %#v, want the same page as the key: %#v", byName.Results, byKey.Results)
	}

	for name, arguments := range map[string]map[string]any{
		"name with a key":            {"name": "target", "stable_key": "symbol-target"},
		"name with a qualified name": {"name": "target", "qualified_name": "pkg.Target"},
		"path without repository":    {"name": "target", "path": "src/caller.go"},
		"name nobody declares":       {"name": "absent"},
	} {
		result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name: findReferencesToolName, Arguments: arguments,
		})
		if err != nil {
			t.Fatalf("%s: CallTool() error = %v", name, err)
		}
		if !result.IsError {
			t.Fatalf("%s: result = %#v, want a classified error", name, result)
		}
	}
}

// A caller that means "the subject is declared here" reaches for `repo`, which
// is four characters from `repository` and filters the answer instead. Measured
// on a real session: the model passed `repo` with a path, was told the path
// needed `repository`, and spent another call saying the same thing with the
// other name.
//
// The pair is the contract. Only the message for a caller who passed `repo`
// carries the correction, so the plain one cannot drift into explaining a
// mistake nobody made.
func TestFindReferencesNamesTheRepoFilterWhenItWasMeantAsTheSubject(t *testing.T) {
	store := referenceSnapshot(t, 1)

	_, _, err := findReferences(context.Background(), nil, FindReferencesInput{
		Name: "target", Repo: "alpha-repo", Path: "internal/facts/facts.go",
	}, store)
	if got := ErrorCode(err); got != CodeInvalidArgument {
		t.Fatalf("error code = %q, want %q", got, CodeInvalidArgument)
	}
	if !strings.Contains(err.Error(), "repo only filters which repositories") {
		t.Fatalf("error = %q, want the filter named", err)
	}

	_, _, err = findReferences(context.Background(), nil, FindReferencesInput{
		Name: "target", Path: "internal/facts/facts.go",
	}, store)
	if !strings.Contains(err.Error(), "requires repository") {
		t.Fatalf("error = %q, want the requirement stated", err)
	}
	if strings.Contains(err.Error(), "repo only filters") {
		t.Fatalf("error = %q, want no correction when repo was not passed", err)
	}
}

// forwardingSnapshot is the real cross-package shape the `workspace` measurement
// found: a declaration, its own export binding, a barrel re-exporting it from
// another file, and one consumer that imports it and calls it. The consumer
// resolves to the declaration and not to the barrel, which is why dropping the
// forwarding bindings loses no caller.
func forwardingSnapshot(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	code := func(value uint8) uint8 { return value }
	symbols := []hotsnapshot.SymbolRow{
		{StableKey: "symbol-decl", CanonicalIdentity: "ts:decl", FileKey: "file-decl", Language: "typescript", Name: "withRetry", QualifiedName: "withRetry", Kind: "function", StartLine: 135, EndLine: 163},
		{StableKey: "symbol-own-export", CanonicalIdentity: "ts:own-export", FileKey: "file-decl", Language: "typescript", Name: "withRetry", QualifiedName: "withRetry", Kind: "export", StartLine: 135, EndLine: 135},
		{StableKey: "symbol-barrel", CanonicalIdentity: "ts:barrel", FileKey: "file-barrel", Language: "typescript", Name: "withRetry", QualifiedName: "withRetry", Kind: "export", StartLine: 7, EndLine: 7},
		{StableKey: "symbol-import", CanonicalIdentity: "ts:import", FileKey: "file-consumer", Language: "typescript", Name: "withRetry", QualifiedName: "withRetry", Kind: "import", StartLine: 3, EndLine: 3},
		{StableKey: "symbol-caller", CanonicalIdentity: "ts:caller", FileKey: "file-consumer", Language: "typescript", Name: "run", QualifiedName: "BotWorker.run", Kind: "method", StartLine: 40, EndLine: 58},
	}
	edge := func(source hotsnapshot.StableKey, kind facts.EdgeKind, sourceFile string) hotsnapshot.EdgeRow {
		return hotsnapshot.EdgeRow{
			SourceKey: source, TargetKey: "symbol-decl",
			Kind:         code(mustFactsEdgeCode(t, kind)),
			Confidence:   code(mustFactsConfidenceCode(t, facts.ExactTypechecked)),
			Provenance:   code(mustFactsProvenanceCode(t, facts.TypeScriptChecker)),
			EvidenceKind: "checker", EvidenceSourceFileKey: sourceFile, EvidenceTargetFileKey: "file-decl",
		}
	}
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repository:provider", Name: "provider", Path: "/provider", Languages: "typescript"},
			{Key: "repository:consumer", Name: "consumer", Path: "/consumer", Languages: "typescript"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "package-provider", RepositoryKey: "repository:provider", Name: "@fixture/shared", ModulePath: "@fixture/shared"},
			{Key: "package-consumer", RepositoryKey: "repository:consumer", Name: "@fixture/core", ModulePath: "@fixture/core"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-decl", RepositoryKey: "repository:provider", PackageKey: "package-provider", Path: "src/utils/retry.ts", Language: "typescript"},
			{Key: "file-barrel", RepositoryKey: "repository:provider", PackageKey: "package-provider", Path: "src/index.ts", Language: "typescript"},
			{Key: "file-consumer", RepositoryKey: "repository:consumer", PackageKey: "package-consumer", Path: "src/BotWorker.ts", Language: "typescript"},
		},
		Symbols: symbols,
		Edges: []hotsnapshot.EdgeRow{
			edge("symbol-own-export", facts.Exports, "file-decl"),
			edge("symbol-barrel", facts.Reexports, "file-barrel"),
			edge("symbol-import", facts.ImportsSymbol, "file-consumer"),
			edge("symbol-caller", facts.CallsDirect, "file-consumer"),
		},
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, id, time.Unix(1_700_000_000+int64(id), 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// TestFindReferencesExcludesForwardingBindingsUnlessAsked defends the default:
// an unasked-for reference answer names the code that uses the symbol, not the
// export bindings that name a path to it, and it says so rather than returning
// a quietly filtered count.
func TestFindReferencesExcludesForwardingBindingsUnlessAsked(t *testing.T) {
	client := newFindReferencesToolClient(t, forwardingSnapshot(t, 71))

	byDefault := callFindReferences(t, client, map[string]any{
		"name": "withRetry", "repository": "provider", "path": "src/utils/retry.ts",
		"direction": FindReferencesDirectionIncoming, "view": ViewFull,
	})
	kinds := map[string]string{}
	for _, row := range byDefault.Results.References {
		kinds[row.EdgeKind] = row.Repository + ":" + row.FilePath
	}
	want := map[string]string{
		string(facts.ImportsSymbol): "consumer:src/BotWorker.ts",
		string(facts.CallsDirect):   "consumer:src/BotWorker.ts",
	}
	if !reflect.DeepEqual(want, kinds) {
		t.Fatalf("default answer = %#v, want %#v", kinds, want)
	}
	// The total counts what the answer holds: a filtered total that did not
	// say it was filtered would understate the references to the symbol.
	if byDefault.Total != 2 || byDefault.Returned != 2 {
		t.Fatalf("default totals = total %d returned %d, want 2 and 2", byDefault.Total, byDefault.Returned)
	}
	if !reflect.DeepEqual([]string{"EXPORTS", "REEXPORTS"}, byDefault.Results.EdgeKindsExcluded) {
		t.Fatalf("declared exclusion = %#v", byDefault.Results.EdgeKindsExcluded)
	}

	barrel := callFindReferences(t, client, map[string]any{
		"name": "withRetry", "repository": "provider", "path": "src/utils/retry.ts",
		"direction": FindReferencesDirectionIncoming, "view": ViewFull,
		"edge_kinds": []string{string(facts.Reexports)},
	})
	if barrel.Total != 1 || barrel.Results.References[0].FilePath != "src/index.ts" {
		t.Fatalf("opt-in answer = %#v", barrel.Results.References)
	}
	// An explicit selection declares nothing: the caller wrote the filter.
	if barrel.Results.EdgeKindsExcluded != nil {
		t.Fatalf("explicit selection declared an exclusion: %#v", barrel.Results.EdgeKindsExcluded)
	}

	everything := callFindReferences(t, client, map[string]any{
		"name": "withRetry", "repository": "provider", "path": "src/utils/retry.ts",
		"direction": FindReferencesDirectionIncoming, "view": ViewFull,
		"edge_kinds": []string{referenceEdgeKindAll},
	})
	if everything.Total != 4 || everything.Results.EdgeKindsExcluded != nil {
		t.Fatalf("wildcard answer = total %d excluded %#v", everything.Total, everything.Results.EdgeKindsExcluded)
	}
}

// TestFindReferencesRejectsWildcardMixedWithEdgeKinds keeps the wildcard alone:
// asking for everything and for a filter at once has no answer, and picking one
// of the two silently would report a set the caller did not ask for.
func TestFindReferencesRejectsWildcardMixedWithEdgeKinds(t *testing.T) {
	client := newFindReferencesToolClient(t, forwardingSnapshot(t, 72))
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: findReferencesToolName,
		Arguments: map[string]any{
			"name": "withRetry", "repository": "provider", "path": "src/utils/retry.ts",
			"edge_kinds": []string{referenceEdgeKindAll, string(facts.Reexports)},
		},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !result.IsError || !strings.Contains(contentText(result), CodeInvalidArgument) {
		t.Fatalf("mixed wildcard = %s", contentText(result))
	}
}

// dispatchSnapshot is the shape a dynamic call has in the graph: a caller that
// calls the interface method, an interface method, and the concrete methods
// that implement it. `sole` has one implementation; `shared` has two.
func dispatchSnapshot(t *testing.T, id uint64, sharedImplementations int) *hotsnapshot.SnapshotStore {
	t.Helper()
	code := func(value uint8) uint8 { return value }
	symbols := []hotsnapshot.SymbolRow{
		{StableKey: "iface-sole", CanonicalIdentity: "go:Store.Put", FileKey: "file-iface", Language: "go", Name: "Put", QualifiedName: "Store.Put", Kind: "method", StartLine: 4, EndLine: 4},
		{StableKey: "impl-sole", CanonicalIdentity: "go:Memory.Put", FileKey: "file-impl", Language: "go", Name: "Put", QualifiedName: "Memory.Put", Kind: "method", StartLine: 10, EndLine: 14},
		{StableKey: "caller-sole", CanonicalIdentity: "go:Handler.Save", FileKey: "file-caller", Language: "go", Name: "Save", QualifiedName: "Handler.Save", Kind: "method", StartLine: 30, EndLine: 36},
		{StableKey: "iface-shared", CanonicalIdentity: "go:Store.Get", FileKey: "file-iface", Language: "go", Name: "Get", QualifiedName: "Store.Get", Kind: "method", StartLine: 5, EndLine: 5},
		{StableKey: "impl-shared-a", CanonicalIdentity: "go:Memory.Get", FileKey: "file-impl", Language: "go", Name: "Get", QualifiedName: "Memory.Get", Kind: "method", StartLine: 16, EndLine: 20},
		{StableKey: "impl-shared-b", CanonicalIdentity: "go:Disk.Get", FileKey: "file-other", Language: "go", Name: "Get", QualifiedName: "Disk.Get", Kind: "method", StartLine: 8, EndLine: 12},
		{StableKey: "caller-shared", CanonicalIdentity: "go:Handler.Load", FileKey: "file-caller", Language: "go", Name: "Load", QualifiedName: "Handler.Load", Kind: "method", StartLine: 40, EndLine: 46},
	}
	edge := func(source, target hotsnapshot.StableKey, kind facts.EdgeKind, sourceFile, targetFile string) hotsnapshot.EdgeRow {
		return hotsnapshot.EdgeRow{
			SourceKey: source, TargetKey: target,
			Kind:         code(mustFactsEdgeCode(t, kind)),
			Confidence:   code(mustFactsConfidenceCode(t, facts.ExactTypechecked)),
			Provenance:   code(mustFactsProvenanceCode(t, facts.GoTypesUse)),
			EvidenceKind: "checker", EvidenceSourceFileKey: sourceFile, EvidenceTargetFileKey: targetFile,
		}
	}
	edges := []hotsnapshot.EdgeRow{
		// The call names the interface method, which is where the checker
		// resolves it, and the implementation carries no incoming edge at all.
		edge("caller-sole", "iface-sole", facts.CallsDirect, "file-caller", "file-iface"),
		edge("impl-sole", "iface-sole", facts.Implements, "file-impl", "file-iface"),
		edge("caller-shared", "iface-shared", facts.CallsDirect, "file-caller", "file-iface"),
		edge("impl-shared-a", "iface-shared", facts.Implements, "file-impl", "file-iface"),
	}
	if sharedImplementations > 1 {
		edges = append(edges, edge("impl-shared-b", "iface-shared", facts.Implements, "file-other", "file-iface"))
	}
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repository:repo-a", Name: "repo-a", Path: "/repo-a", Languages: "go"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "package-a", RepositoryKey: "repository:repo-a", Name: "pkg", ModulePath: "example.com/a"}},
		Files: []hotsnapshot.FileRow{
			{Key: "file-iface", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "store.go", Language: "go"},
			{Key: "file-impl", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "memory.go", Language: "go"},
			{Key: "file-other", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "disk.go", Language: "go"},
			{Key: "file-caller", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "handler.go", Language: "go"},
		},
		Symbols: symbols,
		Edges:   edges,
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, id, time.Unix(1_700_000_000+int64(id), 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// TestFindReferencesReachesTheSoleImplementationOfAnInterfaceMethod defends the
// bridge and, more importantly, its limit. A call through an interface resolves
// to the interface method, so a question about the implementation used to answer
// that nothing referenced it -- in the same words a real absence gets.
func TestFindReferencesReachesTheSoleImplementationOfAnInterfaceMethod(t *testing.T) {
	client := newFindReferencesToolClient(t, dispatchSnapshot(t, 81, 1))

	sole := callFindReferences(t, client, map[string]any{
		"qualified_name": "Memory.Put", "repository": "repo-a", "path": "memory.go",
		"direction": FindReferencesDirectionIncoming, "view": ViewFull,
	})
	if sole.Total != 1 || len(sole.Results.References) != 1 {
		t.Fatalf("sole implementation = total %d rows %d", sole.Total, len(sole.Results.References))
	}
	row := sole.Results.References[0]
	if row.FilePath != "handler.go" || row.QualifiedName != "Handler.Save" {
		t.Fatalf("bridged row = %#v", row)
	}
	// The row says how it got here: a caller of the interface method is not a
	// direct caller of the implementation, and passing it off as one would be
	// the same lie in the other direction.
	if row.Via != "Store.Put" {
		t.Fatalf("bridged row does not name the interface it came through: %#v", row)
	}
	if !reflect.DeepEqual([]string{"Store.Put"}, sole.Results.DispatchThrough) {
		t.Fatalf("page does not declare the dispatch: %#v", sole.Results.DispatchThrough)
	}
}

// TestFindReferencesWillNotGuessBetweenTwoImplementations is the other half. A
// call through an interface with two implementations reaches one of them, and
// naming both as called would trade a false absence for a false presence.
func TestFindReferencesWillNotGuessBetweenTwoImplementations(t *testing.T) {
	client := newFindReferencesToolClient(t, dispatchSnapshot(t, 82, 2))

	shared := callFindReferences(t, client, map[string]any{
		"qualified_name": "Memory.Get", "repository": "repo-a", "path": "memory.go",
		"direction": FindReferencesDirectionIncoming, "view": ViewFull,
	})
	if shared.Total != 0 || len(shared.Results.DispatchThrough) != 0 {
		t.Fatalf("two implementations were bridged: total %d dispatch %#v",
			shared.Total, shared.Results.DispatchThrough)
	}

	// With one implementation the same question is answered, which is what
	// makes the refusal above a decision rather than a missing feature.
	single := newFindReferencesToolClient(t, dispatchSnapshot(t, 83, 1))
	answered := callFindReferences(t, single, map[string]any{
		"qualified_name": "Memory.Get", "repository": "repo-a", "path": "memory.go",
		"direction": FindReferencesDirectionIncoming, "view": ViewFull,
	})
	if answered.Total != 1 {
		t.Fatalf("one implementation was not bridged: total %d", answered.Total)
	}
}

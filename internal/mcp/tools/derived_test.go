package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

// derivedSnapshot holds one registered repository that uses a symbol of the
// standard library, and the derived provider that declares it. Both names carry
// a symbol called `Clone`, which is the case the default filter exists for.
func derivedSnapshot(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	code := func(value uint8) uint8 { return value }
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repository:app", Name: "app", Path: "/app", Languages: "rust"},
			{Key: "repository:rust:1.96.1", Name: "rust:1.96.1", Path: "/toolchain/library", Languages: "rust"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "package:app", RepositoryKey: "repository:app", Name: "app", ModulePath: "app"},
			{Key: "package:core", RepositoryKey: "repository:rust:1.96.1", Name: "core", ModulePath: "core"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file:app", RepositoryKey: "repository:app", PackageKey: "package:app", Path: "src/lib.rs", Language: "rust"},
			{Key: "file:core", RepositoryKey: "repository:rust:1.96.1", PackageKey: "package:core", Path: "core/src/clone.rs", Language: "rust"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "symbol-app-clone", CanonicalIdentity: "rust:app.Clone", FileKey: "file:app", Language: "rust", Name: "Clone", QualifiedName: "app::Clone", Kind: "trait", StartLine: 3, EndLine: 5},
			{StableKey: "symbol-app-use", CanonicalIdentity: "rust:app.duplicate", FileKey: "file:app", Language: "rust", Name: "duplicate", QualifiedName: "app::duplicate", Kind: "func", StartLine: 10, EndLine: 14},
			{StableKey: "symbol-core-clone", CanonicalIdentity: "rust:core.Clone", FileKey: "file:core", Language: "rust", Name: "Clone", QualifiedName: "clone::Clone", Kind: "trait", StartLine: 100, EndLine: 140},
		},
		Edges: []hotsnapshot.EdgeRow{
			{SourceKey: "symbol-app-use", TargetKey: "symbol-core-clone",
				Kind:         code(mustFactsEdgeCode(t, facts.CallsDirect)),
				Confidence:   code(mustFactsConfidenceCode(t, facts.ExactTypechecked)),
				Provenance:   code(mustFactsProvenanceCode(t, facts.RustAnalyzerUse)),
				EvidenceKind: "types", EvidenceSourceFileKey: "file:app", EvidenceTargetFileKey: "file:core"},
		},
		Unresolved: []hotsnapshot.UnresolvedReferenceRow{
			{Key: "unresolved:app", RepositoryKey: "repository:app", FileKey: "file:app", Language: "rust",
				RequestedPackage: "serde", Reason: "CRATE_PROVIDER_NOT_FOUND"},
			{Key: "unresolved:core", RepositoryKey: "repository:rust:1.96.1", FileKey: "file:core", Language: "rust",
				RequestedPackage: "core", Reason: "DEFINITION_NOT_INDEXED"},
		},
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, id, time.Unix(1_700_000_000+int64(id), 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// derivedClient connects a session to a server carrying every tool this
// contract spans, so one fixture answers all five questions.
func derivedClient(t *testing.T, store *hotsnapshot.SnapshotStore) *sdkmcp.ClientSession {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterFindSymbolWithSnapshotStore(server, store)
	RegisterFindReferencesWithSnapshotStore(server, store)
	RegisterListRepositoriesWithSnapshotStore(server, store)
	RegisterGraphStatusWithSnapshotStore(server, store)
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

func callDerivedTool(t *testing.T, client *sdkmcp.ClientSession, name string, arguments map[string]any, out any) {
	t.Helper()
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		t.Fatalf("%s transport error = %v", name, err)
	}
	if result.IsError {
		t.Fatalf("%s returned an error: %v", name, contentText(result))
	}
	if err := json.Unmarshal([]byte(contentText(result)), out); err != nil {
		t.Fatalf("%s decode error = %v", name, err)
	}
}

// TestFindSymbolWithholdsTheDerivedProviderByDefault is the reason the filter
// exists: with the standard library in the graph, a search for a name it
// declares answers with it unless asked otherwise.
func TestFindSymbolWithholdsTheDerivedProviderByDefault(t *testing.T) {
	store := derivedSnapshot(t, 61)
	client := derivedClient(t, store)

	var withheld Response[[]SymbolSummary]
	callDerivedTool(t, client, "find_symbol", map[string]any{"name": "Clone"}, &withheld)
	if withheld.Total != 1 || len(withheld.Results) != 1 {
		t.Fatalf("default page = %#v, want only the repository's own symbol", withheld.Results)
	}
	if withheld.Results[0].QualifiedName != "app::Clone" {
		t.Fatalf("default result = %#v, want app::Clone", withheld.Results[0])
	}

	var asked Response[[]SymbolSummary]
	callDerivedTool(t, client, "find_symbol", map[string]any{"name": "Clone", "include_derived": true}, &asked)
	if asked.Total != 2 {
		t.Fatalf("include_derived total = %d, want both", asked.Total)
	}

	// Naming the provider is a request for it: a caller who spells the
	// repository out should not also need the flag.
	var named Response[[]SymbolSummary]
	callDerivedTool(t, client, "find_symbol", map[string]any{"name": "Clone", "repo": "rust:1.96.1"}, &named)
	if named.Total != 1 || named.Results[0].QualifiedName != "clone::Clone" {
		t.Fatalf("repo-named page = %#v, want the standard library's symbol", named.Results)
	}
}

// TestFindReferencesWithholdsDerivedRows covers the outgoing direction, where
// the row is the standard library symbol a repository reached.
func TestFindReferencesWithholdsDerivedRows(t *testing.T) {
	store := derivedSnapshot(t, 62)
	client := derivedClient(t, store)

	var withheld Response[ReferenceResult]
	callDerivedTool(t, client, "find_references", map[string]any{
		"stable_key": "symbol-app-use", "direction": FindReferencesDirectionOutgoing,
	}, &withheld)
	if withheld.Total != 0 || len(withheld.Results.References) != 0 {
		t.Fatalf("default page = %#v, want no derived rows", withheld.Results.References)
	}

	var asked Response[ReferenceResult]
	callDerivedTool(t, client, "find_references", map[string]any{
		"stable_key": "symbol-app-use", "direction": FindReferencesDirectionOutgoing,
		"include_derived": true,
	}, &asked)
	if asked.Total != 1 || asked.Results.References[0].QualifiedName != "clone::Clone" {
		t.Fatalf("include_derived page = %#v, want the standard library row", asked.Results.References)
	}
	// The edge is exact and stays exact: withholding a row is a page decision,
	// never a claim about what was observed.
	if asked.Results.References[0].Confidence != string(facts.ExactTypechecked) {
		t.Fatalf("derived row = %#v, want the exact confidence it was published with", asked.Results.References[0])
	}
}

// TestListRepositoriesMarksTheDerivedProvider states the difference a caller
// needs: nothing checks the standard library out, so it can neither be stale nor
// have moved, and reporting an unreadable commit would invent a problem.
func TestListRepositoriesMarksTheDerivedProvider(t *testing.T) {
	store := derivedSnapshot(t, 64)
	client := derivedClient(t, store)

	var listed Response[[]RepositorySummary]
	callDerivedTool(t, client, "list_repositories", map[string]any{}, &listed)
	if listed.Total != 2 {
		t.Fatalf("total = %d, want both repositories listed", listed.Total)
	}
	byName := make(map[string]RepositorySummary, len(listed.Results))
	for _, row := range listed.Results {
		byName[row.Name] = row
	}
	if byName["app"].Derived {
		t.Fatalf("app = %#v, want a registered repository", byName["app"])
	}
	derived := byName["rust:1.96.1"]
	if !derived.Derived || derived.Moved {
		t.Fatalf("derived = %#v, want it marked and never moved", derived)
	}
	if derived.MovedDetail == "" {
		t.Fatalf("derived = %#v, want the reason it has no commit", derived)
	}
}

// TestGraphStatusBreaksOutTheDerivedProvider is what keeps `symbols` readable
// once the standard library is in the graph.
func TestGraphStatusBreaksOutTheDerivedProvider(t *testing.T) {
	store := derivedSnapshot(t, 65)
	client := derivedClient(t, store)

	var status Response[GraphStatus]
	callDerivedTool(t, client, "graph_status", map[string]any{}, &status)
	if status.Results.Derived == nil {
		t.Fatalf("status = %#v, want the derived section", status.Results)
	}
	report := status.Results.Derived
	if len(report.Repositories) != 1 || report.Repositories[0] != "rust:1.96.1" {
		t.Fatalf("derived repositories = %#v", report.Repositories)
	}
	if report.Symbols != 1 || report.Packages != 1 || report.Files != 1 {
		t.Fatalf("derived counts = %#v, want one of each", report)
	}
	// The edge belongs to the repository that observed it, so it is inbound,
	// never the standard library's own.
	if report.EdgesInbound != 1 || report.EdgesWithin != 0 {
		t.Fatalf("derived edges = %#v, want one inbound", report)
	}
	// The library's own gaps are counted apart: `unresolved` above otherwise
	// answers «what is my code missing» with a number about the standard library.
	if report.Unresolved != 1 || status.Results.Unresolved != 2 {
		t.Fatalf("unresolved = %d derived of %d, want one of each",
			report.Unresolved, status.Results.Unresolved)
	}
	if status.Results.Symbols != 3 {
		t.Fatalf("total symbols = %d, want every symbol still counted", status.Results.Symbols)
	}
}

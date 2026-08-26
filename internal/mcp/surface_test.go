package mcp

import (
	"context"
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// allowedTools is the entire MCP surface Kivgraph is allowed to expose, from
// PLAN.md 17.1. This list is a contract, not a snapshot of the code: adding a
// tool without adding it here is the failure this file exists to catch.
//
// get_unresolved_references is deliberately absent. It answers a question about
// the index rather than about the code, no agent asks it, and every tool on this
// list costs description tokens in every request of every session. It remains
// available from the CLI.
var allowedTools = []string{
	"find_by_intent",
	"find_cross_repo_consumers",
	"find_references",
	"find_symbol",
	"get_blast_radius",
	"get_file_outline",
	"get_source",
	"get_symbol",
	"graph_status",
	"list_repositories",
	"trace_dependencies",
}

// forbiddenTools is the mutation surface excluded from the default server,
// because it is read-only over repositories it does not own. Query execution
// and indexing control are included: configured serve adds only the explicit
// consent-gated index_project tool.
var forbiddenTools = []string{
	"execute_cypher",
	"execute_query",
	"index_project",
	"index",
	"update",
	"refresh",
	"rebuild",
	"register_repository",
	"remove_repository",
	"edit_file",
	"edit",
	"run_command",
}

// surfaceSpecification is the protocol document that describes this surface.
// The path is relative to this package, which is what a Go test can rely on.
const surfaceSpecification = "../../docs/protocol/mcp-surface-v3.md"

// TestTheSpecificationNamesEveryToolThatIsServed closes LUQUE-2230 as a class.
//
// allowedTools is the contract inside the code, and it caught nothing about the
// document that describes the contract to a reader: find_by_intent shipped in
// serve while the specification enumerated ten tools and claimed eleven, and it
// was found by accident weeks later. A tool nobody documents is a tool nobody
// calls, so the two lists have to be one.
//
// The comparison is against the fenced block, not against a mention anywhere in
// the prose: a name that only appears in a sentence about what was retired must
// not satisfy this.
func TestTheSpecificationNamesEveryToolThatIsServed(t *testing.T) {
	document, err := os.ReadFile(surfaceSpecification)
	if err != nil {
		t.Fatalf("read %s: %v", surfaceSpecification, err)
	}
	listed := specificationToolBlock(t, string(document))
	documented := make(map[string]struct{}, len(listed))
	for _, name := range listed {
		documented[name] = struct{}{}
	}
	for _, name := range allowedTools {
		if _, found := documented[name]; !found {
			t.Errorf("%s does not list %q, which serve registers", surfaceSpecification, name)
		}
	}
	served := make(map[string]struct{}, len(allowedTools))
	for _, name := range allowedTools {
		served[name] = struct{}{}
	}
	for _, name := range listed {
		if _, found := served[name]; !found {
			t.Errorf("%s lists %q, which no server registers", surfaceSpecification, name)
		}
	}
	// The mutation is documented in prose rather than in the block, because a
	// client that never configures indexing never sees it.
	if !strings.Contains(string(document), "index_project") {
		t.Errorf("%s does not mention index_project", surfaceSpecification)
	}
}

// specificationToolBlock returns the tool names of the document's first fenced
// text block, which is the enumeration of the surface.
func specificationToolBlock(t *testing.T, document string) []string {
	t.Helper()
	const fence = "```text"
	start := strings.Index(document, fence)
	if start < 0 {
		t.Fatalf("%s has no fenced text block enumerating the tools", surfaceSpecification)
	}
	rest := document[start+len(fence):]
	end := strings.Index(rest, "```")
	if end < 0 {
		t.Fatalf("%s has an unterminated fenced block", surfaceSpecification)
	}
	names := strings.Fields(rest[:end])
	if len(names) == 0 {
		t.Fatalf("%s enumerates no tool", surfaceSpecification)
	}
	return names
}

func TestServerExposesExactlyTheAllowedSurface(t *testing.T) {
	names := listToolNames(t, publishedServer(t))
	if len(names) != len(allowedTools) {
		t.Fatalf("tools = %v, want exactly %v", names, allowedTools)
	}
	for index := range names {
		if names[index] != allowedTools[index] {
			t.Fatalf("tools = %v, want %v", names, allowedTools)
		}
	}
}

// MaximumSurfaceSchemaBytes bounds what a client loads before it can use this
// server at all. A harness loads tool schemas on demand and frequently never
// looks: the surface has to stay cheap enough to be worth looking at.
//
// The number is the measured cost with room to move, not an aspiration. It was
// `34.932` characters while every tool published the JSON Schema the SDK
// derives from its result type, and `4.768` once they published the object
// schema instead -- the input schemas, which are the half that tells a caller
// how to call, were only `2.530` of the original total.
const MaximumSurfaceSchemaBytes = 8000

func TestServerSurfaceStaysCheapToLoad(t *testing.T) {
	session := connectToServer(t, publishedServer(t))
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	encoded, err := json.Marshal(listed.Tools)
	if err != nil {
		t.Fatalf("Marshal tools: %v", err)
	}
	if len(encoded) > MaximumSurfaceSchemaBytes {
		t.Fatalf("published surface = %d bytes over %d tools, above the %d ceiling",
			len(encoded), len(listed.Tools), MaximumSurfaceSchemaBytes)
	}

	// No tool declares an output schema, and that is load-bearing rather than
	// tidy. The SDK fills `structuredContent` from the typed handler result
	// whenever a schema is present, and then repeats the same JSON in a text
	// block: the answer travels twice. Measured over the six questions of
	// `benchmarks/mcp-token-cost`, the duplicate was 24.066 bytes in one pass.
	for _, tool := range listed.Tools {
		if tool.OutputSchema != nil {
			t.Fatalf("%s publishes an output schema, which restores the duplicate structured channel", tool.Name)
		}
	}
}

// TestServerAnswersInOneChannel closes the loop the schema check opens: no
// declared schema is only a promise until a real call is inspected.
func TestServerAnswersInOneChannel(t *testing.T) {
	session := connectToServer(t, publishedServer(t))
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "graph_status"})
	if err != nil {
		t.Fatalf("graph_status CallTool() error = %v", err)
	}
	if result.StructuredContent != nil {
		t.Fatalf("graph_status carries structuredContent as well as text: %#v", result.StructuredContent)
	}
	if len(result.Content) != 1 {
		t.Fatalf("graph_status returned %d content blocks, want exactly one", len(result.Content))
	}
	if _, ok := result.Content[0].(*sdkmcp.TextContent); !ok {
		t.Fatalf("graph_status content block is %T, want text", result.Content[0])
	}
}

// TestServerExposesNoMutatingTool checks the forbidden list by substring, not
// by equality: a tool named "rebuild_graph" or "graph_index" would satisfy an
// equality check while breaking the same rule.
func TestServerExposesNoMutatingTool(t *testing.T) {
	for _, name := range listToolNames(t, publishedServer(t)) {
		for _, forbidden := range forbiddenTools {
			if name == forbidden || strings.Contains(name, forbidden) {
				t.Fatalf("tool %q matches forbidden name %q", name, forbidden)
			}
		}
	}
}

func TestServerAnnotatesEveryToolReadOnly(t *testing.T) {
	session := connectToServer(t, publishedServer(t))
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range listed.Tools {
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Fatalf("tool %q annotations = %#v, want ReadOnlyHint", tool.Name, tool.Annotations)
		}
		if tool.Annotations.DestructiveHint != nil && *tool.Annotations.DestructiveHint {
			t.Fatalf("tool %q is annotated destructive", tool.Name)
		}
	}
}

// TestServerRejectsForbiddenToolCalls closes the loop: absence from the listing
// is not enough, an unlisted name must also fail when called directly.
func TestServerRejectsForbiddenToolCalls(t *testing.T) {
	session := connectToServer(t, publishedServer(t))
	for _, name := range forbiddenTools {
		t.Run(name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
				Name:      name,
				Arguments: map[string]any{},
			})
			if err == nil && (result == nil || !result.IsError) {
				t.Fatalf("CallTool(%q) succeeded: %#v", name, result)
			}
		})
	}
}

func listToolNames(t *testing.T, server *sdkmcp.Server) []string {
	t.Helper()
	session := connectToServer(t, server)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	sort.Strings(names)
	return names
}

func connectToServer(t *testing.T, server *sdkmcp.Server) *sdkmcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "surface-test", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}

// publishedServer is a server with a graph to answer from. The surface contract
// is about what a working server exposes; a server with no published generation
// deliberately exposes nothing, which TestServerWithoutAGenerationPublishesNoTool
// covers instead.
func publishedServer(t *testing.T) *sdkmcp.Server {
	t.Helper()
	return NewServerWithSnapshotStore(publishedStore(t))
}

// publishedStore is the smallest store that counts as a published generation,
// which is all a surface test needs: the shape of the surface does not depend on
// what the graph holds.
func publishedStore(t *testing.T) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo-a", Name: "a", Languages: "go"}},
	}, 1, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// TestServerWithoutAGenerationPublishesNoTool is the fail-closed handshake. A
// client spawns this process, so exiting reads as a crash and says nothing;
// answering with tools that cannot answer teaches the agent that the tools do
// not work. Completing the handshake with no tools and a repair instruction is
// the only shape a client can act on.
func TestServerWithoutAGenerationPublishesNoTool(t *testing.T) {
	session := connectToServer(t, NewServer())
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(listed.Tools) != 0 {
		t.Fatalf("tools without a published generation = %v, want none", toolNames(listed.Tools))
	}
	initResult := session.InitializeResult()
	if initResult == nil {
		t.Fatal("InitializeResult() = nil, want a completed handshake")
	}
	if !strings.Contains(initResult.Instructions, "kivgraph index --full") {
		t.Fatalf("instructions = %q, want the command that repairs this", initResult.Instructions)
	}
}

// TestServerInstructionsRouteWithoutVolatileFacts guards the one string that
// survives every client's schema deferral. Claude Code truncates it at 2 KB, and
// anything derived from the graph would rewrite bytes of a cached system prompt
// on every re-index.
func TestServerInstructionsRouteWithoutVolatileFacts(t *testing.T) {
	session := connectToServer(t, publishedServer(t))
	initResult := session.InitializeResult()
	if initResult == nil {
		t.Fatal("InitializeResult() = nil")
	}
	instructions := initResult.Instructions
	if len(instructions) == 0 || len(instructions) > 2048 {
		t.Fatalf("instructions are %d bytes, want between 1 and 2048", len(instructions))
	}
	for _, want := range []string{"find_references", "get_source", "Where it loses", "Python", "Dart", "CANDIDATE"} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("instructions = %q, want them to mention %q", instructions, want)
		}
	}
	// A number that moves with the graph invalidates the client's prompt cache.
	for _, digit := range []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"} {
		if strings.Contains(instructions, digit) {
			t.Fatalf("instructions contain the digit %q; volatile facts belong in graph_status", digit)
		}
	}
}

func toolNames(listed []*sdkmcp.Tool) []string {
	names := make([]string, 0, len(listed))
	for _, tool := range listed {
		names = append(names, tool.Name)
	}
	return names
}

// MaximumResidentSurfaceBytes bounds what a host keeps resident for this server.
//
// Neither target host holds the JSON schemas: Oh My Pi mounts each tool as a
// device whose documentation is read on demand, and Claude Code defers schemas
// behind its tool search. What stays is the name, twice -- once as a route and
// once as a heading -- and the description. That is the whole budget in which the
// routing has to fit, so it is the number a regression is measured against.
//
// Bytes, not tokens: this package has no tokenizer, and the benchmark in
// benchmarks/mcp-token-cost measures the token figure the phase quotes. The two
// move together; this guards the drift.
const MaximumResidentSurfaceBytes = 1900

// MaximumIndexingSurfaceBytes bounds the other shape this server has. A client
// that configures indexing is handed one tool more -- the only one that can
// change the graph -- and it is budgeted on its own line rather than inside the
// number above, because a client that never configures it never pays for it.
//
// Both lines are guarded, and that is the point: the ceiling above was measured
// against a server built without an indexer, so the surface that actually ships
// through `kivgraph serve` was never on any scale.
const MaximumIndexingSurfaceBytes = 2100

func TestServerSurfaceStaysCheapToKeepResident(t *testing.T) {
	session := connectToServer(t, publishedServer(t))
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	resident := 0
	for _, tool := range listed.Tools {
		if tool.Description == "" {
			t.Fatalf("tool %q has no description; the description is the only routing a deferred surface carries", tool.Name)
		}
		resident += len(tool.Name)*2 + len(tool.Description)
	}
	if resident > MaximumResidentSurfaceBytes {
		t.Fatalf("resident surface = %d bytes over %d tools, above the %d ceiling",
			resident, len(listed.Tools), MaximumResidentSurfaceBytes)
	}
	// A description that carries a number derived from the graph rewrites bytes
	// of a cached system prompt on every re-index.
	for _, tool := range listed.Tools {
		for _, digit := range "0123456789" {
			if strings.ContainsRune(tool.Description, digit) {
				t.Fatalf("description of %q contains the digit %q; volatile facts belong in a call", tool.Name, digit)
			}
		}
	}
}

// TestASessionMapsNothingUntilItAsksSomething is the load-bearing half of ADR
// 0067. The daemon builds one of these servers per accepted session, and a
// handshake that reached for the graph would map it for every client -- including
// the ones that go on to ask nothing, which is most of them.
//
// So the whole handshake is measured: initialize, the instructions it carries and
// the tool list a client reads to decide what to call. None of it may run the
// loader; the first tool call must.
func TestASessionMapsNothingUntilItAsksSomething(t *testing.T) {
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo-a", Name: "a", Languages: "go"}},
	}, 12, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	loads := 0
	store := hotsnapshot.NewDeferredSnapshotStore(12, func() (*hotsnapshot.GraphSnapshot, error) {
		loads++
		return snapshot, nil
	})

	session := connectToServer(t, NewServerWithSnapshotStore(store))
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	// The surface has to be the working one: a deferred generation is a
	// generation, and advertising the repair instructions instead would tell the
	// agent the graph is broken.
	if len(listed.Tools) == 0 {
		t.Fatal("a deferred generation published no tool, so the handshake read as broken")
	}
	if instructions := session.InitializeResult().Instructions; strings.Contains(instructions, "index_project") &&
		!strings.Contains(instructions, "find_references") {
		t.Fatalf("the handshake carried the repair instructions for a healthy generation: %q", instructions)
	}
	if loads != 0 {
		t.Fatalf("the handshake mapped the graph %d times; a session that asks nothing must map nothing", loads)
	}

	if _, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "list_repositories", Arguments: map[string]any{},
	}); err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if loads != 1 {
		t.Fatalf("the first query mapped the graph %d times, want exactly 1", loads)
	}
}

// TestIndexingSurfaceStaysCheapToKeepResident guards the shape a configured
// client is handed: every query tool, plus the one that rebuilds the graph.
func TestIndexingSurfaceStaysCheapToKeepResident(t *testing.T) {
	session := connectToServer(t, NewServerWithSnapshotStoreAndIndexer(
		publishedStore(t), &fakeProjectIndexer{}))
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}

	mutating := 0
	resident := 0
	for _, tool := range listed.Tools {
		if tool.Description == "" {
			t.Fatalf("tool %q has no description: the description is the only routing a deferred surface carries", tool.Name)
		}
		resident += len(tool.Name)*2 + len(tool.Description)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			mutating++
		}
	}
	if mutating != 1 {
		t.Errorf("mutating tools = %d, want exactly the one that rebuilds the graph", mutating)
	}
	if resident > MaximumIndexingSurfaceBytes {
		t.Errorf("indexing surface = %d bytes over %d tools, above the %d ceiling",
			resident, len(listed.Tools), MaximumIndexingSurfaceBytes)
	}
	// The two shapes differ by one tool and nothing else, so the query ceiling
	// cannot be met by moving a description into the tool only some clients see.
	query := connectToServer(t, publishedServer(t))
	queryTools, err := query.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(listed.Tools) != len(queryTools.Tools)+1 {
		t.Errorf("indexing surface has %d tools and the query surface %d, want exactly one more",
			len(listed.Tools), len(queryTools.Tools))
	}
}

package mcp

import (
	"context"
	"encoding/json"
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
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo-a", Name: "a", Languages: "go"}},
	}, 1, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return NewServerWithSnapshotStore(hotsnapshot.NewSnapshotStore(snapshot))
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
	for _, want := range []string{"find_references", "get_source", "Where it loses"} {
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

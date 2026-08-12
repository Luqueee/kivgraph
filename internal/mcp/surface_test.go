package mcp

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// allowedTools is the entire MCP surface Ladygraph is allowed to expose, from
// PLAN.md 17.1. This list is a contract, not a snapshot of the code: adding a
// tool without adding it here is the failure this file exists to catch.
var allowedTools = []string{
	"find_cross_repo_consumers",
	"find_references",
	"find_symbol",
	"get_blast_radius",
	"get_file_outline",
	"get_symbol",
	"get_unresolved_references",
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
	names := listToolNames(t, NewServer())
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
	session := connectToServer(t, NewServer())
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

	// The output schema is the part that was cut, and the cut is what the
	// ceiling depends on: a tool that publishes a derived one again puts
	// thousands of characters back without changing the tool count.
	for _, tool := range listed.Tools {
		if tool.OutputSchema == nil {
			continue
		}
		schema, err := json.Marshal(tool.OutputSchema)
		if err != nil {
			t.Fatalf("Marshal output schema of %q: %v", tool.Name, err)
		}
		if len(schema) > 64 {
			t.Fatalf("%s publishes a %d-byte output schema; use ConciseOutputSchema", tool.Name, len(schema))
		}
	}
}

// TestServerExposesNoMutatingTool checks the forbidden list by substring, not
// by equality: a tool named "rebuild_graph" or "graph_index" would satisfy an
// equality check while breaking the same rule.
func TestServerExposesNoMutatingTool(t *testing.T) {
	for _, name := range listToolNames(t, NewServer()) {
		for _, forbidden := range forbiddenTools {
			if name == forbidden || strings.Contains(name, forbidden) {
				t.Fatalf("tool %q matches forbidden name %q", name, forbidden)
			}
		}
	}
}

func TestServerAnnotatesEveryToolReadOnly(t *testing.T) {
	session := connectToServer(t, NewServer())
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
	session := connectToServer(t, NewServer())
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

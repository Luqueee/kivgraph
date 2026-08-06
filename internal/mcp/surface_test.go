package mcp

import (
	"context"
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
	"get_symbol",
	"get_unresolved_references",
	"graph_status",
	"list_repositories",
	"trace_dependencies",
}

// forbiddenTools is PLAN.md 17.2: the surface Ladygraph must never expose,
// because it is read-only over repositories it does not own. Query execution
// and indexing control are included: both would let a client reach past the
// published snapshot.
var forbiddenTools = []string{
	"execute_cypher",
	"execute_query",
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

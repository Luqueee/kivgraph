package mcp

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// introspectionCatalog is the whole surface a configured client is handed: the
// eleven graph-query tools of allowedTools and the three indexing controls.
// It is spelled out rather than derived from allowedTools so that a tool
// added to one list and not the other fails here instead of agreeing with
// itself.
var introspectionCatalog = []string{
	"find_by_intent",
	"find_cross_repo_consumers",
	"find_references",
	"find_symbol",
	"get_blast_radius",
	"get_file_outline",
	"get_index_status",
	"get_source",
	"get_symbol",
	"graph_status",
	"index_project",
	"list_repositories",
	"start_index_project",
	"trace_dependencies",
}

// unavailableStore is a configured server that has never published: the store
// exists, holds no snapshot and has no deferred load to run. It is what
// `openConfiguredSnapshot` builds when no generation is active, so it is the
// shape every test here asks about.
func unavailableStore(t *testing.T) *hotsnapshot.SnapshotStore {
	t.Helper()
	store := hotsnapshot.NewSnapshotStore(nil)
	if store.Available() {
		t.Fatal("a store with no snapshot reports itself available")
	}
	return store
}

func introspectingServer(t *testing.T, store *hotsnapshot.SnapshotStore) *sdkmcp.Server {
	t.Helper()
	return NewServerWithMetricsAndSnapshotStoreAndIndexerOptions(
		nil, store, &fakeProjectIndexer{}, ServerOptions{ExposeUnavailableTools: true})
}

// TestColdStartStillPublishesOnlyTheRepair is ADR 0067 as it stands. The option
// added beside it must not become the default by accident, and the way that
// would happen is a zero value that stopped meaning what it means: this is the
// test that fails when it does.
func TestColdStartStillPublishesOnlyTheRepair(t *testing.T) {
	names := listToolNames(t, NewServerWithSnapshotStoreAndIndexer(unavailableStore(t), &fakeProjectIndexer{}))
	want := []string{"get_index_status", "index_project", "start_index_project"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("cold-start tools = %v, want indexing controls %v", names, want)
	}
}

// TestIntrospectionPublishesTheWholeCatalogWithoutAGeneration is the feature.
// An inspector can only score what tools/list returns, and a fail-closed
// handshake returns nothing to score.
func TestIntrospectionPublishesTheWholeCatalogWithoutAGeneration(t *testing.T) {
	names := listToolNames(t, introspectingServer(t, unavailableStore(t)))
	want := append([]string(nil), introspectionCatalog...)
	sort.Strings(want)
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("introspection tools = %v, want %v", names, want)
	}
}

// TestIntrospectionCatalogIsTheServedCatalog is what stops this from becoming a
// second, schema-only surface. The names an inspector reads with no graph have
// to be the names a client calls with one; if they can differ, what is scored
// is not what ships.
func TestIntrospectionCatalogIsTheServedCatalog(t *testing.T) {
	served := listToolNames(t, NewServerWithSnapshotStoreAndIndexer(publishedStore(t), &fakeProjectIndexer{}))
	inspected := listToolNames(t, introspectingServer(t, unavailableStore(t)))
	if !reflect.DeepEqual(served, inspected) {
		t.Fatalf("served tools = %v, introspected = %v, want the same catalogue", served, inspected)
	}
}

// TestIntrospectionServesTheSameToolDefinitions closes the loop the names leave
// open. Registries score the description and the input schema, not the name, so
// two surfaces that agree on names and disagree on schemas would score a server
// nobody runs.
func TestIntrospectionServesTheSameToolDefinitions(t *testing.T) {
	served := toolDefinitions(t, NewServerWithSnapshotStoreAndIndexer(publishedStore(t), &fakeProjectIndexer{}))
	inspected := toolDefinitions(t, introspectingServer(t, unavailableStore(t)))
	for name, want := range served {
		got, listed := inspected[name]
		if !listed {
			t.Errorf("introspection does not list %q", name)
			continue
		}
		if got != want {
			t.Errorf("%q differs between the two surfaces:\n served = %s\n listed = %s", name, want, got)
		}
	}
}

// toolDefinitions is the part of a listing a client routes on: the name, the
// description and the input schema. The annotations travel with it because a
// tool that lost its read-only hint would be scored as a mutation.
func toolDefinitions(t *testing.T, server *sdkmcp.Server) map[string]string {
	t.Helper()
	session := connectToServer(t, server)
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	definitions := make(map[string]string, len(listed.Tools))
	for _, tool := range listed.Tools {
		encoded, err := json.Marshal(struct {
			Description string                  `json:"description"`
			InputSchema any                     `json:"inputSchema"`
			Annotations *sdkmcp.ToolAnnotations `json:"annotations"`
		}{tool.Description, tool.InputSchema, tool.Annotations})
		if err != nil {
			t.Fatalf("marshal %s: %v", tool.Name, err)
		}
		definitions[tool.Name] = string(encoded)
	}
	return definitions
}

// smallestValidCall is one schema-valid request per query tool: enough of an
// argument to get past the tool's own validation and reach the graph, and no
// more. The inputs are not loosened to make this pass -- a call that stopped at
// INVALID_ARGUMENT would prove nothing about the boundary being tested.
var smallestValidCall = map[string]map[string]any{
	"graph_status":              {},
	"list_repositories":         {},
	"find_symbol":               {"name": "Thing"},
	"find_by_intent":            {"intent": "where the parser lives"},
	"get_symbol":                {"repository": "repo-a", "path": "a.go", "qualified_name": "pkg.Thing"},
	"get_source":                {"symbols": []any{map[string]any{"repository": "repo-a", "path": "a.go", "qualified_name": "pkg.Thing"}}},
	"get_file_outline":          {"repository": "repo-a", "path": "a.go"},
	"find_references":           {"name": "Thing"},
	"find_cross_repo_consumers": {"repository": "repo-a", "path": "a.go", "qualified_name": "pkg.Thing"},
	"trace_dependencies":        {"repository": "repo-a", "path": "a.go", "qualified_name": "pkg.Thing"},
	"get_blast_radius":          {"repository": "repo-a", "path": "a.go", "qualified_name": "pkg.Thing"},
}

// TestEveryIntrospectedToolRefusesWithoutAGraph is the safety half of the
// feature, and the reason the option is allowed to exist at all: a listed tool
// that cannot answer must say so in the vocabulary the surface already has.
// A panic, a nil dereference or an empty page that reads as success would each
// be worse than not listing the tool.
//
// graph_status is the one exception and it is deliberate: the tool a client
// calls to find out *why* the others refuse cannot itself refuse. It answers,
// and what it answers is that the graph is empty.
func TestEveryIntrospectedToolRefusesWithoutAGraph(t *testing.T) {
	session := connectToServer(t, introspectingServer(t, unavailableStore(t)))
	for _, name := range introspectionCatalog {
		if name == "index_project" || name == "start_index_project" || name == "get_index_status" {
			continue
		}
		arguments, known := smallestValidCall[name]
		if !known {
			t.Fatalf("%q is served but this test does not know how to call it", name)
		}
		t.Run(name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
				Name: name, Arguments: arguments,
			})
			if err != nil {
				t.Fatalf("CallTool(%q, %#v) transport error = %v", name, arguments, err)
			}
			text := contentText(t, result)
			if name == "graph_status" {
				if result.IsError {
					t.Fatalf("CallTool(%q, %#v) refused: %s", name, arguments, text)
				}
				if !strings.Contains(text, `"status":"empty"`) {
					t.Fatalf("CallTool(%q, %#v) without a generation = %s, want an empty status", name, arguments, text)
				}
				return
			}
			if !result.IsError {
				t.Fatalf("CallTool(%q, %#v) answered without a graph: %s", name, arguments, text)
			}
			if !strings.Contains(text, "INDEX_NOT_READY") {
				t.Fatalf("CallTool(%q, %#v) refused with %q, want INDEX_NOT_READY", name, arguments, text)
			}
		})
	}

	// The session survived every refusal: a process that died on one of them
	// would take the client's server with it, which is the failure this whole
	// option has to avoid.
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("the session did not survive the refusals: %v", err)
	}
}

// TestIntrospectionKeepsIndexProjectGated is the other safety property, and it
// is checked on the same server as the refusals: an option that widened the
// catalogue must not have widened what the catalogue is allowed to do.
func TestIntrospectionKeepsIndexProjectGated(t *testing.T) {
	session := connectToServer(t, introspectingServer(t, unavailableStore(t)))
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "index_project",
		Arguments: map[string]any{"name": "repo-a", "path": t.TempDir()},
	})
	if err != nil {
		t.Fatalf("index_project CallTool() transport error = %v", err)
	}
	if !result.IsError {
		t.Fatal("index_project indexed without approval")
	}
	if text := contentText(t, result); !strings.Contains(text, "PERMISSION_REQUIRED") {
		t.Fatalf("index_project unconfirmed = %q, want PERMISSION_REQUIRED", text)
	}
}

func TestIntrospectionKeepsStartIndexProjectGated(t *testing.T) {
	session := connectToServer(t, introspectingServer(t, unavailableStore(t)))
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "start_index_project",
		Arguments: map[string]any{
			"name": "repo-a", "path": t.TempDir(), "languages": []any{"go"},
		},
	})
	if err != nil {
		t.Fatalf("start_index_project CallTool() transport error = %v", err)
	}
	if !result.IsError {
		t.Fatal("start_index_project indexed without approval")
	}
	if text := contentText(t, result); !strings.Contains(text, "PERMISSION_REQUIRED") {
		t.Fatalf("start_index_project unconfirmed = %q, want PERMISSION_REQUIRED", text)
	}
}

// TestIntrospectionDoesNotClaimAGraphExists guards the one thing this option is
// not allowed to do. The instructions are the client's routing, and telling it
// the graph is healthy when nothing is published sends work to tools that
// cannot answer instead of to the command that repairs them.
func TestIntrospectionDoesNotClaimAGraphExists(t *testing.T) {
	session := connectToServer(t, introspectingServer(t, unavailableStore(t)))
	initResult := session.InitializeResult()
	if initResult == nil {
		t.Fatal("InitializeResult() = nil, want a completed handshake")
	}
	if !strings.Contains(initResult.Instructions, "kivgraph index --full") {
		t.Fatalf("instructions = %q, want the command that repairs this", initResult.Instructions)
	}
}

// TestIntrospectionMapsNothing is ADR 0067 applied to the new shape. An option
// that materialised the graph to decide what to list would put back the whole
// cost the deferred store removed -- on every session, including the ones that
// go on to ask nothing.
func TestIntrospectionMapsNothing(t *testing.T) {
	loads := 0
	store := hotsnapshot.NewDeferredSnapshotStore(12, func() (*hotsnapshot.GraphSnapshot, error) {
		loads++
		return nil, nil
	})
	session := connectToServer(t, introspectingServer(t, store))
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if loads != 0 {
		t.Fatalf("the handshake mapped the graph %d times, want none", loads)
	}
}

func contentText(t *testing.T, result *sdkmcp.CallToolResult) string {
	t.Helper()
	if result == nil {
		t.Fatal("CallTool() returned no result")
	}
	parts := make([]string, 0, len(result.Content))
	for _, block := range result.Content {
		text, ok := block.(*sdkmcp.TextContent)
		if !ok {
			t.Fatalf("content block is %T, want text", block)
		}
		parts = append(parts, text.Text)
	}
	return strings.Join(parts, "\n")
}

package agenthook

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// writeFile is the helper the endpoint cases need and production does not.
func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o600)
}

// answeringSession is a daemon that answers the two calls the gate makes.
//
// It is an in-process MCP server rather than a stub of the Graph interface,
// because the interface is not what this exercises: the wiring is the tool
// names, the arguments, the error flag and the text envelope, and a fake that
// implemented Graph would skip every one of them.
func answeringSession(t *testing.T, answers map[string]string, failing map[string]bool) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fake-daemon", Version: "1.0.0"}, nil)
	for name, body := range answers {
		text := body
		isError := failing[name]
		server.AddTool(
			&sdkmcp.Tool{Name: name, InputSchema: map[string]any{"type": "object"}},
			func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
				return &sdkmcp.CallToolResult{
					IsError: isError,
					Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}},
				}, nil
			})
	}
	serverSide, clientSide := sdkmcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverSide, nil); err != nil {
		t.Fatalf("connect server: %v", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// TestSymbolReadsAResolvedAnswerOffTheWire covers the call itself, not just the
// parser: the tool name, the text envelope and the success path.
func TestSymbolReadsAResolvedAnswerOffTheWire(t *testing.T) {
	graph := &daemonGraph{session: answeringSession(t,
		map[string]string{"find_references": referencesFixture}, nil)}
	facts, err := graph.Symbol(context.Background(), "locationLabel")
	if err != nil {
		t.Fatalf("Symbol() error = %v", err)
	}
	if facts.Declarations != 1 || facts.References != 5 || facts.Repositories != 1 {
		t.Fatalf("facts = %#v", facts)
	}
}

// TestSymbolReadsAnAmbiguityOffTheErrorFlag holds the half of the contract that
// makes one call enough: the server reports ambiguity by failing the call, and
// the gate has to read that as facts rather than as a failure.
func TestSymbolReadsAnAmbiguityOffTheErrorFlag(t *testing.T) {
	graph := &daemonGraph{session: answeringSession(t,
		map[string]string{"find_references": ambiguousFixture},
		map[string]bool{"find_references": true})}
	facts, err := graph.Symbol(context.Background(), "Load")
	if err != nil {
		t.Fatalf("an ambiguous name came back as an error: %v", err)
	}
	if facts.Declarations != 5 || len(facts.Sample) != sampleRows {
		t.Fatalf("facts = %#v", facts)
	}
}

// TestAToolThatIsNotThereIsAnError keeps a daemon serving a surface the gate
// does not recognise from reading as "the graph knows nothing", which would be
// an allow reached for the wrong reason.
func TestAToolThatIsNotThereIsAnError(t *testing.T) {
	graph := &daemonGraph{session: answeringSession(t,
		map[string]string{"something_else": "{}"}, nil)}
	if _, err := graph.Symbol(context.Background(), "Load"); err == nil {
		t.Fatal("a missing tool answered without complaining")
	}
	if _, err := graph.Intent(context.Background(), "New.*Server"); err == nil {
		t.Fatal("a missing tool answered without complaining")
	}
}

// TestIntentReadsRankedCandidates covers the other call, including the two
// spellings of a qualified name the compact view may use.
func TestIntentReadsRankedCandidates(t *testing.T) {
	const matches = `{"snapshot_id":90,"symbols":[` +
		`{"repository":"kivgraph","file_path":"internal/mcp/server.go","qualified_name":"mcp.NewServer"},` +
		`{"repository":"kena","file_path":"internal/api/server.go","qn":"api.NewServer"}]}`
	graph := &daemonGraph{session: answeringSession(t,
		map[string]string{"find_by_intent": matches}, nil)}
	facts, err := graph.Intent(context.Background(), "New.*Server")
	if err != nil {
		t.Fatalf("Intent() error = %v", err)
	}
	if facts.Declarations != 2 || facts.Repositories != 2 {
		t.Fatalf("facts = %#v", facts)
	}
	want := []string{
		"kivgraph internal/mcp/server.go mcp.NewServer",
		"kena internal/api/server.go api.NewServer",
	}
	for index, row := range want {
		if facts.Sample[index] != row {
			t.Fatalf("sample[%d] = %q, want %q", index, facts.Sample[index], row)
		}
	}
}

// TestIntentTreatsAFailedCallAsNoOpinion is the negative: find_by_intent has no
// classified failure the gate acts on, so anything it refuses leaves the gate
// with nothing rather than with an error to report.
func TestIntentTreatsAFailedCallAsNoOpinion(t *testing.T) {
	graph := &daemonGraph{session: answeringSession(t,
		map[string]string{"find_by_intent": `INDEX_NOT_READY: no graph is published yet`},
		map[string]bool{"find_by_intent": true})}
	facts, err := graph.Intent(context.Background(), "New.*Server")
	if err != nil {
		t.Fatalf("Intent() error = %v", err)
	}
	if facts.Known() {
		t.Fatalf("a refused call produced facts: %#v", facts)
	}
}

// TestAMalformedIntentAnswerIsAnError keeps a broken response from reading as
// an empty one.
func TestAMalformedIntentAnswerIsAnError(t *testing.T) {
	graph := &daemonGraph{session: answeringSession(t,
		map[string]string{"find_by_intent": "not json"}, nil)}
	if _, err := graph.Intent(context.Background(), "New.*Server"); err == nil {
		t.Fatal("a malformed answer decoded without complaining")
	}
}

// TestDialRefusesWhereNoDaemonPublished covers the path a hook takes on nearly
// every machine it runs on: there is no endpoint file, and Dial has to say so
// rather than block or wait.
func TestDialRefusesWhereNoDaemonPublished(t *testing.T) {
	graph, closer, err := Dial(context.Background(), t.TempDir())
	if err == nil {
		t.Fatal("Dial() succeeded with no endpoint published")
	}
	if graph != nil {
		t.Fatalf("a failed Dial returned a graph: %#v", graph)
	}
	if closer != nil {
		t.Fatal("a failed Dial returned something to close")
	}
}

// TestDialRefusesAnEndpointItCannotUse keeps a leftover or truncated endpoint
// file from being dialled.
func TestDialRefusesAnEndpointItCannotUse(t *testing.T) {
	for name, body := range map[string]string{
		"not json":     "{",
		"no url":       `{"token":"abc"}`,
		"no token":     `{"url":"http://127.0.0.1:7788/mcp"}`,
		"empty object": `{}`,
		"wrong type":   `[]`,
	} {
		t.Run(name, func(t *testing.T) {
			directory := t.TempDir()
			if err := writeFile(filepath.Join(directory, "daemon.json"), body); err != nil {
				t.Fatal(err)
			}
			if _, _, err := Dial(context.Background(), directory); err == nil {
				t.Fatalf("Dial() accepted %s", name)
			} else if errors.Is(err, context.Canceled) {
				t.Fatalf("unexpected error kind: %v", err)
			}
		})
	}
}

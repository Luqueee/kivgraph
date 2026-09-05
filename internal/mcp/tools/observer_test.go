package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestSummarizeLogQueryLeavesOpaqueAndSensitiveArgumentsOut keeps the durable
// operator log useful without turning it into a copy of the MCP request.
func TestSummarizeLogQueryLeavesOpaqueAndSensitiveArgumentsOut(t *testing.T) {
	got := summarizeLogQuery(findByIntentToolName, []byte(`{
		"intent":"HTTP endpoints", "cursor":"opaque-cursor", "token":"secret"
	}`))
	if got != `intent="HTTP endpoints"` {
		t.Fatalf("summarizeLogQuery() = %q, want only the user-facing query", got)
	}
}

func TestSummarizeLogQueryDoesNotCopySourceStableKeys(t *testing.T) {
	got := summarizeLogQuery(getSourceToolName, []byte(`{
		"symbols":[{"stable_key":"opaque-key"},{"qualified_name":"mcp.NewServer"}]
	}`))
	want := `symbols=["[stable key]","mcp.NewServer"]`
	if got != want || strings.Contains(got, "opaque-key") {
		t.Fatalf("summarizeLogQuery() = %q, want %q without the stable key", got, want)
	}
}

func TestSummarizeLogQueryNamesTheIntentAndKeywords(t *testing.T) {
	got := summarizeLogQuery(findByIntentToolName, []byte(`{
		"repo":"kivgraph", "keywords":["router","handler"],
		"intent":"HTTP endpoints and routes"
	}`))
	want := `intent="HTTP endpoints and routes" keywords=["router","handler"] repo="kivgraph"`
	if got != want {
		t.Fatalf("summarizeLogQuery() = %q, want %q", got, want)
	}
}

func TestSummarizeLogQueryRedactsAbsolutePaths(t *testing.T) {
	for _, test := range []struct {
		name string
		path string
	}{
		{"unix", "/private/worktree/internal/mcp/server.go"},
		{"windows drive", `C:\private\worktree\internal\mcp\server.go`},
		{"windows UNC", `\\server\private\worktree\internal\mcp\server.go`},
	} {
		t.Run(test.name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]string{
				"repository": "kivgraph",
				"path":       test.path,
			})
			if err != nil {
				t.Fatal(err)
			}
			got := summarizeLogQuery(fileOutlineToolName, raw)
			want := `repository="kivgraph" path="[absolute path]"`
			if got != want || strings.Contains(got, "private") {
				t.Fatalf("summarizeLogQuery(file outline %s path) = %q, want %q without the absolute path", test.name, got, want)
			}
		})
	}

	got := summarizeLogQuery(getSourceToolName, []byte(`{
		"symbols":[{"repository":"kivgraph", "path":"/private/worktree/internal/mcp/server.go"}]
	}`))
	want := `symbols=["kivgraph:[absolute path]"]`
	if got != want || strings.Contains(got, "/private/worktree") {
		t.Fatalf("summarizeLogQuery(source selector) = %q, want %q without the absolute path", got, want)
	}

	got = summarizeLogQuery(getSourceToolName, []byte(`{
		"symbols":[{"repository":"kivgraph", "path":"internal/mcp/server.go"}]
	}`))
	want = `symbols=["kivgraph:internal/mcp/server.go"]`
	if got != want {
		t.Fatalf("summarizeLogQuery(relative source selector) = %q, want %q", got, want)
	}
}

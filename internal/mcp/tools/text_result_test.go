package tools

import (
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// decodeResponse reads a tool result through the only channel a query tool
// fills, and fails when a second one appears. Asserting the absence is the
// point: the duplicate structured channel was invisible in every test that
// simply read whichever copy it preferred.
func decodeResponse[T any](t *testing.T, result *sdkmcp.CallToolResult) Response[T] {
	t.Helper()
	if result.StructuredContent != nil {
		t.Fatalf("tool result carries structuredContent as well as text: %#v", result.StructuredContent)
	}
	if len(result.Content) != 1 {
		t.Fatalf("tool result has %d content blocks, want exactly one", len(result.Content))
	}
	text, ok := result.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("tool result content block is %T, want text", result.Content[0])
	}
	var response Response[T]
	if err := json.Unmarshal([]byte(text.Text), &response); err != nil {
		t.Fatalf("Unmarshal tool text: %v", err)
	}
	return response
}

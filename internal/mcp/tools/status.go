package tools

import (
	"context"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// GraphStatus is the status payload carried by the standard response
// envelope.
type GraphStatus struct {
	Status       string `json:"status"`
	Repositories int    `json:"repositories"`
	Symbols      int    `json:"symbols"`
	Edges        int    `json:"edges"`
}

// RegisterGraphStatus adds the read-only graph status tool to a server.
func RegisterGraphStatus(server *sdkmcp.Server) {
	RegisterGraphStatusWithObserver(server, nil)
}

// RegisterGraphStatusWithObserver adds graph_status and optionally observes handler latency.
func RegisterGraphStatusWithObserver(server *sdkmcp.Server, observer Observer) {
	handler := graphStatus
	if observer != nil {
		handler = func(ctx context.Context, request *sdkmcp.CallToolRequest, arguments struct{}) (*sdkmcp.CallToolResult, Response[GraphStatus], error) {
			start := time.Now()
			result, status, err := graphStatus(ctx, request, arguments)
			observer("graph_status", time.Since(start))
			return result, status, err
		}
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "graph_status",
		Description: "Returns the current status of the indexed graph.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, handler)
}

func graphStatus(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	_ struct{},
) (*sdkmcp.CallToolResult, Response[GraphStatus], error) {
	return nil, Response[GraphStatus]{
		Total:    1,
		Returned: 1,
		Results: GraphStatus{
			Status:       "empty",
			Repositories: 0,
			Symbols:      0,
			Edges:        0,
		},
	}, nil
}

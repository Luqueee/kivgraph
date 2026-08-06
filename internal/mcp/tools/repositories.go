package tools

import (
	"context"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RepositorySummary is the stable public shape for a registered repository.
type RepositorySummary struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	Languages []string `json:"languages"`
}

// RepositoryList is the paginated repository response.
type RepositoryList struct {
	Repositories []RepositorySummary `json:"repositories"`
	Total        int                 `json:"total"`
	Returned     int                 `json:"returned"`
	Truncated    bool                `json:"truncated"`
}

// RegisterListRepositories adds the read-only repository listing tool.
func RegisterListRepositories(server *sdkmcp.Server) {
	RegisterListRepositoriesWithObserver(server, nil)
}

// RegisterListRepositoriesWithObserver adds list_repositories and optionally observes handler latency.
func RegisterListRepositoriesWithObserver(server *sdkmcp.Server, observer Observer) {
	handler := listRepositories
	if observer != nil {
		handler = func(ctx context.Context, request *sdkmcp.CallToolRequest, arguments struct{}) (*sdkmcp.CallToolResult, RepositoryList, error) {
			start := time.Now()
			result, repositories, err := listRepositories(ctx, request, arguments)
			observer("list_repositories", time.Since(start))
			return result, repositories, err
		}
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        "list_repositories",
		Description: "Lists repositories registered with Ladygraph.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, handler)
}

func listRepositories(
	_ context.Context,
	_ *sdkmcp.CallToolRequest,
	_ struct{},
) (*sdkmcp.CallToolResult, RepositoryList, error) {
	return nil, RepositoryList{
		Repositories: []RepositorySummary{},
		Total:        0,
		Returned:     0,
		Truncated:    false,
	}, nil
}

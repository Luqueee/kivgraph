package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/mcp/tools"
	"github.com/Luqueee/ladygraph/internal/version"
)

const serverName = "ladygraph"

// NewServer creates the empty Ladygraph MCP server with its advertised identity.
func NewServer() *sdkmcp.Server {
	return newServer(nil)
}

// NewServerWithObserver creates the empty server and observes tool-handler latency.
func NewServerWithObserver(observer tools.Observer) *sdkmcp.Server {
	return newServer(observer)
}

func newServer(observer tools.Observer) *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    serverName,
		Version: version.Value,
	}, nil)
	tools.RegisterGraphStatusWithObserver(server, observer)
	tools.RegisterListRepositoriesWithObserver(server, observer)
	return server
}

// Run serves one MCP session over the process stdin/stdout transport.
func Run(ctx context.Context) error {
	return NewServer().Run(ctx, &sdkmcp.StdioTransport{})
}

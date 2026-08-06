package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/mcp/tools"
	"github.com/Luqueee/ladygraph/internal/version"
)

const serverName = "ladygraph"

// NewServer creates the Ladygraph MCP server with no graph source.
func NewServer() *sdkmcp.Server {
	return newServer(nil, nil)
}

// NewServerWithObserver creates the server without a graph source and observes
// tool-handler latency.
func NewServerWithObserver(observer tools.Observer) *sdkmcp.Server {
	return newServer(observer, nil)
}

// NewServerWithSnapshotStore creates the server backed by the published
// immutable HotSnapshot.
func NewServerWithSnapshotStore(snapshotStore *hotsnapshot.SnapshotStore) *sdkmcp.Server {
	return newServer(nil, snapshotStore)
}

// NewServerWithObserverAndSnapshotStore creates the snapshot-backed server and
// observes tool-handler latency.
func NewServerWithObserverAndSnapshotStore(observer tools.Observer, snapshotStore *hotsnapshot.SnapshotStore) *sdkmcp.Server {
	return newServer(observer, snapshotStore)
}

func newServer(observer tools.Observer, snapshotStore *hotsnapshot.SnapshotStore) *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    serverName,
		Version: version.Value,
	}, nil)
	tools.RegisterGraphStatusWithObserver(server, observer)
	tools.RegisterListRepositoriesWithObserverAndSnapshotStore(server, observer, snapshotStore)
	tools.RegisterFindSymbolWithObserverAndSnapshotStore(server, observer, snapshotStore)
	tools.RegisterGetSymbolWithObserverAndSnapshotStore(server, observer, snapshotStore)
	tools.RegisterFindReferencesWithObserverAndSnapshotStore(server, observer, snapshotStore)
	tools.RegisterFindCrossRepoConsumersWithObserverAndSnapshotStore(server, observer, snapshotStore)
	tools.RegisterTraceDependenciesWithObserverAndSnapshotStore(server, observer, snapshotStore)
	tools.RegisterGetBlastRadiusWithObserverAndSnapshotStore(server, observer, snapshotStore)
	return server

}

// Run serves one MCP session over the process stdin/stdout transport.
func Run(ctx context.Context) error {
	return RunWithSnapshotStore(ctx, nil)
}

// RunWithSnapshotStore serves one MCP session using snapshotStore as the
// immutable graph source for query tools.
func RunWithSnapshotStore(ctx context.Context, snapshotStore *hotsnapshot.SnapshotStore) error {
	return NewServerWithSnapshotStore(snapshotStore).Run(ctx, &sdkmcp.StdioTransport{})
}

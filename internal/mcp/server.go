package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/mcp/tools"
	"github.com/Luqueee/ladygraph/internal/metrics"
	"github.com/Luqueee/ladygraph/internal/version"
)

const serverName = "ladygraph"

// NewServer creates the Ladygraph MCP server with no graph source.
func NewServer() *sdkmcp.Server {
	return newServer(nil, nil, nil)
}

// NewServerWithObserver creates the server without a graph source and observes
// tool-handler latency.
func NewServerWithObserver(observer tools.Observer) *sdkmcp.Server {
	return newServer(observer, nil, nil)
}

// NewServerWithMetrics creates the server without a graph source and records
// query metrics in registry.
func NewServerWithMetrics(registry *metrics.Registry) *sdkmcp.Server {
	return newServer(nil, nil, registry)
}

// NewServerWithObserverAndMetrics creates the server without a graph source,
// observing latency and recording query metrics in registry.
func NewServerWithObserverAndMetrics(observer tools.Observer, registry *metrics.Registry) *sdkmcp.Server {
	return newServer(observer, nil, registry)
}

// NewServerWithSnapshotStore creates the server backed by the published
// immutable HotSnapshot.
func NewServerWithSnapshotStore(snapshotStore *hotsnapshot.SnapshotStore) *sdkmcp.Server {
	return newServer(nil, snapshotStore, nil)
}

// NewServerWithObserverAndSnapshotStore creates the snapshot-backed server and
// observes tool-handler latency.
func NewServerWithObserverAndSnapshotStore(observer tools.Observer, snapshotStore *hotsnapshot.SnapshotStore) *sdkmcp.Server {
	return newServer(observer, snapshotStore, nil)
}

// NewServerWithMetricsAndSnapshotStore creates the snapshot-backed server and
// records query metrics in registry.
func NewServerWithMetricsAndSnapshotStore(registry *metrics.Registry, snapshotStore *hotsnapshot.SnapshotStore) *sdkmcp.Server {
	return newServer(nil, snapshotStore, registry)
}

// NewServerWithObserverAndMetricsAndSnapshotStore combines the legacy latency
// observer with the process-local metrics registry.
func NewServerWithObserverAndMetricsAndSnapshotStore(
	observer tools.Observer,
	registry *metrics.Registry,
	snapshotStore *hotsnapshot.SnapshotStore,
) *sdkmcp.Server {
	return newServer(observer, snapshotStore, registry)
}

func newServer(observer tools.Observer, snapshotStore *hotsnapshot.SnapshotStore, registry *metrics.Registry) *sdkmcp.Server {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    serverName,
		Version: version.Value,
	}, nil)
	var callObserver tools.CallObserver
	if registry != nil {
		callObserver = func(observation tools.CallObservation) {
			registry.ObserveQuery(metrics.QueryObservation{
				ToolName:          observation.ToolName,
				Elapsed:           observation.Elapsed,
				Returned:          observation.Returned,
				Truncated:         observation.Truncated,
				UnresolvedRelated: observation.UnresolvedRelated,
				SnapshotID:        observation.SnapshotID,
				SnapshotAgeMS:     observation.SnapshotAgeMS,
				Err:               observation.Err,
			})
		}
	}
	if registry != nil && snapshotStore != nil {
		if snapshot := snapshotStore.Load(); snapshot != nil {
			metadata := snapshot.Metadata()
			registry.ObserveSnapshot(metrics.SnapshotObservation{
				ID:        metadata.ID,
				CreatedAt: metadata.CreatedAt,
			})
		}
	}
	tools.RegisterGraphStatusWithObserverAndSnapshotStoreAndMetrics(server, observer, snapshotStore, nil, registry, callObserver)
	tools.RegisterListRepositoriesWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterFindSymbolWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterGetSymbolWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterFindReferencesWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterFindCrossRepoConsumersWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterTraceDependenciesWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterGetBlastRadiusWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterGetUnresolvedReferencesWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	return server
}

// Run serves one MCP session over the process stdin/stdout transport with a
// process-local metrics registry available to graph_status.
func Run(ctx context.Context) error {
	return RunWithMetricsAndSnapshotStore(ctx, metrics.NewRegistry(), nil)
}

// RunWithSnapshotStore serves one MCP session using snapshotStore as the
// immutable graph source for query tools and a fresh metrics registry.
func RunWithSnapshotStore(ctx context.Context, snapshotStore *hotsnapshot.SnapshotStore) error {
	return RunWithMetricsAndSnapshotStore(ctx, metrics.NewRegistry(), snapshotStore)
}

// RunWithMetrics serves one MCP session with the caller-owned metrics registry.
func RunWithMetrics(ctx context.Context, registry *metrics.Registry) error {
	return RunWithMetricsAndSnapshotStore(ctx, registry, nil)
}

// RunWithMetricsAndSnapshotStore serves one MCP session with caller-owned
// metrics and the immutable graph source.
func RunWithMetricsAndSnapshotStore(
	ctx context.Context,
	registry *metrics.Registry,
	snapshotStore *hotsnapshot.SnapshotStore,
) error {
	if registry == nil {
		registry = metrics.NewRegistry()
	}
	return NewServerWithMetricsAndSnapshotStore(registry, snapshotStore).Run(ctx, &sdkmcp.StdioTransport{})
}

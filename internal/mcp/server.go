// Package mcp assembles the Kivgraph MCP server: the tool surface, the
// instructions returned at handshake, and the transports it is served over.
package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/freshness"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/indexing"
	"github.com/Luqueee/kivgraph/internal/mcp/tools"
	"github.com/Luqueee/kivgraph/internal/metrics"
	"github.com/Luqueee/kivgraph/internal/version"
)

const serverName = "kivgraph"

// ServerOptions carries the choices a caller makes about the *shape* of the
// surface, as opposed to what answers from it. The zero value is the surface
// every existing constructor builds, so a caller that has nothing to say says
// nothing.
type ServerOptions struct {
	// ExposeUnavailableTools registers the query tools even when no
	// generation is published.
	//
	// It exists for the inspectors and registries that read a tool catalogue
	// before anybody has indexed anything: they can only score what
	// tools/list returns, and the fail-closed handshake of ADR 0067 returns
	// nothing to score. It changes what is *listed* and nothing else. There
	// is still no graph, so graph-dependent query tools return INDEX_NOT_READY
	// until one is published while graph_status reports an empty graph status.
	// index_project stays behind the same consent gate, and the handshake still
	// carries the repair instructions -- telling a client a graph exists when
	// none does would be the one lie this option must not tell.
	ExposeUnavailableTools bool
}

// NewServer creates the Kivgraph MCP server with no graph source.
func NewServer() *sdkmcp.Server {
	return newServer(nil, nil, nil)
}

// NewServerWithIndexer creates a server that also exposes the explicit
// permission-gated index_project mutation.
func NewServerWithIndexer(indexer indexing.ProjectIndexer) *sdkmcp.Server {
	return newServerWithIndexer(nil, nil, nil, indexer, ServerOptions{})
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

// NewServerWithSnapshotStoreAndIndexer creates the configured server backed by
// the published snapshot and an authorized project indexer.
func NewServerWithSnapshotStoreAndIndexer(
	snapshotStore *hotsnapshot.SnapshotStore,
	indexer indexing.ProjectIndexer,
) *sdkmcp.Server {
	return newServerWithIndexer(nil, snapshotStore, nil, indexer, ServerOptions{})
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

// NewServerWithMetricsAndSnapshotStoreAndIndexer combines query metrics with
// the configured project indexer.
func NewServerWithMetricsAndSnapshotStoreAndIndexer(
	registry *metrics.Registry,
	snapshotStore *hotsnapshot.SnapshotStore,
	indexer indexing.ProjectIndexer,
) *sdkmcp.Server {
	return newServerWithIndexer(nil, snapshotStore, registry, indexer, ServerOptions{})
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

// NewServerWithMetricsAndSnapshotStoreAndIndexerOptions is the configured
// server with the surface options applied. ServerOptions{} builds exactly what
// NewServerWithMetricsAndSnapshotStoreAndIndexer builds.
func NewServerWithMetricsAndSnapshotStoreAndIndexerOptions(
	registry *metrics.Registry,
	snapshotStore *hotsnapshot.SnapshotStore,
	indexer indexing.ProjectIndexer,
	options ServerOptions,
) *sdkmcp.Server {
	return newServerWithIndexer(nil, snapshotStore, registry, indexer, options)
}

func newServer(observer tools.Observer, snapshotStore *hotsnapshot.SnapshotStore, registry *metrics.Registry) *sdkmcp.Server {
	return newServerWithIndexer(observer, snapshotStore, registry, nil, ServerOptions{})
}

func newServerWithIndexer(
	observer tools.Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	registry *metrics.Registry,
	indexer indexing.ProjectIndexer,
	options ServerOptions,
) *sdkmcp.Server {
	// A server with no published generation completes the handshake, publishes no
	// query tool and says how to repair itself. It is the one shape a client can
	// act on: it spawns this process, so exiting reads as a crash, and answering
	// with tools that cannot answer teaches the agent that the tools do not work.
	//
	// The question is availability, not the graph. Asking for the graph here
	// would map it once per accepted session -- and this runs for every session,
	// including the ones that go on to ask nothing at all, which is most of
	// them. See ADR 0067.
	published := snapshotStore.Available()
	// What is listed and what is true are two different questions, and only
	// the first one this option answers. A client told the graph is healthy
	// when nothing is published would route its work to tools that cannot
	// answer and read the refusals as broken tools; the repair instructions
	// are what let it act instead.
	exposeQueries := published || options.ExposeUnavailableTools
	instructions := serverInstructions
	if !published {
		instructions = staleServerInstructions
	}
	server := sdkmcp.NewServer(&sdkmcp.Implementation{
		Name:    serverName,
		Version: version.Value,
	}, &sdkmcp.ServerOptions{Instructions: instructions})
	var callObserver tools.CallObserver
	if registry != nil {
		callObserver = func(observation tools.CallObservation) {
			registry.ObserveQuery(metrics.QueryObservation{
				ToolName:          observation.ToolName,
				Query:             observation.Query,
				Elapsed:           observation.Elapsed,
				Returned:          observation.Returned,
				Truncated:         observation.Truncated,
				UnresolvedRelated: observation.UnresolvedRelated,
				SnapshotID:        observation.SnapshotID,
				SnapshotAgeMS:     observation.SnapshotAgeMS,
				Err:               observation.Err,
				// Classified here because this is the seam where the tool
				// vocabulary is still in scope: `internal/metrics` cannot
				// import `tools`, since `tools` imports it.
				Refused: tools.IsRefusal(observation.Err),
			})
		}
	}
	// The snapshot's own metadata is not observed here for the same reason: it
	// would require the graph. The loader records it when it runs, which is also
	// the moment the numbers become true of this process.
	if !exposeQueries {
		// index_project is the exception: it is how a client without a graph
		// builds one, and it needs no graph to run.
		tools.RegisterIndexProject(server, indexer, callObserver)
		return server
	}
	var statusProbe tools.HostStatusProbe
	if verifier, ok := indexer.(interface {
		ContentFreshness(context.Context) freshness.Status
	}); ok {
		statusProbe = func(ctx context.Context) (tools.HostStatus, error) {
			status := verifier.ContentFreshness(ctx)
			return tools.HostStatus{ContentFreshness: &status}, nil
		}
	}
	registerQueryTools(server, observer, snapshotStore, registry, callObserver, statusProbe)
	tools.RegisterIndexProject(server, indexer, callObserver)
	return server
}

// registerQueryTools adds the read-only surface. It is one function so that the
// catalogue an inspector reads without a graph is the same catalogue a client
// gets with one: two lists would drift, and a schema copied for inspection
// would describe a tool nobody serves.
func registerQueryTools(
	server *sdkmcp.Server,
	observer tools.Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	registry *metrics.Registry,
	callObserver tools.CallObserver,
	probes ...tools.HostStatusProbe,
) {
	var probe tools.HostStatusProbe
	if len(probes) > 0 {
		probe = probes[0]
	}
	tools.RegisterGraphStatusWithObserverAndSnapshotStoreAndMetrics(server, observer, snapshotStore, probe, registry, callObserver)
	tools.RegisterListRepositoriesWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterFindSymbolWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterFindByIntentWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterGetSymbolWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterGetSourceWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterGetFileOutlineWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterFindReferencesWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterFindCrossRepoConsumersWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterTraceDependenciesWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
	tools.RegisterGetBlastRadiusWithObserverAndSnapshotStore(server, observer, snapshotStore, callObserver)
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

// RunWithSnapshotStoreAndIndexer serves the configured MCP surface, including
// the permission-gated index_project tool.
func RunWithSnapshotStoreAndIndexer(
	ctx context.Context,
	snapshotStore *hotsnapshot.SnapshotStore,
	indexer indexing.ProjectIndexer,
) error {
	return RunWithMetricsAndSnapshotStoreAndIndexer(ctx, metrics.NewRegistry(), snapshotStore, indexer)
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
	return RunWithMetricsAndSnapshotStoreAndIndexer(ctx, registry, snapshotStore, nil)
}

// RunWithMetricsAndSnapshotStoreAndIndexer serves one MCP session with
// caller-owned metrics, the immutable graph source, and an authorized project
// indexer.
func RunWithMetricsAndSnapshotStoreAndIndexer(
	ctx context.Context,
	registry *metrics.Registry,
	snapshotStore *hotsnapshot.SnapshotStore,
	indexer indexing.ProjectIndexer,
) error {
	return RunWithMetricsAndSnapshotStoreAndIndexerOptions(ctx, registry, snapshotStore, indexer, ServerOptions{})
}

// RunWithMetricsAndSnapshotStoreAndIndexerOptions is the same session with the
// surface options applied. ServerOptions{} serves exactly what
// RunWithMetricsAndSnapshotStoreAndIndexer serves.
func RunWithMetricsAndSnapshotStoreAndIndexerOptions(
	ctx context.Context,
	registry *metrics.Registry,
	snapshotStore *hotsnapshot.SnapshotStore,
	indexer indexing.ProjectIndexer,
	options ServerOptions,
) error {
	if registry == nil {
		registry = metrics.NewRegistry()
	}
	server := NewServerWithMetricsAndSnapshotStoreAndIndexerOptions(registry, snapshotStore, indexer, options)
	return server.Run(ctx, &sdkmcp.StdioTransport{})
}

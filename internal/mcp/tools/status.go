package tools

import (
	"context"
	"fmt"
	"sort"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/metrics"
)

const (
	// GraphStatusEmpty means no snapshot is published: every query tool
	// answers INDEX_NOT_READY until one is.
	GraphStatusEmpty = "empty"
	// GraphStatusReady means a snapshot is published and queryable.
	GraphStatusReady = "ready"

	// HealthNotConfigured is reported for a component this deployment
	// wired a probe for that answered nothing. It is a statement about the
	// server, not about the component: an unprobed worker is not the same
	// as a healthy one.
	HealthNotConfigured = "not_configured"
	// HealthNotApplicable is reported for a component this process does
	// not use. `serve` answers queries from the published HotSnapshot: it
	// never opens the database and never runs the TypeScript worker, so
	// reporting those two as unconfigured suggested a misconfiguration
	// where there is none. What is or is not being served is `status`,
	// `snapshot_id` and `snapshot_age_ms`.
	HealthNotApplicable = "not_applicable"

	graphStatusToolName = "graph_status"
)

// GraphStatus reports what the server is currently serving from. Snapshot and
// host fields are never inferred; metrics are included only when the process
// supplies the optional registry.
type GraphStatus struct {
	Status string `json:"status"`

	SnapshotID        *uint64 `json:"snapshot_id"`
	SnapshotBuiltAt   string  `json:"snapshot_built_at,omitempty"`
	SnapshotAgeMS     *int64  `json:"snapshot_age_ms"`
	SnapshotRowFormat uint32  `json:"snapshot_row_format_version,omitempty"`
	SchemaVersion     int     `json:"schema_version,omitempty"`
	ResolverVersion   string  `json:"resolver_version,omitempty"`

	Repositories int `json:"repositories"`
	Packages     int `json:"packages"`
	Files        int `json:"files"`
	Symbols      int `json:"symbols"`
	Evidence     int `json:"evidence"`
	Edges        int `json:"edges"`
	PackageEdges int `json:"package_edges"`
	Unresolved   int `json:"unresolved"`

	EdgesByKind        []GraphStatusCount `json:"edges_by_kind"`
	UnresolvedByReason []GraphStatusCount `json:"unresolved_by_reason"`

	LastRebuildAt string          `json:"last_rebuild_at,omitempty"`
	LastUpdateAt  string          `json:"last_update_at,omitempty"`
	Worker        ComponentHealth `json:"worker"`
	Storage       ComponentHealth `json:"storage"`

	// Metrics is present only when the hosting process wires a metrics
	// registry; an absent registry must not be reported as healthy or empty
	// metrics.
	Metrics *metrics.Report `json:"metrics,omitempty"`
}

type GraphStatusCount struct {
	Key   string `json:"key"`
	Count int    `json:"count"`
}

// ComponentHealth is one probed dependency. State is HealthNotConfigured when
// the host wired no probe for it.
type ComponentHealth struct {
	State  string `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// HostStatus is what only the process hosting the server can know: when the
// graph was last rebuilt or updated, and whether its dependencies answer.
type HostStatus struct {
	LastRebuildAt time.Time
	LastUpdateAt  time.Time
	Worker        ComponentHealth
	Storage       ComponentHealth
}

// HostStatusProbe supplies HostStatus. It runs on the graph_status fast path,
// so an implementation must answer from cached state, never by rebuilding or
// by opening a database.
type HostStatusProbe func(context.Context) (HostStatus, error)

// RegisterGraphStatus adds the read-only graph status tool with no snapshot
// and no host probe.
func RegisterGraphStatus(server *sdkmcp.Server) {
	RegisterGraphStatusWithObserverAndSnapshotStore(server, nil, nil, nil)
}

// RegisterGraphStatusWithObserver adds graph_status and optionally observes
// handler latency.
func RegisterGraphStatusWithObserver(server *sdkmcp.Server, observer Observer) {
	RegisterGraphStatusWithObserverAndSnapshotStore(server, observer, nil, nil)
}

// RegisterGraphStatusWithSnapshotStore registers graph_status over the
// immutable snapshot currently published by snapshotStore.
func RegisterGraphStatusWithSnapshotStore(server *sdkmcp.Server, snapshotStore *hotsnapshot.SnapshotStore) {
	RegisterGraphStatusWithObserverAndSnapshotStore(server, nil, snapshotStore, nil)
}

// RegisterGraphStatusWithObserverAndSnapshotStore registers graph_status over
// a snapshot store, optionally observing latency and probing host state.
func RegisterGraphStatusWithObserverAndSnapshotStore(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	probe HostStatusProbe,
	callObservers ...CallObserver,
) {
	RegisterGraphStatusWithObserverAndSnapshotStoreAndMetrics(
		server,
		observer,
		snapshotStore,
		probe,
		nil,
		callObservers...,
	)
}

// RegisterGraphStatusWithObserverAndSnapshotStoreAndMetrics registers
// graph_status with the process-local metrics report when registry is set.
func RegisterGraphStatusWithObserverAndSnapshotStoreAndMetrics(
	server *sdkmcp.Server,
	observer Observer,
	snapshotStore *hotsnapshot.SnapshotStore,
	probe HostStatusProbe,
	registry *metrics.Registry,
	callObservers ...CallObserver,
) {
	callObserver := firstCallObserver(callObservers)
	handler := func(
		ctx context.Context,
		request *sdkmcp.CallToolRequest,
		arguments struct{},
	) (*sdkmcp.CallToolResult, Response[GraphStatus], error) {
		return graphStatus(ctx, request, arguments, snapshotStore, probe, registry)
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments struct{},
		) (*sdkmcp.CallToolResult, Response[GraphStatus], error) {
			start := time.Now()
			result, status, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, graphStatusToolName, start, status, err)
			return result, status, err
		}
	}
	sdkmcp.AddTool(server, &sdkmcp.Tool{
		Name:        graphStatusToolName,
		Description: "Returns the published snapshot, its provenance, its counts, dependency health, and internal metrics.",
		Annotations: &sdkmcp.ToolAnnotations{ReadOnlyHint: true},
	}, handler)
}

// graphStatus never fails on a missing snapshot: reporting that the index is
// empty is precisely this tool's job, and the tool a client calls to find out
// why the others answer INDEX_NOT_READY cannot itself refuse to answer.
func graphStatus(
	ctx context.Context,
	_ *sdkmcp.CallToolRequest,
	_ struct{},
	snapshotStore *hotsnapshot.SnapshotStore,
	probe HostStatusProbe,
	metricsRegistries ...*metrics.Registry,
) (*sdkmcp.CallToolResult, Response[GraphStatus], error) {
	var registry *metrics.Registry
	if len(metricsRegistries) > 0 {
		registry = metricsRegistries[0]
	}
	status := GraphStatus{
		Status:             GraphStatusEmpty,
		EdgesByKind:        []GraphStatusCount{},
		UnresolvedByReason: []GraphStatusCount{},
		Worker: ComponentHealth{
			State:  HealthNotApplicable,
			Detail: "the TypeScript worker runs during indexing, not in this server",
		},
		Storage: ComponentHealth{
			State:  HealthNotApplicable,
			Detail: "this server answers from the published snapshot and never opens the database",
		},
	}
	if err := applyHostStatus(ctx, &status, probe); err != nil {
		return nil, Response[GraphStatus]{}, err
	}

	var snapshot *hotsnapshot.GraphSnapshot
	if snapshotStore != nil {
		snapshot = snapshotStore.Load()
	}
	if snapshot == nil {
		applyMetricsStatus(&status, registry)
		return nil, Response[GraphStatus]{Total: 1, Returned: 1, Results: status}, nil
	}
	if err := applySnapshotStatus(&status, snapshot); err != nil {
		return nil, Response[GraphStatus]{}, WrapToolError(
			CodeSnapshotUnavailable,
			"active snapshot contains invalid status metadata",
			err,
		)
	}

	metadata := snapshot.Metadata()
	snapshotID := metadata.ID
	snapshotAgeMS := snapshotAgeMilliseconds(metadata.CreatedAt)
	applyMetricsStatus(&status, registry)
	return nil, Response[GraphStatus]{
		SnapshotID: &snapshotID, SnapshotAgeMS: &snapshotAgeMS,
		Total: 1, Returned: 1, Results: status,
	}, nil
}

func applyMetricsStatus(status *GraphStatus, registry *metrics.Registry) {
	if registry == nil {
		return
	}
	report := registry.Report()
	status.Metrics = &report
}

func applyHostStatus(ctx context.Context, status *GraphStatus, probe HostStatusProbe) error {
	if probe == nil {
		return nil
	}
	host, err := probe(ctx)
	if err != nil {
		return WrapToolError(CodeSnapshotUnavailable, "host status is unavailable", err)
	}
	if !host.LastRebuildAt.IsZero() {
		status.LastRebuildAt = host.LastRebuildAt.UTC().Format(time.RFC3339)
	}
	if !host.LastUpdateAt.IsZero() {
		status.LastUpdateAt = host.LastUpdateAt.UTC().Format(time.RFC3339)
	}
	if host.Worker.State != "" {
		status.Worker = host.Worker
	}
	if host.Storage.State != "" {
		status.Storage = host.Storage
	}
	return nil
}

func applySnapshotStatus(status *GraphStatus, snapshot *hotsnapshot.GraphSnapshot) error {
	metadata := snapshot.Metadata()
	counts := metadata.Counts
	snapshotID := metadata.ID
	status.Status = GraphStatusReady
	status.SnapshotID = &snapshotID
	status.SnapshotBuiltAt = metadata.CreatedAt.UTC().Format(time.RFC3339)
	age := snapshotAgeMilliseconds(metadata.CreatedAt)
	status.SnapshotAgeMS = &age
	status.SnapshotRowFormat = metadata.Version
	status.SchemaVersion = metadata.SchemaVersion
	status.ResolverVersion = metadata.ResolverVersion
	status.Repositories = int(counts.Repositories)
	status.Packages = int(counts.Packages)
	status.Files = int(counts.Files)
	status.Symbols = int(counts.Symbols)
	status.Evidence = int(counts.Evidence)
	status.Edges = int(counts.Edges)
	status.PackageEdges = int(counts.PackageEdges)
	status.Unresolved = int(counts.Unresolved)

	edges, err := snapshotEdgeKindCounts(snapshot, int(counts.Symbols))
	if err != nil {
		return err
	}
	status.EdgesByKind = edges
	reasons, err := snapshotUnresolvedReasonCounts(snapshot)
	if err != nil {
		return err
	}
	status.UnresolvedByReason = reasons
	return nil
}

// snapshotEdgeKindCounts walks the forward CSR once. Every symbol edge is
// counted exactly once because the forward adjacency holds each edge under its
// source; the reverse CSR is the same multiset seen from the other end.
func snapshotEdgeKindCounts(snapshot *hotsnapshot.GraphSnapshot, symbols int) ([]GraphStatusCount, error) {
	counts := make(map[string]int)
	for id := 0; id < symbols; id++ {
		for _, edge := range snapshot.Outgoing(hotsnapshot.SymbolID(id)) {
			decoded, isReference, err := decodeReferenceEdge(edge)
			if err != nil {
				return nil, err
			}
			if !isReference {
				return nil, fmt.Errorf("symbol edge from %d has non-reference kind %d", id, edge.Kind)
			}
			counts[string(decoded.Kind)]++
		}
	}
	return sortedStatusCounts(counts), nil
}

func snapshotUnresolvedReasonCounts(snapshot *hotsnapshot.GraphSnapshot) ([]GraphStatusCount, error) {
	counts := make(map[string]int)
	table := snapshot.Strings()
	for _, reference := range snapshot.UnresolvedReferences() {
		reason, found := table.String(reference.Reason)
		if !found {
			return nil, fmt.Errorf("unresolved reference has an invalid reason")
		}
		counts[reason]++
	}
	return sortedStatusCounts(counts), nil
}

func sortedStatusCounts(counts map[string]int) []GraphStatusCount {
	result := make([]GraphStatusCount, 0, len(counts))
	for key, count := range counts {
		result = append(result, GraphStatusCount{Key: key, Count: count})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key < result[j].Key })
	return result
}

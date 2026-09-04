package tools

import (
	"context"
	"fmt"
	"sort"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/freshness"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/metrics"
	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
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

// GraphStatusInput optionally narrows profile discovery to named graphs.
type GraphStatusInput struct {
	Profile []string `json:"profile,omitempty" jsonschema:"Profiles to report; omit or use * alone for all."`
}

// GraphStatus reports what the server is currently serving from. Snapshot and
// host fields are never inferred; metrics are included only when the process
// supplies the optional registry.
type GraphStatus struct {
	ContentFreshness *freshness.Status    `json:"content_freshness,omitempty"`
	Status           string               `json:"status"`
	Profiles         []GraphProfileStatus `json:"profiles,omitempty"`

	SnapshotID        *uint64 `json:"snapshot_id"`
	SnapshotBuiltAt   string  `json:"snapshot_built_at,omitempty"`
	SnapshotAgeMS     *int64  `json:"snapshot_age_ms"`
	SnapshotRowFormat uint32  `json:"snapshot_row_format_version,omitempty"`
	SchemaVersion     int     `json:"schema_version,omitempty"`
	ResolverVersion   string  `json:"resolver_version,omitempty"`
	// SchemaVersionExpected is the canonical schema this binary builds, and
	// SchemaOutdated is the comparison against the one above, stated rather
	// than left for the reader to make.
	//
	// A generation published by an older binary stays readable: the snapshot
	// is a projection with its own row format, so nothing refuses to open it
	// and every query answers. What it cannot do is carry facts its resolver
	// never emitted -- after ADR 0060, the reach of a Go or Rust type built
	// under schema 3 still excludes its methods. That answer looks complete
	// and is not, which is the one thing this tool exists to catch.
	//
	// Reporting is deliberate and refusing is not the same decision: a graph
	// one schema behind is stale, not corrupt, and cutting a session off from
	// a usable answer needs its own measurement.
	SchemaVersionExpected int  `json:"schema_version_expected,omitempty"`
	SchemaOutdated        bool `json:"schema_outdated,omitempty"`
	// SnapshotUnreadable is why the generation this server holds could not be
	// mapped, when that is what happened.
	//
	// The snapshot is read by the first query that needs it rather than at
	// startup (ADR 0067), so a refusal that used to kill the process now reaches
	// a caller instead. `status` is honestly `empty` -- nothing is published --
	// but «empty» alone would read as «never indexed», which is a different
	// problem with a different fix.
	SnapshotUnreadable string `json:"snapshot_unreadable,omitempty"`

	Repositories int `json:"repositories"`
	Packages     int `json:"packages"`
	Files        int `json:"files"`
	Symbols      int `json:"symbols"`
	Evidence     int `json:"evidence"`
	Edges        int `json:"edges"`
	PackageEdges int `json:"package_edges"`
	Unresolved   int `json:"unresolved"`
	// Derived breaks out what the providers Kivgraph built from the machine
	// contribute, so the totals above can be read. The standard library of one
	// toolchain is around twenty thousand symbols: a count that folded it in
	// silently would answer «how big is my code» with a number about Rust.
	Derived *GraphStatusDerived `json:"derived,omitempty"`

	EdgesByKind        []GraphStatusCount `json:"edges_by_kind"`
	UnresolvedByReason []GraphStatusCount `json:"unresolved_by_reason"`

	// RepositoryFreshness carries, per repository, the commit the graph was
	// built from and the commit its working tree holds now. It answers, in
	// the one call an agent makes before trusting anything else, whether
	// what it is about to be told is stale.
	//
	// The array is not named "repositories": that key is the repository
	// count of the snapshot and belongs to the block of counts above.
	RepositoryFreshness []RepositorySummary `json:"repository_freshness"`
	// RepositoriesMoved counts the entries of RepositoryFreshness whose
	// working tree left the indexed commit. A repository whose HEAD could
	// not be read is not one of them and is not silently counted as fresh
	// either; its entry says why.
	RepositoriesMoved int `json:"repositories_moved"`

	LastRebuildAt string          `json:"last_rebuild_at,omitempty"`
	LastUpdateAt  string          `json:"last_update_at,omitempty"`
	Worker        ComponentHealth `json:"worker"`
	Storage       ComponentHealth `json:"storage"`

	// Metrics is present only when the hosting process wires a metrics
	// registry; an absent registry must not be reported as healthy or empty
	// metrics.
	Metrics *metrics.Report `json:"metrics,omitempty"`
}

// GraphProfileStatus is the discovery row graph_status owns once an
// installation contains more than one profile.
type GraphProfileStatus struct {
	Name         string  `json:"name"`
	SnapshotID   *uint64 `json:"snapshot_id"`
	Repositories int     `json:"repositories"`
	Default      bool    `json:"default,omitempty"`
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
	ContentFreshness *freshness.Status
	LastRebuildAt    time.Time
	LastUpdateAt     time.Time
	Worker           ComponentHealth
	Storage          ComponentHealth
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
		arguments GraphStatusInput,
	) (*sdkmcp.CallToolResult, Response[GraphStatus], error) {
		if snapshotStore == nil {
			return graphStatus(ctx, request, struct{}{}, nil, probe, registry)
		}
		requested := arguments.Profile
		if len(requested) == 0 && snapshotStore.ProfileCount() > 1 {
			requested = []string{"*"}
		}
		selected, selectionErr := snapshotStore.ResolveProfiles(requested)
		if selectionErr != nil {
			return nil, Response[GraphStatus]{}, WrapToolError(CodeInvalidArgument, selectionErr.Error(), selectionErr)
		}
		if len(selected) > 1 {
			return graphStatusAcrossProfiles(ctx, request, selected, snapshotStore.DefaultProfileName(), probe, registry)
		}
		store, profile, count := selected[0].Store, selected[0].Name, snapshotStore.ProfileCount()
		result, response, err := graphStatus(ctx, request, struct{}{}, store, probe, registry)
		// The service probe attests only its default profile. Generation IDs
		// are profile-local, so equal IDs do not make another profile fresh.
		if profile != snapshotStore.DefaultProfileName() {
			response.Results.ContentFreshness = nil
		}
		scopeResponse(&response, profile, count)
		return result, response, err
	}
	if observer != nil || callObserver != nil {
		underlying := handler
		handler = func(
			ctx context.Context,
			request *sdkmcp.CallToolRequest,
			arguments GraphStatusInput,
		) (*sdkmcp.CallToolResult, Response[GraphStatus], error) {
			start := time.Now()
			result, status, err := underlying(ctx, request, arguments)
			observe(observer, callObserver, graphStatusToolName, start, status, err)
			return result, status, err
		}
	}
	addQueryTool(server, &sdkmcp.Tool{
		Name:        graphStatusToolName,
		Description: "The published generation: counts, provenance, and whether a repository moved since it was indexed. Call it when an answer looks stale.",
		Annotations: readOnlyClosedWorld(),
	}, handler)
}

func graphStatusAcrossProfiles(
	ctx context.Context,
	request *sdkmcp.CallToolRequest,
	selected []hotsnapshot.ProfileStore,
	defaultProfile string,
	probe HostStatusProbe,
	registry *metrics.Registry,
) (*sdkmcp.CallToolResult, Response[GraphStatus], error) {
	combined := GraphStatus{
		Status: GraphStatusReady, Profiles: make([]GraphProfileStatus, 0, len(selected)),
		EdgesByKind: []GraphStatusCount{}, UnresolvedByReason: []GraphStatusCount{},
		RepositoryFreshness: []RepositorySummary{},
	}
	profiles := make([]ProfileSnapshot, 0, len(selected))
	edgesByKind := make(map[string]int)
	unresolvedByReason := make(map[string]int)
	for index, profile := range selected {
		profileProbe, profileRegistry := HostStatusProbe(nil), (*metrics.Registry)(nil)
		if index == 0 {
			profileProbe, profileRegistry = probe, registry
		}
		_, response, err := graphStatus(ctx, request, struct{}{}, profile.Store, profileProbe, profileRegistry)
		if err != nil {
			return nil, Response[GraphStatus]{}, err
		}
		status := response.Results
		if index == 0 {
			combined.Worker, combined.Storage = status.Worker, status.Storage
			combined.LastRebuildAt, combined.LastUpdateAt = status.LastRebuildAt, status.LastUpdateAt
			combined.Metrics = status.Metrics
		}
		combined.SchemaOutdated = combined.SchemaOutdated || status.SchemaOutdated
		if status.Status != GraphStatusReady {
			combined.Status = GraphStatusEmpty
		}
		combined.Repositories += status.Repositories
		combined.Packages += status.Packages
		combined.Files += status.Files
		combined.Symbols += status.Symbols
		combined.Evidence += status.Evidence
		combined.Edges += status.Edges
		combined.PackageEdges += status.PackageEdges
		combined.Unresolved += status.Unresolved
		combined.RepositoriesMoved += status.RepositoriesMoved
		for _, count := range status.EdgesByKind {
			edgesByKind[count.Key] += count.Count
		}
		for _, count := range status.UnresolvedByReason {
			unresolvedByReason[count.Key] += count.Count
		}
		for _, repository := range status.RepositoryFreshness {
			repository.Profile = profile.Name
			combined.RepositoryFreshness = append(combined.RepositoryFreshness, repository)
		}
		if status.Derived != nil {
			if combined.Derived == nil {
				combined.Derived = &GraphStatusDerived{}
			}
			combined.Derived.Repositories = append(combined.Derived.Repositories, status.Derived.Repositories...)
			combined.Derived.Packages += status.Derived.Packages
			combined.Derived.Files += status.Derived.Files
			combined.Derived.Symbols += status.Derived.Symbols
			combined.Derived.EdgesWithin += status.Derived.EdgesWithin
			combined.Derived.EdgesInbound += status.Derived.EdgesInbound
			combined.Derived.Unresolved += status.Derived.Unresolved
		}
		combined.Profiles = append(combined.Profiles, GraphProfileStatus{
			Name: profile.Name, SnapshotID: response.SnapshotID,
			Repositories: status.Repositories, Default: profile.Name == defaultProfile,
		})
		profileSnapshot := ProfileSnapshot{Name: profile.Name}
		if response.SnapshotID != nil {
			profileSnapshot.SnapshotID = *response.SnapshotID
		}
		profiles = append(profiles, profileSnapshot)
	}
	combined.EdgesByKind = sortedStatusCounts(edgesByKind)
	combined.UnresolvedByReason = sortedStatusCounts(unresolvedByReason)
	return nil, Response[GraphStatus]{
		Profiles:          profiles,
		CrossProfileEdges: "not_resolved",
		Total:             len(combined.Profiles),
		Returned:          len(combined.Profiles),
		Results:           combined,
	}, nil
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
		Status:              GraphStatusEmpty,
		EdgesByKind:         []GraphStatusCount{},
		UnresolvedByReason:  []GraphStatusCount{},
		RepositoryFreshness: []RepositorySummary{},
		Worker: ComponentHealth{
			State:  HealthNotApplicable,
			Detail: "the TypeScript worker runs during indexing, not in this server",
		},
		Storage: ComponentHealth{
			State:  HealthNotApplicable,
			Detail: "this server answers from the published snapshot and never opens the database",
		},
	}
	// Pin the response before probing: a host may observe a publication while
	// it reads its cached freshness evidence. Loading afterwards could attach
	// the old evidence to the new graph.
	var snapshot *hotsnapshot.GraphSnapshot
	if snapshotStore != nil {
		snapshot = snapshotStore.Load()
	}
	if err := applyHostStatus(ctx, &status, probe); err != nil {
		return nil, Response[GraphStatus]{}, err
	}
	if evidence := status.ContentFreshness; evidence != nil {
		generationless := evidence.Generation == 0 && (evidence.State == "fresh" || evidence.State == "stale")
		generationChanged := evidence.Generation > 0 &&
			(snapshot == nil || evidence.Generation != snapshot.Metadata().ID)
		if generationless || generationChanged {
			pinned := *evidence
			pinned.State = "unverified"
			if generationless {
				pinned.Detail = "freshness state has no published generation"
			} else {
				pinned.Detail = "generation changed during freshness check; retry graph_status"
			}
			status.ContentFreshness = &pinned
		}
	}
	if snapshot == nil {
		// A generation that could not be mapped is not the same state as no
		// generation at all, and this is the only tool that can say so: every
		// other one answers INDEX_NOT_READY for both.
		if failure := snapshotStore.LoadFailure(); failure != nil {
			status.SnapshotUnreadable = failure.Error()
		}
		applyMetricsStatus(&status, registry)
		return nil, Response[GraphStatus]{Total: 1, Returned: 1, Results: status}, nil
	}
	if err := applySnapshotStatus(ctx, &status, snapshot); err != nil {
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
	status.ContentFreshness = host.ContentFreshness
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

func applySnapshotStatus(ctx context.Context, status *GraphStatus, snapshot *hotsnapshot.GraphSnapshot) error {
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
	status.SchemaVersionExpected = ladybug.CanonicalSchemaVersion
	// Only older counts as outdated. A snapshot from a newer schema is a
	// different problem -- an old binary reading a new graph -- and calling it
	// "outdated" would point at the wrong side.
	status.SchemaOutdated = metadata.SchemaVersion > 0 &&
		metadata.SchemaVersion < ladybug.CanonicalSchemaVersion
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
	freshness, moved, err := snapshotRepositoryFreshness(snapshot, int(counts.Repositories))
	if err != nil {
		return err
	}
	status.RepositoryFreshness = freshness
	status.RepositoriesMoved = moved
	derived, err := snapshotDerivedCounts(ctx, snapshot)
	if err != nil {
		return err
	}
	status.Derived = derived
	return nil
}

// snapshotRepositoryFreshness describes every repository of the snapshot and
// counts the ones whose working tree left the commit the graph was built from.
//
// It reads the HEAD of each repository, which is what makes the answer worth
// anything: a status that only repeats what the snapshot remembers cannot
// tell a caller that the snapshot is no longer true.
func snapshotRepositoryFreshness(snapshot *hotsnapshot.GraphSnapshot, repositories int) ([]RepositorySummary, int, error) {
	summaries := make([]RepositorySummary, 0, repositories)
	moved := 0
	for index := range repositories {
		record, found := snapshot.Repository(hotsnapshot.RepositoryID(index))
		if !found {
			return nil, 0, fmt.Errorf("repository index %d is missing", index)
		}
		summary, err := repositorySummary(snapshot, record)
		if err != nil {
			return nil, 0, err
		}
		if summary.Moved {
			moved++
		}
		summaries = append(summaries, summary)
	}
	return summaries, moved, nil
}

// snapshotEdgeKindCounts walks the forward CSR once. Every symbol edge is
// counted exactly once because the forward adjacency holds each edge under its
// source; the reverse CSR is the same multiset seen from the other end.
//
// Every kind is counted, not only the ones find_references answers with. This
// breakdown sits under the `edges` count and has to add up to it: METHOD_OF is
// a symbol edge the graph genuinely holds, and refusing it made graph_status
// fail outright on every corpus indexed after ADR 0060. An unknown code is a
// different thing -- a row this build cannot read -- and still fails.
func snapshotEdgeKindCounts(snapshot *hotsnapshot.GraphSnapshot, symbols int) ([]GraphStatusCount, error) {
	counts := make(map[string]int)
	for id := 0; id < symbols; id++ {
		for _, edge := range snapshot.Outgoing(hotsnapshot.SymbolID(id)) {
			kind, err := facts.EdgeKindFromCode(edge.Kind)
			if err != nil {
				return nil, fmt.Errorf("symbol edge from %d: %w", id, err)
			}
			counts[string(kind)]++
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

// GraphStatusDerived is what the providers Kivgraph derived from the machine
// contribute to the totals of the graph.
//
// Only the counts that can be attributed to a repository are here. An edge is
// attributed to the repository of its source symbol, which is the side that
// made the observation: the whole point of indexing the standard library is that
// code in a registered repository now reaches it, and counting those edges as
// the standard library's own would hide exactly that.
type GraphStatusDerived struct {
	// Repositories names every derived provider in the snapshot.
	Repositories []string `json:"repositories"`
	Packages     int      `json:"packages"`
	Files        int      `json:"files"`
	Symbols      int      `json:"symbols"`
	// EdgesWithin counts the edges whose source is a symbol of a derived
	// provider: the standard library referring to itself.
	EdgesWithin int `json:"edges_within"`
	// EdgesInbound counts the edges that leave a registered repository and
	// land in a derived provider. This is the number the feature exists for.
	EdgesInbound int `json:"edges_inbound"`
	// Unresolved counts the gaps the derived provider declares about its own
	// code. The standard library declares thousands -- most of its arithmetic is
	// generated by macros -- and they are true and none of them the caller's, so
	// the total above only reads as «what my code is missing» with this beside it.
	Unresolved int `json:"unresolved"`
}

// snapshotDerivedCounts breaks out the derived providers, or nil when the graph
// holds none. A section reporting zeros would claim a measurement nobody made.
func snapshotDerivedCounts(ctx context.Context, snapshot *hotsnapshot.GraphSnapshot) (*GraphStatusDerived, error) {
	table := snapshot.Strings()
	counts := snapshot.Metadata().Counts
	derived := make(map[hotsnapshot.RepositoryID]struct{})
	names := make([]string, 0, 1)
	err := snapshot.VisitRepositories(ctx, 0, hotsnapshot.RepositoryID(counts.Repositories),
		func(id hotsnapshot.RepositoryID, record hotsnapshot.RepositoryRecord) error {
			name, found := table.String(record.Name)
			if !found {
				return fmt.Errorf("repository %d has an invalid name", id)
			}
			if !facts.IsSyntheticRepository(name) {
				return nil
			}
			derived[id] = struct{}{}
			names = append(names, name)
			return nil
		})
	if err != nil {
		return nil, err
	}
	if len(derived) == 0 {
		return nil, nil
	}
	sort.Strings(names)
	report := &GraphStatusDerived{Repositories: names}
	if err := snapshot.VisitPackages(ctx, 0, hotsnapshot.PackageID(counts.Packages),
		func(_ hotsnapshot.PackageID, record hotsnapshot.PackageRecord) error {
			if _, found := derived[record.Repository]; found {
				report.Packages++
			}
			return nil
		}); err != nil {
		return nil, err
	}
	fromDerived := make(map[hotsnapshot.FileID]struct{})
	if err := snapshot.VisitFiles(ctx, 0, hotsnapshot.FileID(counts.Files),
		func(id hotsnapshot.FileID, record hotsnapshot.FileRecord) error {
			if _, found := derived[record.Repository]; found {
				report.Files++
				fromDerived[id] = struct{}{}
			}
			return nil
		}); err != nil {
		return nil, err
	}
	symbolIsDerived := make(map[hotsnapshot.SymbolID]bool, counts.Symbols)
	if err := snapshot.VisitSymbols(ctx, 0, hotsnapshot.SymbolID(counts.Symbols),
		func(id hotsnapshot.SymbolID, record hotsnapshot.SymbolRecord) error {
			_, found := fromDerived[record.File]
			symbolIsDerived[id] = found
			if found {
				report.Symbols++
			}
			return nil
		}); err != nil {
		return nil, err
	}
	for _, reference := range snapshot.UnresolvedReferences() {
		if _, found := derived[reference.Repository]; found {
			report.Unresolved++
		}
	}
	// The symbol CSR is indexed by source, so walking it attributes every edge
	// to the repository that observed it without a second lookup.
	for id := 0; id < int(counts.Symbols); id++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		source := symbolIsDerived[hotsnapshot.SymbolID(id)]
		for _, edge := range snapshot.Outgoing(hotsnapshot.SymbolID(id)) {
			switch {
			case source:
				report.EdgesWithin++
			case symbolIsDerived[edge.Target]:
				report.EdgesInbound++
			}
		}
	}
	return report, nil
}

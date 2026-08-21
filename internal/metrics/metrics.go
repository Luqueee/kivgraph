// Package metrics provides the process-local metrics registry used by Kivgraph.
//
// The registry deliberately has no exporter dependency. Query counters use
// atomics for accumulation after a shared lookup; lifecycle and storage
// observations update a small locked state. An optional OpenTelemetry bridge
// forwards the same observations without changing the base registry contract.
package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	QueryDuration          = "kivgraph_query_duration"
	QueryTotal             = "kivgraph_query_total"
	QueryErrors            = "kivgraph_query_errors"
	QueryResults           = "kivgraph_query_results"
	QueryTruncated         = "kivgraph_query_truncated"
	QueryUnresolvedRelated = "kivgraph_query_unresolved_related"
	SnapshotID             = "kivgraph_snapshot_id"
	SnapshotAge            = "kivgraph_snapshot_age"
	SnapshotBuildDuration  = "kivgraph_snapshot_build_duration"
	SnapshotBytes          = "kivgraph_snapshot_bytes"
	IndexDuration          = "kivgraph_index_duration"
	IndexFiles             = "kivgraph_index_files"
	IndexSymbols           = "kivgraph_index_symbols"
	IndexEdges             = "kivgraph_index_edges"
	IndexUnresolved        = "kivgraph_index_unresolved"
	LadybugTransaction     = "kivgraph_ladybug_transaction_duration"
	LadybugDatabaseBytes   = "kivgraph_ladybug_database_bytes"
	TSWorkerRestarts       = "kivgraph_ts_worker_restarts"
	TSWorkerMemory         = "kivgraph_ts_worker_memory"
)

// Registry is a process-local metrics collector. It is safe for concurrent
// observations and reports. Query observations are lock-free after a tool name
// has been seen once.
type Registry struct {
	mu       sync.RWMutex
	queries  map[string]*queryCounter
	state    lifecycleState
	otel     *OpenTelemetry
	recorder QueryRecorder
}

// QueryRecorder receives every observed query. It is how an observation
// outlives the process that made it: the counters in this registry are minted
// fresh by each server and discarded when it exits, so a reader running as its
// own process can only see what a recorder wrote down.
//
// It runs on the calling goroutine, inside the hot path of a tool call, so an
// implementation must not block.
type QueryRecorder func(QueryObservation)

// NewRegistry creates an empty metrics registry.
func NewRegistry() *Registry {
	return &Registry{queries: make(map[string]*queryCounter)}
}

// NewRegistryWithRecorder creates a registry that also hands every query
// observation to recorder. A nil recorder yields a plain registry.
func NewRegistryWithRecorder(recorder QueryRecorder) *Registry {
	registry := NewRegistry()
	registry.recorder = recorder
	return registry
}

// QueryObservation is the completed result of one MCP query handler.
type QueryObservation struct {
	ToolName          string
	Elapsed           time.Duration
	Returned          int
	Truncated         bool
	UnresolvedRelated int
	SnapshotID        *uint64
	SnapshotAgeMS     *int64
	Err               error
}

// QueryMetrics is the cumulative view for one tool. Latency is retained as
// count, sum, and maximum rather than an unbounded sample list.
type QueryMetrics struct {
	Calls             uint64        `json:"calls"`
	Errors            uint64        `json:"errors"`
	Results           uint64        `json:"results"`
	Truncated         uint64        `json:"truncated"`
	UnresolvedRelated uint64        `json:"unresolved_related"`
	LatencyCount      uint64        `json:"latency_count"`
	LatencyTotal      time.Duration `json:"latency_total"`
	LatencyMax        time.Duration `json:"latency_max"`
}

// SnapshotObservation records the immutable snapshot currently available to
// readers. CreatedAt is retained so Report can derive a live age.
type SnapshotObservation struct {
	ID            uint64
	CreatedAt     time.Time
	BuildDuration time.Duration
	Bytes         int64
}

// SnapshotMetrics is the current snapshot gauge set.
type SnapshotMetrics struct {
	ID            uint64        `json:"id"`
	CreatedAt     time.Time     `json:"created_at"`
	Age           time.Duration `json:"age"`
	BuildDuration time.Duration `json:"build_duration"`
	Bytes         int64         `json:"bytes"`
}

// IndexObservation records the latest completed indexing operation.
type IndexObservation struct {
	Duration   time.Duration
	Files      uint64
	Symbols    uint64
	Edges      uint64
	Unresolved uint64
}

// IndexMetrics is the latest index operation gauge set.
type IndexMetrics struct {
	Duration   time.Duration `json:"duration"`
	Files      uint64        `json:"files"`
	Symbols    uint64        `json:"symbols"`
	Edges      uint64        `json:"edges"`
	Unresolved uint64        `json:"unresolved"`
}

// WorkerObservation records the current TypeScript worker state.
type WorkerObservation struct {
	Restarts    uint64
	MemoryBytes int64
}

// WorkerMetrics is the current TypeScript worker gauge set.
type WorkerMetrics struct {
	Restarts    uint64 `json:"restarts"`
	MemoryBytes int64  `json:"memory_bytes"`
}

// LadybugObservation records one LadybugDB transaction and the latest database
// size observed by the caller.
type LadybugObservation struct {
	TransactionDuration time.Duration
	DatabaseBytes       int64
}

// LadybugMetrics is the cumulative transaction view and latest size gauge.
type LadybugMetrics struct {
	Transactions     uint64        `json:"transactions"`
	TransactionTotal time.Duration `json:"transaction_total"`
	TransactionMax   time.Duration `json:"transaction_max"`
	DatabaseBytes    int64         `json:"database_bytes"`
}

// Report is a consistent copy of all metrics available from the registry.
// Report is what one process measured. A section is absent when this process
// never observed it: `kivgraph serve` records queries and the snapshot it
// loaded, and never indexes anything, so reporting an index that took zero
// seconds over zero files would describe work that happened somewhere else --
// and read exactly like a graph that is empty.
type Report struct {
	Queries  map[string]QueryMetrics `json:"queries"`
	Snapshot *SnapshotMetrics        `json:"snapshot,omitempty"`
	Index    *IndexMetrics           `json:"index,omitempty"`
	Worker   *WorkerMetrics          `json:"worker,omitempty"`
	Ladybug  *LadybugMetrics         `json:"ladybug,omitempty"`
}

type queryCounter struct {
	calls             atomic.Uint64
	errors            atomic.Uint64
	results           atomic.Uint64
	truncated         atomic.Uint64
	unresolvedRelated atomic.Uint64
	latencyCount      atomic.Uint64
	latencyTotal      atomic.Uint64
	latencyMax        atomic.Uint64
}

type lifecycleState struct {
	snapshot SnapshotMetrics
	index    IndexMetrics
	worker   WorkerMetrics
	ladybug  LadybugMetrics

	// Observed marks the sections this process actually recorded, so a
	// report can leave out what it never measured instead of reporting it
	// as zero.
	observedSnapshot bool
	observedIndex    bool
	observedWorker   bool
	observedLadybug  bool
}

// ObserveQuery records one completed query. Negative values are clamped so a
// malformed caller cannot make counters wrap through unsigned arithmetic.
func (r *Registry) ObserveQuery(observation QueryObservation) {
	if r == nil || observation.ToolName == "" {
		return
	}
	counter := r.queryCounter(observation.ToolName)
	counter.calls.Add(1)
	if observation.Err != nil {
		counter.errors.Add(1)
	}
	if observation.Returned > 0 {
		counter.results.Add(uint64(observation.Returned))
	}
	if observation.UnresolvedRelated > 0 {
		counter.unresolvedRelated.Add(uint64(observation.UnresolvedRelated))
	}
	if observation.Truncated {
		counter.truncated.Add(1)
	}
	elapsed := nonNegativeDuration(observation.Elapsed)
	counter.latencyCount.Add(1)
	counter.latencyTotal.Add(uint64(elapsed))
	updateMax(&counter.latencyMax, uint64(elapsed))

	if observation.SnapshotID != nil || observation.SnapshotAgeMS != nil {
		r.mu.Lock()
		r.state.observedSnapshot = true
		if observation.SnapshotID != nil {
			r.state.snapshot.ID = *observation.SnapshotID
		}
		if observation.SnapshotAgeMS != nil {
			r.state.snapshot.Age = nonNegativeMilliseconds(*observation.SnapshotAgeMS)
		}
		r.mu.Unlock()
	}
	if r.otel != nil {
		r.otel.observeQuery(observation)
	}
	if r.recorder != nil {
		r.recorder(observation)
	}
}

// ObserveSnapshot replaces the current snapshot gauges.
func (r *Registry) ObserveSnapshot(observation SnapshotObservation) {
	if r == nil {
		return
	}
	normalized := SnapshotObservation{
		ID:            observation.ID,
		CreatedAt:     observation.CreatedAt,
		BuildDuration: nonNegativeDuration(observation.BuildDuration),
		Bytes:         nonNegativeInt64(observation.Bytes),
	}
	r.mu.Lock()
	r.state.observedSnapshot = true
	r.state.snapshot = SnapshotMetrics{
		ID:            normalized.ID,
		CreatedAt:     normalized.CreatedAt,
		BuildDuration: normalized.BuildDuration,
		Bytes:         normalized.Bytes,
	}
	r.mu.Unlock()
	if r.otel != nil {
		r.otel.observeSnapshot(normalized)
	}
}

// ObserveIndex replaces the latest index gauges.
func (r *Registry) ObserveIndex(observation IndexObservation) {
	if r == nil {
		return
	}
	normalized := IndexObservation{
		Duration:   nonNegativeDuration(observation.Duration),
		Files:      observation.Files,
		Symbols:    observation.Symbols,
		Edges:      observation.Edges,
		Unresolved: observation.Unresolved,
	}
	r.mu.Lock()
	r.state.observedIndex = true
	r.state.index = IndexMetrics{
		Duration:   normalized.Duration,
		Files:      normalized.Files,
		Symbols:    normalized.Symbols,
		Edges:      normalized.Edges,
		Unresolved: normalized.Unresolved,
	}
	r.mu.Unlock()
	if r.otel != nil {
		r.otel.observeIndex(normalized)
	}
}

// ObserveWorker replaces the current TypeScript worker gauges.
func (r *Registry) ObserveWorker(observation WorkerObservation) {
	if r == nil {
		return
	}
	normalized := WorkerObservation{
		Restarts:    observation.Restarts,
		MemoryBytes: nonNegativeInt64(observation.MemoryBytes),
	}
	r.mu.Lock()
	r.state.observedWorker = true
	r.state.worker = WorkerMetrics{
		Restarts:    normalized.Restarts,
		MemoryBytes: normalized.MemoryBytes,
	}
	r.mu.Unlock()
	if r.otel != nil {
		r.otel.observeWorker(normalized)
	}
}

// RecordWorkerRestart increments the TypeScript worker restart counter without
// changing the last memory gauge.
func (r *Registry) RecordWorkerRestart() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.state.observedWorker = true
	r.state.worker.Restarts++
	observation := WorkerObservation{
		Restarts:    r.state.worker.Restarts,
		MemoryBytes: r.state.worker.MemoryBytes,
	}
	r.mu.Unlock()
	if r.otel != nil {
		r.otel.observeWorker(observation)
	}
}

// ObserveLadybug records one LadybugDB transaction and replaces the database
// size gauge with the latest non-negative observation.
func (r *Registry) ObserveLadybug(observation LadybugObservation) {
	if r == nil {
		return
	}
	normalized := LadybugObservation{
		TransactionDuration: nonNegativeDuration(observation.TransactionDuration),
		DatabaseBytes:       nonNegativeInt64(observation.DatabaseBytes),
	}
	r.mu.Lock()
	r.state.observedLadybug = true
	r.state.ladybug.Transactions++
	r.state.ladybug.TransactionTotal += normalized.TransactionDuration
	if normalized.TransactionDuration > r.state.ladybug.TransactionMax {
		r.state.ladybug.TransactionMax = normalized.TransactionDuration
	}
	r.state.ladybug.DatabaseBytes = normalized.DatabaseBytes
	r.mu.Unlock()
	if r.otel != nil {
		r.otel.observeLadybug(normalized)
	}
}

// Report returns a copy of the current metrics and computes snapshot age from
// the current clock when CreatedAt is available.
func (r *Registry) Report() Report {
	return r.ReportAt(time.Now())
}

// ReportAt is Report with an explicit clock, useful for deterministic callers
// and tests.
func (r *Registry) ReportAt(now time.Time) Report {
	if r == nil {
		return Report{Queries: map[string]QueryMetrics{}}
	}
	r.mu.RLock()
	state := r.state
	queries := make(map[string]QueryMetrics, len(r.queries))
	for name, counter := range r.queries {
		queries[name] = QueryMetrics{
			Calls:             counter.calls.Load(),
			Errors:            counter.errors.Load(),
			Results:           counter.results.Load(),
			Truncated:         counter.truncated.Load(),
			UnresolvedRelated: counter.unresolvedRelated.Load(),
			LatencyCount:      counter.latencyCount.Load(),
			LatencyTotal:      time.Duration(counter.latencyTotal.Load()),
			LatencyMax:        time.Duration(counter.latencyMax.Load()),
		}
	}
	r.mu.RUnlock()

	if !state.snapshot.CreatedAt.IsZero() {
		state.snapshot.Age = nonNegativeDuration(now.Sub(state.snapshot.CreatedAt))
	}
	report := Report{Queries: queries}
	if state.observedSnapshot {
		snapshot := state.snapshot
		report.Snapshot = &snapshot
	}
	if state.observedIndex {
		index := state.index
		report.Index = &index
	}
	if state.observedWorker {
		worker := state.worker
		report.Worker = &worker
	}
	if state.observedLadybug {
		ladybug := state.ladybug
		report.Ladybug = &ladybug
	}
	return report
}

func (r *Registry) queryCounter(name string) *queryCounter {
	r.mu.RLock()
	counter := r.queries[name]
	r.mu.RUnlock()
	if counter != nil {
		return counter
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.queries == nil {
		r.queries = make(map[string]*queryCounter)
	}
	if counter = r.queries[name]; counter == nil {
		counter = &queryCounter{}
		r.queries[name] = counter
	}
	return counter
}

func updateMax(destination *atomic.Uint64, candidate uint64) {
	for current := destination.Load(); candidate > current; {
		if destination.CompareAndSwap(current, candidate) {
			return
		}
	}
}

func nonNegativeDuration(value time.Duration) time.Duration {
	if value < 0 {
		return 0
	}
	return value
}

func nonNegativeMilliseconds(value int64) time.Duration {
	if value <= 0 {
		return 0
	}
	maxMilliseconds := int64((time.Duration(1<<63 - 1)) / time.Millisecond)
	if value > maxMilliseconds {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(value) * time.Millisecond
}

func nonNegativeInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

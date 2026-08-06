// Package metrics provides the process-local metrics registry used by Ladygraph.
//
// The registry deliberately has no exporter dependency. Query counters use
// atomics for accumulation after a shared lookup; lifecycle and storage
// observations update a small locked state. An exporter can consume Report
// without changing callers.
package metrics

import (
	"sync"
	"sync/atomic"
	"time"
)

const (
	QueryDuration         = "ladygraph_query_duration"
	QueryTotal            = "ladygraph_query_total"
	QueryErrors           = "ladygraph_query_errors"
	QueryResults          = "ladygraph_query_results"
	QueryTruncated        = "ladygraph_query_truncated"
	SnapshotID            = "ladygraph_snapshot_id"
	SnapshotAge           = "ladygraph_snapshot_age"
	SnapshotBuildDuration = "ladygraph_snapshot_build_duration"
	SnapshotBytes         = "ladygraph_snapshot_bytes"
	IndexDuration         = "ladygraph_index_duration"
	IndexFiles            = "ladygraph_index_files"
	IndexSymbols          = "ladygraph_index_symbols"
	IndexEdges            = "ladygraph_index_edges"
	IndexUnresolved       = "ladygraph_index_unresolved"
	LadybugTransaction    = "ladygraph_ladybug_transaction_duration"
	LadybugDatabaseBytes  = "ladygraph_ladybug_database_bytes"
	TSWorkerRestarts      = "ladygraph_ts_worker_restarts"
	TSWorkerMemory        = "ladygraph_ts_worker_memory"
)

// Registry is a process-local metrics collector. It is safe for concurrent
// observations and reports. Query observations are lock-free after a tool name
// has been seen once.
type Registry struct {
	mu      sync.RWMutex
	queries map[string]*queryCounter
	state   lifecycleState
}

// NewRegistry creates an empty metrics registry.
func NewRegistry() *Registry {
	return &Registry{queries: make(map[string]*queryCounter)}
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
type Report struct {
	Queries  map[string]QueryMetrics `json:"queries"`
	Snapshot SnapshotMetrics         `json:"snapshot"`
	Index    IndexMetrics            `json:"index"`
	Worker   WorkerMetrics           `json:"worker"`
	Ladybug  LadybugMetrics          `json:"ladybug"`
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
		if observation.SnapshotID != nil {
			r.state.snapshot.ID = *observation.SnapshotID
		}
		if observation.SnapshotAgeMS != nil {
			r.state.snapshot.Age = nonNegativeMilliseconds(*observation.SnapshotAgeMS)
		}
		r.mu.Unlock()
	}
}

// ObserveSnapshot replaces the current snapshot gauges.
func (r *Registry) ObserveSnapshot(observation SnapshotObservation) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.state.snapshot = SnapshotMetrics{
		ID:            observation.ID,
		CreatedAt:     observation.CreatedAt,
		BuildDuration: nonNegativeDuration(observation.BuildDuration),
		Bytes:         nonNegativeInt64(observation.Bytes),
	}
	r.mu.Unlock()
}

// ObserveIndex replaces the latest index gauges.
func (r *Registry) ObserveIndex(observation IndexObservation) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.state.index = IndexMetrics{
		Duration:   nonNegativeDuration(observation.Duration),
		Files:      observation.Files,
		Symbols:    observation.Symbols,
		Edges:      observation.Edges,
		Unresolved: observation.Unresolved,
	}
	r.mu.Unlock()
}

// ObserveWorker replaces the current TypeScript worker gauges.
func (r *Registry) ObserveWorker(observation WorkerObservation) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.state.worker = WorkerMetrics{
		Restarts:    observation.Restarts,
		MemoryBytes: nonNegativeInt64(observation.MemoryBytes),
	}
	r.mu.Unlock()
}

// RecordWorkerRestart increments the TypeScript worker restart counter without
// changing the last memory gauge.
func (r *Registry) RecordWorkerRestart() {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.state.worker.Restarts++
	r.mu.Unlock()
}

// ObserveLadybug records one LadybugDB transaction and replaces the database
// size gauge with the latest non-negative observation.
func (r *Registry) ObserveLadybug(observation LadybugObservation) {
	if r == nil {
		return
	}
	elapsed := nonNegativeDuration(observation.TransactionDuration)
	r.mu.Lock()
	r.state.ladybug.Transactions++
	r.state.ladybug.TransactionTotal += elapsed
	if elapsed > r.state.ladybug.TransactionMax {
		r.state.ladybug.TransactionMax = elapsed
	}
	r.state.ladybug.DatabaseBytes = nonNegativeInt64(observation.DatabaseBytes)
	r.mu.Unlock()
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
	return Report{Queries: queries, Snapshot: state.snapshot, Index: state.index, Worker: state.worker, Ladybug: state.ladybug}
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

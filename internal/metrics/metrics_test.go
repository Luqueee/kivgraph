package metrics

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestReportAggregatesQueryAndLifecycleMetrics(t *testing.T) {
	registry := NewRegistry()
	clock := time.Unix(1_700_000_000, 0).UTC()
	snapshotID := uint64(7)
	ageMS := int64(12)

	registry.ObserveQuery(QueryObservation{
		ToolName:          "find_symbol",
		Elapsed:           3 * time.Millisecond,
		Returned:          4,
		Truncated:         true,
		UnresolvedRelated: 2,
		SnapshotID:        &snapshotID,
		SnapshotAgeMS:     &ageMS,
	})
	registry.ObserveQuery(QueryObservation{
		ToolName:   "find_symbol",
		Elapsed:    5 * time.Millisecond,
		Returned:   1,
		Err:        errors.New("not found"),
		SnapshotID: &snapshotID,
	})
	registry.ObserveQuery(QueryObservation{ToolName: "list_repositories", Elapsed: -time.Second, Returned: -1})

	registry.ObserveSnapshot(SnapshotObservation{
		ID:            snapshotID,
		CreatedAt:     clock.Add(-250 * time.Millisecond),
		BuildDuration: 11 * time.Millisecond,
		Bytes:         -1,
	})
	registry.ObserveIndex(IndexObservation{
		Duration:   20 * time.Millisecond,
		Files:      3,
		Symbols:    8,
		Edges:      13,
		Unresolved: 2,
	})
	registry.ObserveWorker(WorkerObservation{Restarts: 2, MemoryBytes: -1})
	registry.ObserveLadybug(LadybugObservation{TransactionDuration: 7 * time.Millisecond, DatabaseBytes: 1234})
	registry.ObserveLadybug(LadybugObservation{TransactionDuration: 9 * time.Millisecond, DatabaseBytes: 2345})

	report := registry.ReportAt(clock)
	find := report.Queries["find_symbol"]
	if find.Calls != 2 || find.Errors != 1 || find.Results != 5 || find.Truncated != 1 || find.UnresolvedRelated != 2 {
		t.Fatalf("find_symbol metrics = %+v", find)
	}
	if find.LatencyCount != 2 || find.LatencyTotal != 8*time.Millisecond || find.LatencyMax != 5*time.Millisecond {
		t.Fatalf("find_symbol latency = %+v", find)
	}
	list := report.Queries["list_repositories"]
	if list.Calls != 1 || list.Results != 0 || list.LatencyTotal != 0 || list.LatencyMax != 0 {
		t.Fatalf("list_repositories metrics = %+v", list)
	}
	if report.Snapshot.ID != snapshotID || report.Snapshot.Age != 250*time.Millisecond || report.Snapshot.BuildDuration != 11*time.Millisecond || report.Snapshot.Bytes != 0 {
		t.Fatalf("snapshot metrics = %+v", report.Snapshot)
	}
	if report.Index.Duration != 20*time.Millisecond || report.Index.Files != 3 || report.Index.Symbols != 8 || report.Index.Edges != 13 || report.Index.Unresolved != 2 {
		t.Fatalf("index metrics = %+v", report.Index)
	}
	if report.Worker.Restarts != 2 || report.Worker.MemoryBytes != 0 {
		t.Fatalf("worker metrics = %+v", report.Worker)
	}
	if report.Ladybug.Transactions != 2 || report.Ladybug.TransactionTotal != 16*time.Millisecond || report.Ladybug.TransactionMax != 9*time.Millisecond || report.Ladybug.DatabaseBytes != 2345 {
		t.Fatalf("Ladybug metrics = %+v", report.Ladybug)
	}
}

func TestRegistryIsSafeForConcurrentQueries(t *testing.T) {
	registry := NewRegistry()
	const workers = 8
	const observations = 1000
	var group sync.WaitGroup
	group.Add(workers)
	for worker := 0; worker < workers; worker++ {
		go func() {
			defer group.Done()
			for range observations {
				registry.ObserveQuery(QueryObservation{
					ToolName: "trace_dependencies",
					Elapsed:  time.Microsecond,
					Returned: 1,
				})
			}
		}()
	}
	group.Wait()

	got := registry.Report().Queries["trace_dependencies"]
	want := uint64(workers * observations)
	if got.Calls != want || got.Results != want || got.LatencyCount != want {
		t.Fatalf("concurrent metrics = %+v, want %d observations", got, want)
	}
}

func TestZeroRegistryValueInitializesOnFirstObservation(t *testing.T) {
	var registry Registry
	registry.ObserveQuery(QueryObservation{ToolName: "graph_status", Returned: 1})
	if got := registry.Report().Queries["graph_status"].Calls; got != 1 {
		t.Fatalf("zero registry calls = %d, want 1", got)
	}
}

func BenchmarkObserveQuery(b *testing.B) {
	registry := NewRegistry()
	observation := QueryObservation{
		ToolName:   "find_symbol",
		Elapsed:    2 * time.Microsecond,
		Returned:   5,
		Truncated:  true,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		registry.ObserveQuery(observation)
	}
}

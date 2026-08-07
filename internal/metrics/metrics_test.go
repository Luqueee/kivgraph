package metrics

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
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
		ToolName:  "find_symbol",
		Elapsed:   2 * time.Microsecond,
		Returned:  5,
		Truncated: true,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		registry.ObserveQuery(observation)
	}
}

func TestOpenTelemetryExportsRegistryObservations(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			t.Errorf("shutdown meter provider: %v", err)
		}
	})

	registry, err := NewRegistryWithOpenTelemetry(OpenTelemetryOptions{MeterProvider: provider})
	if err != nil {
		t.Fatalf("NewRegistryWithOpenTelemetry() error = %v", err)
	}

	snapshotID := uint64(42)
	snapshotAgeMS := int64(9)
	registry.ObserveSnapshot(SnapshotObservation{
		ID:            snapshotID,
		CreatedAt:     time.Unix(1_700_000_000, 0),
		BuildDuration: 12 * time.Millisecond,
		Bytes:         1234,
	})
	registry.ObserveQuery(QueryObservation{
		ToolName:          "find_symbol",
		Elapsed:           4 * time.Millisecond,
		Returned:          3,
		UnresolvedRelated: 2,
		SnapshotID:        &snapshotID,
		SnapshotAgeMS:     &snapshotAgeMS,
	})
	registry.ObserveIndex(IndexObservation{
		Duration:   5 * time.Millisecond,
		Files:      6,
		Symbols:    7,
		Edges:      8,
		Unresolved: 9,
	})
	registry.ObserveWorker(WorkerObservation{Restarts: 3, MemoryBytes: 2048})
	registry.RecordWorkerRestart()
	registry.ObserveLadybug(LadybugObservation{
		TransactionDuration: 7 * time.Millisecond,
		DatabaseBytes:       4096,
	})

	var resourceMetrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &resourceMetrics); err != nil {
		t.Fatalf("collect OpenTelemetry metrics: %v", err)
	}
	observed := flattenOTelMetrics(resourceMetrics)

	if got := otelSumInt64(t, observed, QueryTotal).Value; got != 1 {
		t.Fatalf("%s = %d, want 1", QueryTotal, got)
	}
	queryTotal := otelSumInt64(t, observed, QueryTotal)
	toolName, ok := queryTotal.Attributes.Value(attribute.Key("tool.name"))
	if !ok || toolName.AsString() != "find_symbol" {
		t.Fatalf("%s tool.name = %v, want find_symbol", QueryTotal, toolName)
	}
	if got := otelSumInt64(t, observed, QueryResults).Value; got != 3 {
		t.Fatalf("%s = %d, want 3", QueryResults, got)
	}
	if got := otelSumInt64(t, observed, QueryUnresolvedRelated).Value; got != 2 {
		t.Fatalf("%s = %d, want 2", QueryUnresolvedRelated, got)
	}
	queryDuration := otelHistogramFloat64(t, observed, QueryDuration)
	if queryDuration.Count != 1 || queryDuration.Sum != 4 {
		t.Fatalf("%s = count %d sum %v, want count 1 sum 4", QueryDuration, queryDuration.Count, queryDuration.Sum)
	}
	if got := otelGaugeInt64(t, observed, SnapshotID).Value; got != 42 {
		t.Fatalf("%s = %d, want 42", SnapshotID, got)
	}
	if got := otelGaugeInt64(t, observed, SnapshotAge).Value; got != 9 {
		t.Fatalf("%s = %d, want 9", SnapshotAge, got)
	}
	if got := otelGaugeFloat64(t, observed, SnapshotBuildDuration).Value; got != 12 {
		t.Fatalf("%s = %v, want 12", SnapshotBuildDuration, got)
	}
	if got := otelGaugeInt64(t, observed, SnapshotBytes).Value; got != 1234 {
		t.Fatalf("%s = %d, want 1234", SnapshotBytes, got)
	}
	if got := otelGaugeFloat64(t, observed, IndexDuration).Value; got != 5 {
		t.Fatalf("%s = %v, want 5", IndexDuration, got)
	}
	if got := otelGaugeInt64(t, observed, IndexFiles).Value; got != 6 {
		t.Fatalf("%s = %d, want 6", IndexFiles, got)
	}
	if got := otelGaugeInt64(t, observed, IndexSymbols).Value; got != 7 {
		t.Fatalf("%s = %d, want 7", IndexSymbols, got)
	}
	if got := otelGaugeInt64(t, observed, IndexEdges).Value; got != 8 {
		t.Fatalf("%s = %d, want 8", IndexEdges, got)
	}
	if got := otelGaugeInt64(t, observed, IndexUnresolved).Value; got != 9 {
		t.Fatalf("%s = %d, want 9", IndexUnresolved, got)
	}
	if got := otelGaugeInt64(t, observed, TSWorkerRestarts).Value; got != 4 {
		t.Fatalf("%s = %d, want 4", TSWorkerRestarts, got)
	}
	if got := otelGaugeInt64(t, observed, TSWorkerMemory).Value; got != 2048 {
		t.Fatalf("%s = %d, want 2048", TSWorkerMemory, got)
	}
	ladybugDuration := otelHistogramFloat64(t, observed, LadybugTransaction)
	if ladybugDuration.Count != 1 || ladybugDuration.Sum != 7 {
		t.Fatalf("%s = count %d sum %v, want count 1 sum 7", LadybugTransaction, ladybugDuration.Count, ladybugDuration.Sum)
	}
	if got := otelGaugeInt64(t, observed, LadybugDatabaseBytes).Value; got != 4096 {
		t.Fatalf("%s = %d, want 4096", LadybugDatabaseBytes, got)
	}
}

func TestOpenTelemetryDefaultsToNoopProvider(t *testing.T) {
	registry, err := NewRegistryWithOpenTelemetry(OpenTelemetryOptions{})
	if err != nil {
		t.Fatalf("NewRegistryWithOpenTelemetry() error = %v", err)
	}
	registry.ObserveQuery(QueryObservation{ToolName: "unknown-tool", Returned: 1})
	if got := registry.Report().Queries["unknown-tool"].Calls; got != 1 {
		t.Fatalf("registry calls = %d, want 1", got)
	}
}

func TestOpenTelemetryRejectsNilMeter(t *testing.T) {
	_, err := NewOpenTelemetry(OpenTelemetryOptions{MeterProvider: nilMeterProvider{}})
	if err == nil {
		t.Fatal("NewOpenTelemetry() error = nil, want nil meter error")
	}
}

type nilMeterProvider struct {
	metric.MeterProvider
}

func (nilMeterProvider) Meter(string, ...metric.MeterOption) metric.Meter {
	return nil
}

func flattenOTelMetrics(resourceMetrics metricdata.ResourceMetrics) map[string]metricdata.Metrics {
	flattened := make(map[string]metricdata.Metrics)
	for _, scope := range resourceMetrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			flattened[metric.Name] = metric
		}
	}
	return flattened
}

func otelMetric(t *testing.T, observed map[string]metricdata.Metrics, name string) metricdata.Metrics {
	t.Helper()
	observedMetric, ok := observed[name]
	if !ok {
		t.Fatalf("OpenTelemetry metric %q not found; got %v", name, observed)
	}
	return observedMetric
}

func otelSumInt64(t *testing.T, observed map[string]metricdata.Metrics, name string) metricdata.DataPoint[int64] {
	t.Helper()
	observedMetric := otelMetric(t, observed, name)
	sum, ok := observedMetric.Data.(metricdata.Sum[int64])
	if !ok || len(sum.DataPoints) != 1 {
		t.Fatalf("OpenTelemetry metric %q data = %#v, want one int64 sum point", name, observedMetric.Data)
	}
	return sum.DataPoints[0]
}

func otelGaugeInt64(t *testing.T, observed map[string]metricdata.Metrics, name string) metricdata.DataPoint[int64] {
	t.Helper()
	observedMetric := otelMetric(t, observed, name)
	gauge, ok := observedMetric.Data.(metricdata.Gauge[int64])
	if !ok || len(gauge.DataPoints) != 1 {
		t.Fatalf("OpenTelemetry metric %q data = %#v, want one int64 gauge point", name, observedMetric.Data)
	}
	return gauge.DataPoints[0]
}

func otelGaugeFloat64(t *testing.T, observed map[string]metricdata.Metrics, name string) metricdata.DataPoint[float64] {
	t.Helper()
	observedMetric := otelMetric(t, observed, name)
	gauge, ok := observedMetric.Data.(metricdata.Gauge[float64])
	if !ok || len(gauge.DataPoints) != 1 {
		t.Fatalf("OpenTelemetry metric %q data = %#v, want one float64 gauge point", name, observedMetric.Data)
	}
	return gauge.DataPoints[0]
}

func otelHistogramFloat64(t *testing.T, observed map[string]metricdata.Metrics, name string) metricdata.HistogramDataPoint[float64] {
	t.Helper()
	observedMetric := otelMetric(t, observed, name)
	histogram, ok := observedMetric.Data.(metricdata.Histogram[float64])
	if !ok || len(histogram.DataPoints) != 1 {
		t.Fatalf("OpenTelemetry metric %q data = %#v, want one float64 histogram point", name, observedMetric.Data)
	}
	return histogram.DataPoints[0]
}

func BenchmarkObserveQueryWithNoopOpenTelemetry(b *testing.B) {
	registry, err := NewRegistryWithOpenTelemetry(OpenTelemetryOptions{})
	if err != nil {
		b.Fatalf("NewRegistryWithOpenTelemetry() error = %v", err)
	}
	observation := QueryObservation{
		ToolName: "find_symbol",
		Elapsed:  2 * time.Microsecond,
		Returned: 5,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		registry.ObserveQuery(observation)
	}
}

func BenchmarkObserveQueryWithSDKOpenTelemetry(b *testing.B) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	b.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			b.Errorf("shutdown meter provider: %v", err)
		}
	})
	registry, err := NewRegistryWithOpenTelemetry(OpenTelemetryOptions{MeterProvider: provider})
	if err != nil {
		b.Fatalf("NewRegistryWithOpenTelemetry() error = %v", err)
	}
	observation := QueryObservation{
		ToolName: "find_symbol",
		Elapsed:  2 * time.Microsecond,
		Returned: 5,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		registry.ObserveQuery(observation)
	}
}

func BenchmarkObserveAll(b *testing.B) {
	benchmarkObserveAll(b, NewRegistry())
}

func BenchmarkObserveAllWithNoopOpenTelemetry(b *testing.B) {
	registry, err := NewRegistryWithOpenTelemetry(OpenTelemetryOptions{})
	if err != nil {
		b.Fatalf("NewRegistryWithOpenTelemetry() error = %v", err)
	}
	benchmarkObserveAll(b, registry)
}

func BenchmarkObserveAllWithSDKOpenTelemetry(b *testing.B) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	b.Cleanup(func() {
		if err := provider.Shutdown(context.Background()); err != nil {
			b.Errorf("shutdown meter provider: %v", err)
		}
	})
	registry, err := NewRegistryWithOpenTelemetry(OpenTelemetryOptions{MeterProvider: provider})
	if err != nil {
		b.Fatalf("NewRegistryWithOpenTelemetry() error = %v", err)
	}
	benchmarkObserveAll(b, registry)
}

func benchmarkObserveAll(b *testing.B, registry *Registry) {
	b.Helper()
	snapshotID := uint64(7)
	snapshotAgeMS := int64(3)
	query := QueryObservation{
		ToolName:          "find_symbol",
		Elapsed:           2 * time.Microsecond,
		Returned:          5,
		UnresolvedRelated: 1,
		SnapshotID:        &snapshotID,
		SnapshotAgeMS:     &snapshotAgeMS,
	}
	snapshot := SnapshotObservation{
		ID:            snapshotID,
		CreatedAt:     time.Unix(1_700_000_000, 0),
		BuildDuration: 4 * time.Millisecond,
		Bytes:         4096,
	}
	index := IndexObservation{
		Duration:   5 * time.Millisecond,
		Files:      6,
		Symbols:    7,
		Edges:      8,
		Unresolved: 1,
	}
	worker := WorkerObservation{Restarts: 2, MemoryBytes: 1024}
	ladybug := LadybugObservation{
		TransactionDuration: 3 * time.Millisecond,
		DatabaseBytes:       8192,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		registry.ObserveQuery(query)
		registry.ObserveSnapshot(snapshot)
		registry.ObserveIndex(index)
		registry.ObserveWorker(worker)
		registry.RecordWorkerRestart()
		registry.ObserveLadybug(ladybug)
	}
}

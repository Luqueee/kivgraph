package metrics

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

const defaultMeterName = "github.com/Luqueee/ladygraph/internal/metrics"

// OpenTelemetryOptions configures the optional OpenTelemetry metrics bridge.
//
// MeterProvider is owned by the caller. Ladygraph never creates an exporter or
// a collector. A nil provider selects the OpenTelemetry no-op provider.
type OpenTelemetryOptions struct {
	MeterProvider metric.MeterProvider
	MeterName     string
}

// OpenTelemetry forwards registry observations to OpenTelemetry instruments.
// It contains no exporter or background goroutine; the caller controls the
// lifetime of the supplied MeterProvider.
type OpenTelemetry struct {
	queryDuration         metric.Float64Histogram
	queryTotal            metric.Int64Counter
	queryErrors           metric.Int64Counter
	queryResults          metric.Int64Counter
	queryTruncated        metric.Int64Counter
	queryUnresolved       metric.Int64Counter
	snapshotID            metric.Int64Gauge
	snapshotAge           metric.Int64Gauge
	snapshotBuildDuration metric.Float64Gauge
	snapshotBytes         metric.Int64Gauge
	indexDuration         metric.Float64Gauge
	indexFiles            metric.Int64Gauge
	indexSymbols          metric.Int64Gauge
	indexEdges            metric.Int64Gauge
	indexUnresolved       metric.Int64Gauge
	ladybugTransaction    metric.Float64Histogram
	ladybugDatabaseBytes  metric.Int64Gauge
	tsWorkerRestarts      metric.Int64Gauge
	tsWorkerMemory        metric.Int64Gauge
	toolOptions           map[string]toolOption
}

type toolOption struct {
	add    []metric.AddOption
	record []metric.RecordOption
}

// NewRegistryWithOpenTelemetry creates a registry with an optional
// OpenTelemetry metrics bridge. The regular NewRegistry constructor remains
// exporter-free and does not incur bridge instrumentation overhead.
func NewRegistryWithOpenTelemetry(options OpenTelemetryOptions) (*Registry, error) {
	telemetry, err := NewOpenTelemetry(options)
	if err != nil {
		return nil, fmt.Errorf("create OpenTelemetry metrics bridge: %w", err)
	}
	return &Registry{
		queries: make(map[string]*queryCounter),
		otel:    telemetry,
	}, nil
}

// NewOpenTelemetry creates the instruments used by the metrics bridge. A nil
// MeterProvider deliberately uses a no-op implementation, so exporters remain
// disabled unless the caller supplies a provider configured for export.
func NewOpenTelemetry(options OpenTelemetryOptions) (*OpenTelemetry, error) {
	provider := options.MeterProvider
	if provider == nil {
		provider = noop.NewMeterProvider()
	}
	meterName := options.MeterName
	if meterName == "" {
		meterName = defaultMeterName
	}
	meter := provider.Meter(meterName)
	if meter == nil {
		return nil, fmt.Errorf("meter provider returned a nil meter")
	}

	telemetry := &OpenTelemetry{toolOptions: newToolOptions()}
	var err error
	if telemetry.queryDuration, err = meter.Float64Histogram(
		QueryDuration,
		metric.WithDescription("Duration of completed MCP tool calls."),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", QueryDuration, err)
	}
	if telemetry.queryTotal, err = meter.Int64Counter(
		QueryTotal,
		metric.WithDescription("Number of completed MCP tool calls."),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", QueryTotal, err)
	}
	if telemetry.queryErrors, err = meter.Int64Counter(
		QueryErrors,
		metric.WithDescription("Number of MCP tool calls that returned an error."),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", QueryErrors, err)
	}
	if telemetry.queryResults, err = meter.Int64Counter(
		QueryResults,
		metric.WithDescription("Number of result items returned by MCP tool calls."),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", QueryResults, err)
	}
	if telemetry.queryTruncated, err = meter.Int64Counter(
		QueryTruncated,
		metric.WithDescription("Number of MCP tool calls whose results were truncated."),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", QueryTruncated, err)
	}
	if telemetry.queryUnresolved, err = meter.Int64Counter(
		QueryUnresolvedRelated,
		metric.WithDescription("Number of unresolved references related to MCP results."),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", QueryUnresolvedRelated, err)
	}
	if telemetry.snapshotID, err = meter.Int64Gauge(
		SnapshotID,
		metric.WithDescription("Identifier of the currently published snapshot."),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", SnapshotID, err)
	}
	if telemetry.snapshotAge, err = meter.Int64Gauge(
		SnapshotAge,
		metric.WithDescription("Age of the currently published snapshot."),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", SnapshotAge, err)
	}
	if telemetry.snapshotBuildDuration, err = meter.Float64Gauge(
		SnapshotBuildDuration,
		metric.WithDescription("Duration of the latest snapshot build."),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", SnapshotBuildDuration, err)
	}
	if telemetry.snapshotBytes, err = meter.Int64Gauge(
		SnapshotBytes,
		metric.WithDescription("Size of the currently published snapshot."),
		metric.WithUnit("By"),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", SnapshotBytes, err)
	}
	if telemetry.indexDuration, err = meter.Float64Gauge(
		IndexDuration,
		metric.WithDescription("Duration of the latest indexing operation."),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", IndexDuration, err)
	}
	if telemetry.indexFiles, err = meter.Int64Gauge(
		IndexFiles,
		metric.WithDescription("Files processed by the latest indexing operation."),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", IndexFiles, err)
	}
	if telemetry.indexSymbols, err = meter.Int64Gauge(
		IndexSymbols,
		metric.WithDescription("Symbols produced by the latest indexing operation."),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", IndexSymbols, err)
	}
	if telemetry.indexEdges, err = meter.Int64Gauge(
		IndexEdges,
		metric.WithDescription("Edges produced by the latest indexing operation."),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", IndexEdges, err)
	}
	if telemetry.indexUnresolved, err = meter.Int64Gauge(
		IndexUnresolved,
		metric.WithDescription("Unresolved references from the latest indexing operation."),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", IndexUnresolved, err)
	}
	if telemetry.ladybugTransaction, err = meter.Float64Histogram(
		LadybugTransaction,
		metric.WithDescription("Duration of LadybugDB transactions."),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", LadybugTransaction, err)
	}
	if telemetry.ladybugDatabaseBytes, err = meter.Int64Gauge(
		LadybugDatabaseBytes,
		metric.WithDescription("Latest observed LadybugDB database size."),
		metric.WithUnit("By"),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", LadybugDatabaseBytes, err)
	}
	if telemetry.tsWorkerRestarts, err = meter.Int64Gauge(
		TSWorkerRestarts,
		metric.WithDescription("Number of TypeScript worker restarts."),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", TSWorkerRestarts, err)
	}
	if telemetry.tsWorkerMemory, err = meter.Int64Gauge(
		TSWorkerMemory,
		metric.WithDescription("Latest observed TypeScript worker memory usage."),
		metric.WithUnit("By"),
	); err != nil {
		return nil, fmt.Errorf("create %s instrument: %w", TSWorkerMemory, err)
	}
	return telemetry, nil
}

func newToolOptions() map[string]toolOption {
	const fallback = "other"
	names := [...]string{
		"find_symbol",
		"get_symbol",
		"find_references",
		"find_cross_repo_consumers",
		"get_blast_radius",
		"list_repositories",
		"graph_status",
		"get_unresolved_references",
		"trace_dependencies",
		fallback,
	}
	options := make(map[string]toolOption, len(names))
	for _, name := range names {
		measurementOption := metric.WithAttributeSet(attribute.NewSet(attribute.String("tool.name", name)))
		options[name] = toolOption{
			add:    []metric.AddOption{measurementOption},
			record: []metric.RecordOption{measurementOption},
		}
	}
	return options
}

func (telemetry *OpenTelemetry) queryOption(toolName string) toolOption {
	if option, ok := telemetry.toolOptions[toolName]; ok {
		return option
	}
	return telemetry.toolOptions["other"]
}

func (telemetry *OpenTelemetry) observeQuery(observation QueryObservation) {
	if telemetry == nil {
		return
	}
	ctx := context.Background()
	option := telemetry.queryOption(observation.ToolName)
	telemetry.queryTotal.Add(ctx, 1, option.add...)
	if observation.Err != nil {
		telemetry.queryErrors.Add(ctx, 1, option.add...)
	}
	if observation.Returned > 0 {
		telemetry.queryResults.Add(ctx, int64(observation.Returned), option.add...)
	}
	if observation.Truncated {
		telemetry.queryTruncated.Add(ctx, 1, option.add...)
	}
	if observation.UnresolvedRelated > 0 {
		telemetry.queryUnresolved.Add(ctx, int64(observation.UnresolvedRelated), option.add...)
	}
	telemetry.queryDuration.Record(ctx, durationMilliseconds(observation.Elapsed), option.record...)
	if observation.SnapshotID != nil {
		telemetry.snapshotID.Record(ctx, nonNegativeUint64(*observation.SnapshotID))
	}
	if observation.SnapshotAgeMS != nil {
		telemetry.snapshotAge.Record(ctx, nonNegativeMillisecondsValue(*observation.SnapshotAgeMS))
	}
}

func (telemetry *OpenTelemetry) observeSnapshot(observation SnapshotObservation) {
	if telemetry == nil {
		return
	}
	ctx := context.Background()
	telemetry.snapshotID.Record(ctx, nonNegativeUint64(observation.ID))
	if !observation.CreatedAt.IsZero() {
		telemetry.snapshotAge.Record(ctx, 0)
	}
	telemetry.snapshotBuildDuration.Record(ctx, durationMilliseconds(observation.BuildDuration))
	telemetry.snapshotBytes.Record(ctx, nonNegativeInt64(observation.Bytes))
}

func (telemetry *OpenTelemetry) observeIndex(observation IndexObservation) {
	if telemetry == nil {
		return
	}
	ctx := context.Background()
	telemetry.indexDuration.Record(ctx, durationMilliseconds(observation.Duration))
	telemetry.indexFiles.Record(ctx, nonNegativeUint64(observation.Files))
	telemetry.indexSymbols.Record(ctx, nonNegativeUint64(observation.Symbols))
	telemetry.indexEdges.Record(ctx, nonNegativeUint64(observation.Edges))
	telemetry.indexUnresolved.Record(ctx, nonNegativeUint64(observation.Unresolved))
}

func (telemetry *OpenTelemetry) observeWorker(observation WorkerObservation) {
	if telemetry == nil {
		return
	}
	ctx := context.Background()
	telemetry.tsWorkerRestarts.Record(ctx, nonNegativeUint64(observation.Restarts))
	telemetry.tsWorkerMemory.Record(ctx, nonNegativeInt64(observation.MemoryBytes))
}

func (telemetry *OpenTelemetry) observeLadybug(observation LadybugObservation) {
	if telemetry == nil {
		return
	}
	ctx := context.Background()
	telemetry.ladybugTransaction.Record(ctx, durationMilliseconds(observation.TransactionDuration))
	telemetry.ladybugDatabaseBytes.Record(ctx, nonNegativeInt64(observation.DatabaseBytes))
}

func durationMilliseconds(value time.Duration) float64 {
	return float64(nonNegativeDuration(value)) / float64(time.Millisecond)
}

func nonNegativeUint64(value uint64) int64 {
	const maximum = uint64(1<<63 - 1)
	if value > maximum {
		return int64(maximum)
	}
	return int64(value)
}

func nonNegativeMillisecondsValue(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

//go:build ladybug && cgo && linux

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"
)

const (
	defaultDatabase = "/tmp/luque-ladybug-qualification.db"
	defaultOutput   = "benchmarks/ladybug-delta-profile"
	sampleCount     = 5
)

type config struct {
	DatabasePath string
	OutputDir    string
	Samples      int
}

type results struct {
	Benchmark   string         `json:"benchmark"`
	Command     string         `json:"command"`
	Commit      string         `json:"commit"`
	GeneratedAt time.Time      `json:"generated_at"`
	GoVersion   string         `json:"go_version"`
	GOOS        string         `json:"goos"`
	GOARCH      string         `json:"goarch"`
	BaseBytes   int64          `json:"base_database_bytes"`
	Samples     int            `json:"samples"`
	Cases       []profileCase  `json:"cases"`
	Gate        gateAssessment `json:"gate"`
	Limitations []string       `json:"limitations"`
}

type profileCase struct {
	Strategy         string      `json:"strategy"`
	Relations        int         `json:"relations"`
	AggregatedDeltas int         `json:"aggregated_deltas"`
	Skipped          bool        `json:"skipped,omitempty"`
	Reason           string      `json:"reason,omitempty"`
	Samples          []sample    `json:"samples,omitempty"`
	Summary          caseSummary `json:"summary,omitempty"`
}

type sample struct {
	Phases           phases `json:"phases_ms"`
	RelationsApplied int    `json:"relations_applied"`
	AllocationsBytes uint64 `json:"allocations_bytes"`
	RSSBytes         uint64 `json:"rss_bytes"`
}

type phases struct {
	Stage     float64 `json:"stage"`
	Begin     float64 `json:"begin"`
	Lookups   float64 `json:"lookups"`
	Deletes   float64 `json:"deletes"`
	Creates   float64 `json:"creates"`
	Integrity float64 `json:"integrity"`
	Commit    float64 `json:"commit"`
	Close     float64 `json:"close"`
	Total     float64 `json:"total"`
}

type caseSummary struct {
	P50MS               float64 `json:"p50_ms"`
	P95MS               float64 `json:"p95_ms"`
	ThroughputPerSecond float64 `json:"throughput_per_second"`
	PeakRSSBytes        uint64  `json:"peak_rss_bytes"`
	AllocationsPerBatch uint64  `json:"allocations_per_batch"`
	PhasesP50           phases  `json:"phases_p50_ms"`
}

type gateAssessment struct {
	SmallDeltaP95MS       float64 `json:"small_delta_p95_ms"`
	SmallDeltaWithin150MS bool    `json:"small_delta_within_150_ms"`
	LargeDeltaP95MS       float64 `json:"large_delta_p95_ms"`
	LargeDeltaWithin500MS bool    `json:"large_delta_within_500_ms"`
	IntegrityPassed       bool    `json:"integrity_passed"`
	ChosenStrategy        string  `json:"chosen_strategy"`
	Passed                bool    `json:"passed"`
}

type strategy string

const (
	strategyIndividual strategy = "prepared_individual"
	strategyBatch      strategy = "prepared_batch"
	strategyCopy       strategy = "staging_copy"
	strategyAggregate  strategy = "aggregate_10_deltas"
)

type referenceRow struct {
	sourceKey string
	targetKey string
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.DatabasePath, "database", defaultDatabase, "qualified LadybugDB database to copy")
	flag.StringVar(&cfg.OutputDir, "output", defaultOutput, "results output directory")
	flag.IntVar(&cfg.Samples, "samples", sampleCount, "independent samples per measured case")
	flag.Parse()
	result, err := run(context.Background(), cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := writeOutputs(cfg.OutputDir, result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("small delta p95: %.1f ms; large delta p95: %.1f ms; gate: %t\n", result.Gate.SmallDeltaP95MS, result.Gate.LargeDeltaP95MS, result.Gate.Passed)
	if !result.Gate.Passed {
		fmt.Fprintln(os.Stderr, "LADYBUG_DELTA_PERFORMANCE_PASS not emitted; see results.json and report.md")
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) (results, error) {
	if cfg.Samples < 1 {
		return results{}, errors.New("samples must be positive")
	}
	info, err := os.Stat(cfg.DatabasePath)
	if err != nil {
		return results{}, fmt.Errorf("stat source database: %w", err)
	}
	if !info.Mode().IsRegular() {
		return results{}, errors.New("source database must be a regular file")
	}
	result := results{
		Benchmark: "ladybug-delta-profile", Command: strings.Join(os.Args, " "), Commit: gitState(), GeneratedAt: time.Now().UTC(),
		GoVersion: runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH, BaseBytes: info.Size(), Samples: cfg.Samples,
		Limitations: []string{
			"Each sample copies the qualified Linux amd64 database and measures one transaction; results do not generalize to other LadybugDB builds or filesystems.",
			"The individual strategy is measured only for 1–10 relationships because 1,000 per-row native calls violate the production batching policy by design.",
			"RSS is the process resident set observed after each sample; it is not a cgroup limit or a storage-controller measurement.",
			"The aggregate result excludes its 150–500 ms queue wait, which must be added before claiming end-to-end latency.",
		},
	}
	for _, entry := range []struct {
		strategy  strategy
		relations int
	}{
		{strategyIndividual, 1}, {strategyIndividual, 10}, {strategyIndividual, 1_000},
		{strategyBatch, 1}, {strategyBatch, 10}, {strategyBatch, 1_000},
		{strategyCopy, 1}, {strategyCopy, 10}, {strategyCopy, 1_000},
		{strategyAggregate, 1}, {strategyAggregate, 10}, {strategyAggregate, 1_000},
	} {
		profile := profileCase{Strategy: string(entry.strategy), Relations: entry.relations, AggregatedDeltas: 1}
		if entry.strategy == strategyAggregate {
			profile.AggregatedDeltas = 10
		}
		if entry.strategy == strategyIndividual && entry.relations > 10 {
			profile.Skipped = true
			profile.Reason = "would issue one native mutation query per relationship; excluded by the production batching policy"
			result.Cases = append(result.Cases, profile)
			continue
		}
		for sampleIndex := 0; sampleIndex < cfg.Samples; sampleIndex++ {
			measurement, measureErr := measure(ctx, cfg.DatabasePath, entry.strategy, entry.relations, profile.AggregatedDeltas, sampleIndex)
			if measureErr != nil {
				return results{}, fmt.Errorf("measure %s/%d sample %d: %w", entry.strategy, entry.relations, sampleIndex+1, measureErr)
			}
			profile.Samples = append(profile.Samples, measurement)
		}
		profile.Summary = summarize(profile.Samples)
		result.Cases = append(result.Cases, profile)
	}
	result.Gate = assessGate(result.Cases)
	return result, nil
}

func measure(_ context.Context, sourcePath string, selected strategy, relations, aggregatedDeltas, sampleIndex int) (sample, error) {
	workDir, err := os.MkdirTemp("", "luque-ladybug-delta-profile-")
	if err != nil {
		return sample{}, err
	}
	defer os.RemoveAll(workDir)
	databasePath := filepath.Join(workDir, "graph.db")
	if err := copyFile(sourcePath, databasePath); err != nil {
		return sample{}, err
	}

	sourceKey := fmt.Sprintf("delta-profile-source-%s-%d", selected, sampleIndex)
	rows := referenceRows(relations * aggregatedDeltas)
	for index := range rows {
		rows[index].sourceKey = sourceKey
	}
	seedPath := filepath.Join(workDir, "seed.csv")
	if err := writeCSV(seedPath, rows); err != nil {
		return sample{}, err
	}
	csvPath := filepath.Join(workDir, "references.csv")
	stageStart := time.Now()
	if selected == strategyCopy || selected == strategyAggregate {
		if err := writeCSV(csvPath, rows); err != nil {
			return sample{}, err
		}
	}
	stageDuration := time.Since(stageStart)

	database, err := lbug.OpenDatabase(databasePath, lbug.DefaultSystemConfig())
	if err != nil {
		return sample{}, err
	}
	connection, err := lbug.OpenConnection(database)
	if err != nil {
		database.Close()
		return sample{}, err
	}
	closed := false
	defer func() {
		if !closed {
			connection.Close()
			database.Close()
		}
	}()
	if err := createSource(connection, sourceKey); err != nil {
		return sample{}, err
	}
	if err := seedOutgoing(connection, seedPath); err != nil {
		return sample{}, err
	}
	individual, batch, err := prepareCreateStatements(connection)
	if err != nil {
		return sample{}, err
	}
	defer individual.Close()
	defer batch.Close()

	var before runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	if err := executeQuery(connection, "BEGIN TRANSACTION"); err != nil {
		return sample{}, err
	}
	beginDuration := time.Since(start)

	lookupStart := time.Now()
	keys := append([]string{sourceKey}, targetKeys(rows)...)
	if count, err := queryCount(connection, fmt.Sprintf("UNWIND %s AS key MATCH (symbol:Symbol) WHERE symbol.stable_key = key RETURN count(*)", cypherList(keys))); err != nil || count != int64(len(rows)+1) {
		return sample{}, fmt.Errorf("lookup endpoints count=%d error=%v", count, err)
	}
	lookupDuration := time.Since(lookupStart)

	deleteStart := time.Now()
	if err := executeQuery(connection, fmt.Sprintf("MATCH (source:Symbol)-[edge:REFERENCES]->() WHERE source.stable_key = %s DELETE edge RETURN count(*)", cypherString(sourceKey))); err != nil {
		return sample{}, err
	}
	deleteDuration := time.Since(deleteStart)

	createStart := time.Now()
	switch selected {
	case strategyIndividual:
		for _, row := range rows {
			if count, err := executeCount(connection, individual, row.arguments()); err != nil || count != 1 {
				return sample{}, fmt.Errorf("individual create count=%d error=%v", count, err)
			}
		}
	case strategyBatch:
		if count, err := executeCount(connection, batch, map[string]any{"rows": rowArguments(rows)}); err != nil || count != int64(len(rows)) {
			return sample{}, fmt.Errorf("batch create count=%d error=%v", count, err)
		}
	case strategyCopy, strategyAggregate:
		if err := executeQuery(connection, fmt.Sprintf("COPY REFERENCES FROM %s", cypherString(csvPath))); err != nil {
			return sample{}, err
		}
	default:
		return sample{}, fmt.Errorf("unknown strategy %q", selected)
	}
	createDuration := time.Since(createStart)

	integrityStart := time.Now()
	count, err := queryCount(connection, fmt.Sprintf("MATCH (source:Symbol)-[edge:REFERENCES]->() WHERE source.stable_key = %s RETURN count(*)", cypherString(sourceKey)))
	if err != nil || count != int64(len(rows)) {
		return sample{}, fmt.Errorf("integrity count=%d want %d error=%v", count, len(rows), err)
	}
	integrityDuration := time.Since(integrityStart)
	commitStart := time.Now()
	if err := executeQuery(connection, "COMMIT"); err != nil {
		return sample{}, err
	}
	commitDuration := time.Since(commitStart)
	individual.Close()
	batch.Close()
	closeStart := time.Now()
	connection.Close()
	database.Close()
	closed = true
	closeDuration := time.Since(closeStart)
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	return sample{
		Phases:           phases{Stage: milliseconds(stageDuration), Begin: milliseconds(beginDuration), Lookups: milliseconds(lookupDuration), Deletes: milliseconds(deleteDuration), Creates: milliseconds(createDuration), Integrity: milliseconds(integrityDuration), Commit: milliseconds(commitDuration), Close: milliseconds(closeDuration), Total: milliseconds(time.Since(start))},
		RelationsApplied: len(rows), AllocationsBytes: after.TotalAlloc - before.TotalAlloc, RSSBytes: residentBytes(),
	}, nil
}

func referenceRows(count int) []referenceRow {
	rows := make([]referenceRow, count)
	for index := range rows {
		rows[index].targetKey = fmt.Sprintf("symbol-%08d", index+1)
	}
	return rows
}

func (row referenceRow) arguments() map[string]any {
	return map[string]any{
		"source_key": row.sourceKey, "target_key": row.targetKey, "evidence_kind": "delta-profile",
		"source_file_key": "file-00000000", "target_file_key": "file-00000000",
	}
}

func rowArguments(rows []referenceRow) []any {
	arguments := make([]any, len(rows))
	for index, row := range rows {
		arguments[index] = row.arguments()
	}
	return arguments
}

func targetKeys(rows []referenceRow) []string {
	keys := make([]string, len(rows))
	for index, row := range rows {
		keys[index] = row.targetKey
	}
	return keys
}

func createSource(connection *lbug.Connection, sourceKey string) error {
	return executeQuery(connection, fmt.Sprintf("CREATE (:Symbol {stable_key: %s, repository_key: \"repository-0000\", file_key: \"file-00000000\", name: \"delta_profile\", qualified_name: \"delta.profile\", kind: \"function\", signature: \"delta_profile()\", start_line: 1, end_line: 1}) RETURN count(*)", cypherString(sourceKey)))
}

func seedOutgoing(connection *lbug.Connection, csvPath string) error {
	if err := executeQuery(connection, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	if err := executeQuery(connection, fmt.Sprintf("COPY REFERENCES FROM %s", cypherString(csvPath))); err != nil {
		_ = executeQuery(connection, "ROLLBACK")
		return err
	}
	return executeQuery(connection, "COMMIT")
}

const individualCreateQuery = `MATCH (source:Symbol), (target:Symbol)
WHERE source.stable_key = $source_key AND target.stable_key = $target_key
CREATE (source)-[:REFERENCES {evidence_kind: $evidence_kind, source_file_key: $source_file_key, target_file_key: $target_file_key}]->(target)
RETURN count(*)`

const batchCreateQuery = `UNWIND $rows AS row
MATCH (source:Symbol), (target:Symbol)
WHERE source.stable_key = row.source_key AND target.stable_key = row.target_key
CREATE (source)-[:REFERENCES {evidence_kind: row.evidence_kind, source_file_key: row.source_file_key, target_file_key: row.target_file_key}]->(target)
RETURN count(*)`

func prepareCreateStatements(connection *lbug.Connection) (*lbug.PreparedStatement, *lbug.PreparedStatement, error) {
	individual, err := connection.Prepare(individualCreateQuery)
	if err != nil {
		return nil, nil, err
	}
	batch, err := connection.Prepare(batchCreateQuery)
	if err != nil {
		individual.Close()
		return nil, nil, err
	}
	return individual, batch, nil
}

func executeCount(connection *lbug.Connection, statement *lbug.PreparedStatement, arguments map[string]any) (int64, error) {
	result, err := connection.Execute(statement, arguments)
	if err != nil {
		if result != nil {
			result.Close()
		}
		return 0, err
	}
	defer result.Close()
	if !result.HasNext() {
		return 0, errors.New("mutation returned no count")
	}
	tuple, err := result.Next()
	if err != nil {
		return 0, err
	}
	defer tuple.Close()
	value, err := tuple.GetValue(0)
	if err != nil {
		return 0, err
	}
	count, ok := value.(int64)
	if !ok {
		return 0, fmt.Errorf("mutation count has type %T", value)
	}
	return count, nil
}

func queryCount(connection *lbug.Connection, query string) (int64, error) {
	result, err := connection.Query(query)
	if err != nil {
		if result != nil {
			result.Close()
		}
		return 0, err
	}
	defer result.Close()
	if !result.HasNext() {
		return 0, errors.New("query returned no count")
	}
	tuple, err := result.Next()
	if err != nil {
		return 0, err
	}
	defer tuple.Close()
	value, err := tuple.GetValue(0)
	if err != nil {
		return 0, err
	}
	count, ok := value.(int64)
	if !ok {
		return 0, fmt.Errorf("query count has type %T", value)
	}
	return count, nil
}

func executeQuery(connection *lbug.Connection, query string) error {
	result, err := connection.Query(query)
	if result != nil {
		result.Close()
	}
	return err
}

func writeCSV(path string, rows []referenceRow) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	writer := bufio.NewWriterSize(file, 256*1024)
	for _, row := range rows {
		if _, err := fmt.Fprintf(writer, "%s,%s,delta-profile,file-00000000,file-00000000\n", row.sourceKey, row.targetKey); err != nil {
			_ = file.Close()
			return err
		}
	}
	return errors.Join(writer.Flush(), file.Close())
}

func summarize(samples []sample) caseSummary {
	totals := make([]float64, len(samples))
	throughputs := make([]float64, len(samples))
	allocations := make([]uint64, len(samples))
	var peakRSS uint64
	for index, value := range samples {
		totals[index] = value.Phases.Total
		throughputs[index] = float64(value.RelationsApplied) / (value.Phases.Total / 1_000)
		allocations[index] = value.AllocationsBytes
		if value.RSSBytes > peakRSS {
			peakRSS = value.RSSBytes
		}
	}
	return caseSummary{P50MS: median(totals), P95MS: percentile(totals, 0.95), ThroughputPerSecond: median(throughputs), PeakRSSBytes: peakRSS, AllocationsPerBatch: medianUint64(allocations), PhasesP50: phases{Stage: medianPhase(samples, func(value phases) float64 { return value.Stage }), Begin: medianPhase(samples, func(value phases) float64 { return value.Begin }), Lookups: medianPhase(samples, func(value phases) float64 { return value.Lookups }), Deletes: medianPhase(samples, func(value phases) float64 { return value.Deletes }), Creates: medianPhase(samples, func(value phases) float64 { return value.Creates }), Integrity: medianPhase(samples, func(value phases) float64 { return value.Integrity }), Commit: medianPhase(samples, func(value phases) float64 { return value.Commit }), Close: medianPhase(samples, func(value phases) float64 { return value.Close }), Total: medianPhase(samples, func(value phases) float64 { return value.Total })}}
}

func assessGate(cases []profileCase) gateAssessment {
	assessment := gateAssessment{ChosenStrategy: "prepared_individual (≤10); prepared_batch (>10)"}
	for _, entry := range cases {
		if entry.Skipped {
			continue
		}
		if entry.Strategy == string(strategyIndividual) && entry.Relations == 10 {
			assessment.SmallDeltaP95MS = entry.Summary.P95MS
		}
		if entry.Strategy == string(strategyBatch) && entry.Relations == 1_000 {
			assessment.LargeDeltaP95MS = entry.Summary.P95MS
		}
	}
	assessment.SmallDeltaWithin150MS = assessment.SmallDeltaP95MS > 0 && assessment.SmallDeltaP95MS < 150
	assessment.LargeDeltaWithin500MS = assessment.LargeDeltaP95MS > 0 && assessment.LargeDeltaP95MS < 500
	assessment.IntegrityPassed = assessment.SmallDeltaP95MS > 0 && assessment.LargeDeltaP95MS > 0
	assessment.Passed = assessment.SmallDeltaWithin150MS && assessment.LargeDeltaWithin500MS && assessment.IntegrityPassed
	return assessment
}

func medianPhase(samples []sample, selectPhase func(phases) float64) float64 {
	values := make([]float64, len(samples))
	for index, value := range samples {
		values[index] = selectPhase(value.Phases)
	}
	return median(values)
}

func median(values []float64) float64 { return percentile(values, 0.50) }

func percentile(values []float64, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copied := append([]float64(nil), values...)
	sort.Float64s(copied)
	return copied[int(float64(len(copied)-1)*quantile+0.999999)]
}

func medianUint64(values []uint64) uint64 {
	if len(values) == 0 {
		return 0
	}
	copied := append([]uint64(nil), values...)
	sort.Slice(copied, func(left, right int) bool { return copied[left] < copied[right] })
	return copied[len(copied)/2]
}

func residentBytes() uint64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "VmRSS:" && fields[2] == "kB" {
			if value, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				return value << 10
			}
		}
	}
	return 0
}

func cypherString(value string) string { return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"` }

func cypherList(values []string) string {
	items := make([]string, len(values))
	for index, value := range values {
		items[index] = cypherString(value)
	}
	return "[" + strings.Join(items, ", ") + "]"
}

func milliseconds(value time.Duration) float64 { return float64(value) / float64(time.Millisecond) }

func copyFile(sourcePath, destinationPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	destination, err := os.Create(destinationPath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(destination, source); err != nil {
		_ = destination.Close()
		return err
	}
	return destination.Close()
}

func gitState() string {
	output, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	commit := strings.TrimSpace(string(output))
	if err := exec.Command("git", "diff", "--quiet").Run(); err != nil {
		return commit + "-dirty"
	}
	return commit
}

func writeOutputs(outputDir string, result results) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "results.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	var report strings.Builder
	fmt.Fprintln(&report, "# Perfil de deltas LadybugDB")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- Commit medido: `%s`\n", result.Commit)
	fmt.Fprintf(&report, "- Fecha: `%s`\n", result.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&report, "- Plataforma: `%s/%s`, `%s`\n", result.GOOS, result.GOARCH, result.GoVersion)
	fmt.Fprintf(&report, "- Base: `%d` bytes; muestras por caso: `%d`\n", result.BaseBytes, result.Samples)
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Resultados")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Estrategia | Relaciones | Deltas agregados | p50 ms | p95 ms | Relaciones/s | RSS pico bytes | Alloc/batch bytes |")
	fmt.Fprintln(&report, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	for _, profile := range result.Cases {
		if profile.Skipped {
			fmt.Fprintf(&report, "| `%s` | %d | %d | — | — | — | — | — |\n", profile.Strategy, profile.Relations, profile.AggregatedDeltas)
			continue
		}
		fmt.Fprintf(&report, "| `%s` | %d | %d | %.1f | %.1f | %.1f | %d | %d |\n", profile.Strategy, profile.Relations, profile.AggregatedDeltas, profile.Summary.P50MS, profile.Summary.P95MS, profile.Summary.ThroughputPerSecond, profile.Summary.PeakRSSBytes, profile.Summary.AllocationsPerBatch)
	}
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Fases p50")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Estrategia | Relaciones | Stage | BEGIN | Lookups | Deletes | Creates | Integrity | COMMIT | Close | Total |")
	fmt.Fprintln(&report, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	for _, profile := range result.Cases {
		if profile.Skipped {
			continue
		}
		phase := profile.Summary.PhasesP50
		fmt.Fprintf(&report, "| `%s` | %d | %.1f | %.1f | %.1f | %.1f | %.1f | %.1f | %.1f | %.1f | %.1f |\n", profile.Strategy, profile.Relations, phase.Stage, phase.Begin, phase.Lookups, phase.Deletes, phase.Creates, phase.Integrity, phase.Commit, phase.Close, phase.Total)
	}
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Gate")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "`LADYBUG_DELTA_PERFORMANCE_PASS`: **%t**. Estrategia segura elegida: `%s`; p95 1–10 relaciones: %.1f ms (límite < 150 ms); p95 1.000 relaciones: %.1f ms (límite < 500 ms).\n", result.Gate.Passed, result.Gate.ChosenStrategy, result.Gate.SmallDeltaP95MS, result.Gate.LargeDeltaP95MS)
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "El writer usa sentencias preparadas individuales para 1–10 relaciones y un `UNWIND` por tipo a partir de 11; también borra por batch. `staging_copy` es más rápido en el corpus, pero no se adopta para un delta genérico: el esquema permite multiplicidad y `COPY` no preserva por sí solo la detección atómica de duplicados.")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "La estrategia agregada agrupa diez deltas antes de mutar. Su medición excluye la espera de cola, por lo que una ventana de 150–500 ms no puede declarar el objetivo end-to-end de 1–10 relaciones.")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Límites")
	fmt.Fprintln(&report)
	for _, limitation := range result.Limitations {
		fmt.Fprintf(&report, "- %s\n", limitation)
	}
	return os.WriteFile(filepath.Join(outputDir, "report.md"), []byte(report.String()), 0o644)
}

//go:build ladybug && cgo

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
	"sort"
	"strconv"
	"strings"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"
	"github.com/Luqueee/kivgraph/internal/procstat"
)

const (
	defaultCorpusDir    = "testdata/generated/synthetic"
	defaultDatabaseDir  = "benchmarks/ladybug-batch/databases"
	defaultSchema       = "schemas/ladybug/001-synthetic.cypher"
	defaultOutputDir    = "benchmarks/ladybug-batch"
	initialSymbols      = 100_000
	initialEdges        = 1_000_000
	maxQualificationRSS = int64(2 * 1024 * 1024 * 1024)
)

var defaultBatchSizes = []int{100, 1_000, 10_000, 50_000}

type config struct {
	CorpusDir   string
	DatabaseDir string
	SchemaPath  string
	OutputDir   string
	BatchSizes  []int
}

type benchmarkResults struct {
	Benchmark            string           `json:"benchmark"`
	Command              string           `json:"command"`
	Commit               string           `json:"commit"`
	GeneratedAt          time.Time        `json:"generated_at"`
	CorpusSeed           int64            `json:"corpus_seed"`
	FullInitialScale     bool             `json:"full_initial_scale"`
	Repositories         int              `json:"repositories"`
	Files                int              `json:"files"`
	Symbols              int              `json:"symbols"`
	Edges                int              `json:"edges"`
	Scenarios            []scenarioResult `json:"scenarios"`
	RecommendedBatchSize int              `json:"recommended_batch_size"`
}

type scenarioResult struct {
	BatchSize        int     `json:"batch_size"`
	Nodes            int     `json:"nodes"`
	Edges            int     `json:"edges"`
	NodeDurationMS   float64 `json:"node_duration_ms"`
	EdgeDurationMS   float64 `json:"edge_duration_ms"`
	TotalDurationMS  float64 `json:"total_duration_ms"`
	NodesPerSecond   float64 `json:"nodes_per_s"`
	EdgesPerSecond   float64 `json:"edges_per_s"`
	RecordsPerSecond float64 `json:"records_per_s"`
	Transactions     int     `json:"transactions"`
	CommitDurationMS float64 `json:"commit_duration_ms"`
	PeakRSSBytes     int64   `json:"peak_rss_bytes"`
	DatabaseBytes    int64   `json:"database_bytes"`
}

type manifest struct {
	Seed         int64          `json:"seed"`
	Repositories int            `json:"repositories"`
	Files        int            `json:"files"`
	Symbols      int            `json:"symbols"`
	Edges        int            `json:"edges"`
	EdgeCounts   map[string]int `json:"edge_counts"`
}

type repositoryRecord struct {
	StableKey string `json:"stable_key"`
	Name      string `json:"name"`
	Path      string `json:"path"`
	Language  string `json:"language"`
}

type fileRecord struct {
	StableKey     string `json:"stable_key"`
	RepositoryKey string `json:"repository_key"`
	Path          string `json:"path"`
	ContentHash   string `json:"content_hash"`
	Language      string `json:"language"`
}

type symbolRecord struct {
	StableKey     string `json:"stable_key"`
	RepositoryKey string `json:"repository_key"`
	FileKey       string `json:"file_key"`
	Name          string `json:"name"`
	QualifiedName string `json:"qualified_name"`
	Kind          string `json:"kind"`
	Signature     string `json:"signature"`
	StartLine     int64  `json:"start_line"`
	EndLine       int64  `json:"end_line"`
}

type edgeRecord struct {
	Type          string `json:"type"`
	From          string `json:"from"`
	To            string `json:"to"`
	RelationKind  string `json:"relation_kind"`
	EvidenceKind  string `json:"evidence_kind"`
	SourceFileKey string `json:"source_file_key"`
	TargetFileKey string `json:"target_file_key"`
}

type statements struct {
	repository  *lbug.PreparedStatement
	file        *lbug.PreparedStatement
	symbol      *lbug.PreparedStatement
	contains    *lbug.PreparedStatement
	defines     *lbug.PreparedStatement
	references  *lbug.PreparedStatement
	callsDirect *lbug.PreparedStatement
}

type batchExecutor struct {
	connection     *lbug.Connection
	transactions   int
	commitDuration time.Duration
	peakRSS        int64
}

func main() {
	var batchSizes string
	var singleScenario bool
	cfg := config{}
	flag.StringVar(&cfg.CorpusDir, "corpus", defaultCorpusDir, "generated corpus directory")
	flag.StringVar(&cfg.DatabaseDir, "database-dir", defaultDatabaseDir, "directory for scenario databases")
	flag.StringVar(&cfg.SchemaPath, "schema", defaultSchema, "LadybugDB schema path")
	flag.StringVar(&cfg.OutputDir, "output", defaultOutputDir, "results output directory")
	flag.StringVar(&batchSizes, "batch-sizes", "100,1000,10000,50000", "comma-separated batch sizes")
	flag.BoolVar(&singleScenario, "single-scenario", false, "run in the current process instead of isolating scenarios")
	flag.Parse()

	var err error
	cfg.BatchSizes, err = parseBatchSizes(batchSizes)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var result benchmarkResults
	if len(cfg.BatchSizes) > 1 && !singleScenario {
		result, err = runIsolated(context.Background(), cfg)
	} else {
		result, err = run(context.Background(), cfg)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := writeOutputs(cfg.OutputDir, result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, scenario := range result.Scenarios {
		fmt.Printf("batch %d: %.1f nodes/s, %.1f edges/s\n", scenario.BatchSize, scenario.NodesPerSecond, scenario.EdgesPerSecond)
	}
}

func run(ctx context.Context, cfg config) (benchmarkResults, error) {
	corpus, err := readManifest(filepath.Join(cfg.CorpusDir, "manifest.json"))
	if err != nil {
		return benchmarkResults{}, err
	}
	if len(cfg.BatchSizes) == 0 {
		return benchmarkResults{}, fmt.Errorf("at least one batch size is required")
	}
	if err := os.MkdirAll(cfg.DatabaseDir, 0o755); err != nil {
		return benchmarkResults{}, fmt.Errorf("create database directory: %w", err)
	}

	result := newResults(corpus)
	for _, batchSize := range cfg.BatchSizes {
		if batchSize <= 0 {
			return benchmarkResults{}, fmt.Errorf("batch size must be positive: %d", batchSize)
		}
		databasePath := filepath.Join(cfg.DatabaseDir, fmt.Sprintf("batch-%d.db", batchSize))
		scenario, err := runScenario(ctx, cfg, corpus, batchSize, databasePath)
		if err != nil {
			return benchmarkResults{}, fmt.Errorf("batch size %d: %w", batchSize, err)
		}
		result.Scenarios = append(result.Scenarios, scenario)
	}
	result.RecommendedBatchSize = recommendBatchSize(result.Scenarios)
	return result, nil
}

func runIsolated(ctx context.Context, cfg config) (benchmarkResults, error) {
	corpus, err := readManifest(filepath.Join(cfg.CorpusDir, "manifest.json"))
	if err != nil {
		return benchmarkResults{}, err
	}
	outputRoot, err := os.MkdirTemp("", "kivgraph-ladybug-batch-")
	if err != nil {
		return benchmarkResults{}, fmt.Errorf("create scenario output directory: %w", err)
	}
	defer os.RemoveAll(outputRoot)

	result := newResults(corpus)
	for _, batchSize := range cfg.BatchSizes {
		scenarioOutput := filepath.Join(outputRoot, strconv.Itoa(batchSize))
		command := exec.CommandContext(
			ctx,
			os.Args[0],
			"--corpus", cfg.CorpusDir,
			"--database-dir", cfg.DatabaseDir,
			"--schema", cfg.SchemaPath,
			"--output", scenarioOutput,
			"--batch-sizes", strconv.Itoa(batchSize),
			"--single-scenario",
		)
		output, err := command.CombinedOutput()
		if err != nil {
			return benchmarkResults{}, fmt.Errorf("batch size %d subprocess: %w: %s", batchSize, err, strings.TrimSpace(string(output)))
		}
		data, err := os.ReadFile(filepath.Join(scenarioOutput, "results.json"))
		if err != nil {
			return benchmarkResults{}, fmt.Errorf("read batch size %d results: %w", batchSize, err)
		}
		var child benchmarkResults
		if err := json.Unmarshal(data, &child); err != nil {
			return benchmarkResults{}, fmt.Errorf("decode batch size %d results: %w", batchSize, err)
		}
		if len(child.Scenarios) != 1 || child.Scenarios[0].BatchSize != batchSize {
			return benchmarkResults{}, fmt.Errorf("batch size %d subprocess returned invalid scenarios", batchSize)
		}
		result.Scenarios = append(result.Scenarios, child.Scenarios[0])
	}
	result.RecommendedBatchSize = recommendBatchSize(result.Scenarios)
	return result, nil
}

func newResults(corpus manifest) benchmarkResults {
	return benchmarkResults{
		Benchmark:        "ladybug-batch",
		Command:          strings.Join(os.Args, " "),
		Commit:           gitState(),
		GeneratedAt:      time.Now().UTC(),
		CorpusSeed:       corpus.Seed,
		FullInitialScale: corpus.Symbols >= initialSymbols && corpus.Edges >= initialEdges,
		Repositories:     corpus.Repositories,
		Files:            corpus.Files,
		Symbols:          corpus.Symbols,
		Edges:            corpus.Edges,
	}
}

func runScenario(ctx context.Context, cfg config, corpus manifest, batchSize int, databasePath string) (scenario scenarioResult, returnErr error) {
	if err := ensureNewDatabasePath(databasePath); err != nil {
		return scenarioResult{}, err
	}
	database, err := lbug.OpenDatabase(databasePath, lbug.DefaultSystemConfig())
	if err != nil {
		return scenarioResult{}, fmt.Errorf("open database: %w", err)
	}
	defer database.Close()
	connection, err := lbug.OpenConnection(database)
	if err != nil {
		return scenarioResult{}, fmt.Errorf("open connection: %w", err)
	}
	defer connection.Close()
	if err := applySchema(connection, cfg.SchemaPath); err != nil {
		return scenarioResult{}, err
	}
	prepared, err := prepareStatements(connection)
	if err != nil {
		return scenarioResult{}, err
	}
	defer prepared.close()
	executor := &batchExecutor{connection: connection, peakRSS: readRSSBytes()}

	nodeStart := time.Now()
	repositories, err := loadBatches(ctx, filepath.Join(cfg.CorpusDir, "repositories.jsonl"), batchSize, func(records []repositoryRecord) error {
		return executor.execute(prepared.repository, repositoryRows(records))
	})
	if err != nil {
		return scenarioResult{}, fmt.Errorf("load repositories: %w", err)
	}
	files, err := loadBatches(ctx, filepath.Join(cfg.CorpusDir, "files.jsonl"), batchSize, func(records []fileRecord) error {
		return executor.execute(prepared.file, fileRows(records))
	})
	if err != nil {
		return scenarioResult{}, fmt.Errorf("load files: %w", err)
	}
	symbols, err := loadBatches(ctx, filepath.Join(cfg.CorpusDir, "symbols.jsonl"), batchSize, func(records []symbolRecord) error {
		return executor.execute(prepared.symbol, symbolRows(records))
	})
	if err != nil {
		return scenarioResult{}, fmt.Errorf("load symbols: %w", err)
	}
	nodeDuration := time.Since(nodeStart)

	edgeStart := time.Now()
	edges, err := loadEdgeBatches(ctx, filepath.Join(cfg.CorpusDir, "edges.jsonl"), batchSize, func(edgeType string, records []edgeRecord) error {
		statement, err := edgePreparedStatement(prepared, edgeType)
		if err != nil {
			return err
		}
		return executor.execute(statement, edgeRows(records))
	})
	if err != nil {
		return scenarioResult{}, fmt.Errorf("load edges: %w", err)
	}
	edgeDuration := time.Since(edgeStart)

	if repositories != corpus.Repositories || files != corpus.Files || symbols != corpus.Symbols || edges != corpus.Edges {
		return scenarioResult{}, fmt.Errorf("loaded counts differ from manifest: repositories=%d files=%d symbols=%d edges=%d", repositories, files, symbols, edges)
	}
	if err := verifyStoredCounts(connection, corpus); err != nil {
		return scenarioResult{}, err
	}
	connection.Close()
	database.Close()
	databaseBytes, err := pathSize(databasePath)
	if err != nil {
		return scenarioResult{}, err
	}

	nodes := repositories + files + symbols
	totalDuration := nodeDuration + edgeDuration
	return scenarioResult{
		BatchSize:        batchSize,
		Nodes:            nodes,
		Edges:            edges,
		NodeDurationMS:   milliseconds(nodeDuration),
		EdgeDurationMS:   milliseconds(edgeDuration),
		TotalDurationMS:  milliseconds(totalDuration),
		NodesPerSecond:   throughput(nodes, nodeDuration),
		EdgesPerSecond:   throughput(edges, edgeDuration),
		RecordsPerSecond: throughput(nodes+edges, totalDuration),
		Transactions:     executor.transactions,
		CommitDurationMS: milliseconds(executor.commitDuration),
		PeakRSSBytes:     executor.peakRSS,
		DatabaseBytes:    databaseBytes,
	}, nil
}

func (executor *batchExecutor) execute(statement *lbug.PreparedStatement, rows []any) error {
	if len(rows) == 0 {
		return nil
	}
	if err := executeQuery(executor.connection, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = executeQuery(executor.connection, "ROLLBACK")
		}
	}()
	result, err := executor.connection.Execute(statement, map[string]any{"rows": rows})
	if result != nil {
		result.Close()
	}
	if err != nil {
		return err
	}
	commitStart := time.Now()
	if err := executeQuery(executor.connection, "COMMIT"); err != nil {
		return err
	}
	committed = true
	executor.commitDuration += time.Since(commitStart)
	executor.transactions++
	executor.peakRSS = max(executor.peakRSS, readRSSBytes())
	return nil
}

func prepareStatements(connection *lbug.Connection) (statements, error) {
	prepared := statements{}
	type querySpec struct {
		name   string
		query  string
		target **lbug.PreparedStatement
	}
	queries := []querySpec{
		{"Repository", "UNWIND $rows AS row CREATE (:Repository {stable_key: row.stable_key, name: row.name, path: row.path, language: row.language})", &prepared.repository},
		{"File", "UNWIND $rows AS row CREATE (:File {stable_key: row.stable_key, repository_key: row.repository_key, path: row.path, content_hash: row.content_hash, language: row.language})", &prepared.file},
		{"Symbol", "UNWIND $rows AS row CREATE (:Symbol {stable_key: row.stable_key, repository_key: row.repository_key, file_key: row.file_key, name: row.name, qualified_name: row.qualified_name, kind: row.kind, signature: row.signature, start_line: row.start_line, end_line: row.end_line})", &prepared.symbol},
		{"CONTAINS", "UNWIND $rows AS row MATCH (source:Repository {stable_key: row.from}), (target:File {stable_key: row.to}) CREATE (source)-[:CONTAINS {relation_kind: row.relation_kind}]->(target)", &prepared.contains},
		{"DEFINES", "UNWIND $rows AS row MATCH (source:File {stable_key: row.from}), (target:Symbol {stable_key: row.to}) CREATE (source)-[:DEFINES {relation_kind: row.relation_kind}]->(target)", &prepared.defines},
		{"REFERENCES", "UNWIND $rows AS row MATCH (source:Symbol {stable_key: row.from}), (target:Symbol {stable_key: row.to}) CREATE (source)-[:REFERENCES {evidence_kind: row.evidence_kind, source_file_key: row.source_file_key, target_file_key: row.target_file_key}]->(target)", &prepared.references},
		{"CALLS_DIRECT", "UNWIND $rows AS row MATCH (source:Symbol {stable_key: row.from}), (target:Symbol {stable_key: row.to}) CREATE (source)-[:CALLS_DIRECT {evidence_kind: row.evidence_kind, source_file_key: row.source_file_key, target_file_key: row.target_file_key}]->(target)", &prepared.callsDirect},
	}
	for _, item := range queries {
		statement, err := connection.Prepare(item.query)
		if err != nil {
			prepared.close()
			return statements{}, fmt.Errorf("prepare %s: %w", item.name, err)
		}
		*item.target = statement
	}
	return prepared, nil
}

func (prepared statements) close() {
	for _, statement := range []*lbug.PreparedStatement{prepared.repository, prepared.file, prepared.symbol, prepared.contains, prepared.defines, prepared.references, prepared.callsDirect} {
		if statement != nil {
			statement.Close()
		}
	}
}

func repositoryRows(records []repositoryRecord) []any {
	rows := make([]any, len(records))
	for index, record := range records {
		rows[index] = map[string]any{"stable_key": record.StableKey, "name": record.Name, "path": record.Path, "language": record.Language}
	}
	return rows
}

func fileRows(records []fileRecord) []any {
	rows := make([]any, len(records))
	for index, record := range records {
		rows[index] = map[string]any{"stable_key": record.StableKey, "repository_key": record.RepositoryKey, "path": record.Path, "content_hash": record.ContentHash, "language": record.Language}
	}
	return rows
}

func symbolRows(records []symbolRecord) []any {
	rows := make([]any, len(records))
	for index, record := range records {
		rows[index] = map[string]any{"stable_key": record.StableKey, "repository_key": record.RepositoryKey, "file_key": record.FileKey, "name": record.Name, "qualified_name": record.QualifiedName, "kind": record.Kind, "signature": record.Signature, "start_line": record.StartLine, "end_line": record.EndLine}
	}
	return rows
}

func edgeRows(records []edgeRecord) []any {
	rows := make([]any, len(records))
	for index, record := range records {
		switch record.Type {
		case "CONTAINS", "DEFINES":
			rows[index] = map[string]any{"from": record.From, "to": record.To, "relation_kind": record.RelationKind}
		default:
			rows[index] = map[string]any{"from": record.From, "to": record.To, "evidence_kind": record.EvidenceKind, "source_file_key": record.SourceFileKey, "target_file_key": record.TargetFileKey}
		}
	}
	return rows
}

func edgePreparedStatement(prepared statements, edgeType string) (*lbug.PreparedStatement, error) {
	switch edgeType {
	case "CONTAINS":
		return prepared.contains, nil
	case "DEFINES":
		return prepared.defines, nil
	case "REFERENCES":
		return prepared.references, nil
	case "CALLS_DIRECT":
		return prepared.callsDirect, nil
	default:
		return nil, fmt.Errorf("unsupported edge type %q", edgeType)
	}
}

func loadBatches[T any](ctx context.Context, path string, batchSize int, load func([]T) error) (int, error) {
	batch := make([]T, 0, batchSize)
	count, err := scanJSONLines(ctx, path, func(record T) error {
		batch = append(batch, record)
		if len(batch) < batchSize {
			return nil
		}
		if err := load(batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	})
	if err != nil {
		return count, err
	}
	if err := load(batch); err != nil {
		return count, err
	}
	return count, nil
}

func loadEdgeBatches(ctx context.Context, path string, batchSize int, load func(string, []edgeRecord) error) (int, error) {
	buffers := map[string][]edgeRecord{}
	count, err := scanJSONLines(ctx, path, func(record edgeRecord) error {
		batch := append(buffers[record.Type], record)
		if len(batch) < batchSize {
			buffers[record.Type] = batch
			return nil
		}
		if err := load(record.Type, batch); err != nil {
			return err
		}
		buffers[record.Type] = batch[:0]
		return nil
	})
	if err != nil {
		return count, err
	}
	types := make([]string, 0, len(buffers))
	for edgeType := range buffers {
		types = append(types, edgeType)
	}
	sort.Strings(types)
	for _, edgeType := range types {
		if err := load(edgeType, buffers[edgeType]); err != nil {
			return count, err
		}
	}
	return count, nil
}

func scanJSONLines[T any](ctx context.Context, path string, visit func(T) error) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 256*1024)
	count := 0
	for {
		if count%1024 == 0 {
			if err := ctx.Err(); err != nil {
				return count, err
			}
		}
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var record T
			if decodeErr := json.Unmarshal(line, &record); decodeErr != nil {
				return count, fmt.Errorf("decode record %d: %w", count+1, decodeErr)
			}
			if visitErr := visit(record); visitErr != nil {
				return count, fmt.Errorf("record %d: %w", count+1, visitErr)
			}
			count++
		}
		if errors.Is(err, io.EOF) {
			return count, nil
		}
		if err != nil {
			return count, err
		}
	}
}

func applySchema(connection *lbug.Connection, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "//") {
			kept = append(kept, line)
		}
	}
	for _, statement := range strings.Split(strings.Join(kept, "\n"), ";") {
		if statement = strings.TrimSpace(statement); statement != "" {
			if err := executeQuery(connection, statement); err != nil {
				return fmt.Errorf("apply schema: %w", err)
			}
		}
	}
	return nil
}

func verifyStoredCounts(connection *lbug.Connection, corpus manifest) error {
	checks := []struct {
		name     string
		query    string
		expected int
	}{
		{"Repository", "MATCH (value:Repository) RETURN count(value)", corpus.Repositories},
		{"File", "MATCH (value:File) RETURN count(value)", corpus.Files},
		{"Symbol", "MATCH (value:Symbol) RETURN count(value)", corpus.Symbols},
		{"CONTAINS", "MATCH ()-[value:CONTAINS]->() RETURN count(value)", corpus.EdgeCounts["CONTAINS"]},
		{"DEFINES", "MATCH ()-[value:DEFINES]->() RETURN count(value)", corpus.EdgeCounts["DEFINES"]},
		{"REFERENCES", "MATCH ()-[value:REFERENCES]->() RETURN count(value)", corpus.EdgeCounts["REFERENCES"]},
		{"CALLS_DIRECT", "MATCH ()-[value:CALLS_DIRECT]->() RETURN count(value)", corpus.EdgeCounts["CALLS_DIRECT"]},
	}
	for _, check := range checks {
		actual, err := queryCount(connection, check.query)
		if err != nil {
			return fmt.Errorf("verify %s: %w", check.name, err)
		}
		if actual != int64(check.expected) {
			return fmt.Errorf("verify %s: got %d, want %d", check.name, actual, check.expected)
		}
	}
	return nil
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
		return 0, fmt.Errorf("count query returned no rows")
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
		return 0, fmt.Errorf("count returned %T", value)
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

func readManifest(path string) (manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var value manifest
	if err := json.Unmarshal(data, &value); err != nil {
		return manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return value, nil
}

func parseBatchSizes(value string) ([]int, error) {
	parts := strings.Split(value, ",")
	sizes := make([]int, 0, len(parts))
	seen := map[int]struct{}{}
	for _, part := range parts {
		size, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || size <= 0 {
			return nil, fmt.Errorf("invalid batch size %q", part)
		}
		if _, exists := seen[size]; exists {
			continue
		}
		seen[size] = struct{}{}
		sizes = append(sizes, size)
	}
	return sizes, nil
}

func ensureNewDatabasePath(path string) error {
	_, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("database path already exists: %s", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func recommendBatchSize(scenarios []scenarioResult) int {
	recommended := 0
	bestThroughput := 0.0
	for _, scenario := range scenarios {
		if scenario.PeakRSSBytes > maxQualificationRSS {
			continue
		}
		if scenario.RecordsPerSecond > bestThroughput {
			recommended = scenario.BatchSize
			bestThroughput = scenario.RecordsPerSecond
		}
	}
	return recommended
}
func writeOutputs(outputDir string, result benchmarkResults) error {
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
	fmt.Fprintln(&report, "# LadybugDB batch insert benchmark")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- Command: `%s`\n- Commit: `%s`\n- Generated at: `%s`\n- Corpus seed: `%d`\n- Full initial scale: `%t`\n", result.Command, result.Commit, result.GeneratedAt.Format(time.RFC3339), result.CorpusSeed, result.FullInitialScale)
	fmt.Fprintf(&report, "- Recommended batch size under the 2 GiB RSS limit: `%d`\n", result.RecommendedBatchSize)
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Batch | Nodes/s | Edges/s | Records/s | Transactions | Commit ms | Peak RSS | Database bytes |")
	fmt.Fprintln(&report, "| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	for _, scenario := range result.Scenarios {
		fmt.Fprintf(&report, "| %d | %.1f | %.1f | %.1f | %d | %.1f | %d | %d |\n", scenario.BatchSize, scenario.NodesPerSecond, scenario.EdgesPerSecond, scenario.RecordsPerSecond, scenario.Transactions, scenario.CommitDurationMS, scenario.PeakRSSBytes, scenario.DatabaseBytes)
	}
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "Each batch is one `UNWIND $rows` prepared statement inside one explicit transaction. Database counts are verified after every scenario.")
	return os.WriteFile(filepath.Join(outputDir, "report.md"), []byte(report.String()), 0o644)
}

func throughput(records int, duration time.Duration) float64 {
	if duration <= 0 {
		return 0
	}
	return float64(records) / duration.Seconds()
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
}

func pathSize(path string) (int64, error) {
	var total int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}

// readRSSBytes samples the resident set size of this process. The executor
// takes the maximum over the samples, so the metric stays "peak observed RSS"
// and remains comparable with the published results.
func readRSSBytes() int64 {
	return procstat.ResidentBytes(os.Getpid())
}

func gitState() string {
	output, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	commit := strings.TrimSpace(string(output))
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err == nil && len(status) > 0 {
		commit += "-dirty"
	}
	return commit
}

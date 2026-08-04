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
	"strconv"
	"strings"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"
)

const (
	defaultCorpusDir    = "testdata/generated/synthetic"
	defaultDatabase     = "benchmarks/ladybug-individual/graph.db"
	defaultSchema       = "schemas/ladybug/001-synthetic.cypher"
	defaultOutputDir    = "benchmarks/ladybug-individual"
	initialScaleSymbols = 100_000
	initialScaleEdges   = 1_000_000
)

type config struct {
	CorpusDir       string
	DatabasePath    string
	SchemaPath      string
	OutputDir       string
	TransactionSize int
}

type results struct {
	Benchmark        string    `json:"benchmark"`
	Command          string    `json:"command"`
	Commit           string    `json:"commit"`
	GeneratedAt      time.Time `json:"generated_at"`
	CorpusSeed       int64     `json:"corpus_seed"`
	TransactionSize  int       `json:"transaction_size"`
	FullInitialScale bool      `json:"full_initial_scale"`
	Repositories     int       `json:"repositories"`
	Files            int       `json:"files"`
	Symbols          int       `json:"symbols"`
	Nodes            int       `json:"nodes"`
	Edges            int       `json:"edges"`
	NodeDurationMS   float64   `json:"node_duration_ms"`
	EdgeDurationMS   float64   `json:"edge_duration_ms"`
	TotalDurationMS  float64   `json:"total_duration_ms"`
	NodesPerSecond   float64   `json:"nodes_per_s"`
	EdgesPerSecond   float64   `json:"edges_per_s"`
	RecordsPerSecond float64   `json:"records_per_s"`
	Transactions     int       `json:"transactions"`
	CommitDurationMS float64   `json:"commit_duration_ms"`
	DatabaseBytes    int64     `json:"database_bytes"`
	RSSBytes         int64     `json:"rss_bytes"`
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

type transactionController struct {
	connection     *lbug.Connection
	size           int
	active         bool
	pending        int
	commits        int
	commitDuration time.Duration
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.CorpusDir, "corpus", defaultCorpusDir, "generated corpus directory")
	flag.StringVar(&cfg.DatabasePath, "database", defaultDatabase, "new LadybugDB database path")
	flag.StringVar(&cfg.SchemaPath, "schema", defaultSchema, "LadybugDB schema path")
	flag.StringVar(&cfg.OutputDir, "output", defaultOutputDir, "results output directory")
	flag.IntVar(&cfg.TransactionSize, "transaction-size", 1000, "records per transaction; zero uses autocommit")
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
	fmt.Printf("loaded %d nodes and %d edges at %.1f records/s\n", result.Nodes, result.Edges, result.RecordsPerSecond)
}

func run(ctx context.Context, cfg config) (result results, returnErr error) {
	if cfg.TransactionSize < 0 {
		return results{}, fmt.Errorf("transaction size must be non-negative: %d", cfg.TransactionSize)
	}
	corpus, err := readManifest(filepath.Join(cfg.CorpusDir, "manifest.json"))
	if err != nil {
		return results{}, err
	}
	if err := ensureNewDatabasePath(cfg.DatabasePath); err != nil {
		return results{}, err
	}
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o755); err != nil {
		return results{}, fmt.Errorf("create database parent: %w", err)
	}

	database, err := lbug.OpenDatabase(cfg.DatabasePath, lbug.DefaultSystemConfig())
	if err != nil {
		return results{}, fmt.Errorf("open database: %w", err)
	}
	defer database.Close()
	connection, err := lbug.OpenConnection(database)
	if err != nil {
		return results{}, fmt.Errorf("open connection: %w", err)
	}
	defer connection.Close()

	if err := applySchema(connection, cfg.SchemaPath); err != nil {
		return results{}, err
	}
	prepared, err := prepareStatements(connection)
	if err != nil {
		return results{}, err
	}
	defer prepared.close()

	nodeTransaction := &transactionController{connection: connection, size: cfg.TransactionSize}
	nodeStart := time.Now()
	repositories, err := loadJSONLines(ctx, filepath.Join(cfg.CorpusDir, "repositories.jsonl"), func(record repositoryRecord) error {
		return executeOne(prepared.repository, connection, map[string]any{
			"stable_key": record.StableKey,
			"name":       record.Name,
			"path":       record.Path,
			"language":   record.Language,
		}, nodeTransaction)
	})
	if err != nil {
		nodeTransaction.rollback()
		return results{}, fmt.Errorf("load repositories: %w", err)
	}
	files, err := loadJSONLines(ctx, filepath.Join(cfg.CorpusDir, "files.jsonl"), func(record fileRecord) error {
		return executeOne(prepared.file, connection, map[string]any{
			"stable_key":     record.StableKey,
			"repository_key": record.RepositoryKey,
			"path":           record.Path,
			"content_hash":   record.ContentHash,
			"language":       record.Language,
		}, nodeTransaction)
	})
	if err != nil {
		nodeTransaction.rollback()
		return results{}, fmt.Errorf("load files: %w", err)
	}
	symbols, err := loadJSONLines(ctx, filepath.Join(cfg.CorpusDir, "symbols.jsonl"), func(record symbolRecord) error {
		return executeOne(prepared.symbol, connection, map[string]any{
			"stable_key":     record.StableKey,
			"repository_key": record.RepositoryKey,
			"file_key":       record.FileKey,
			"name":           record.Name,
			"qualified_name": record.QualifiedName,
			"kind":           record.Kind,
			"signature":      record.Signature,
			"start_line":     record.StartLine,
			"end_line":       record.EndLine,
		}, nodeTransaction)
	})
	if err != nil {
		nodeTransaction.rollback()
		return results{}, fmt.Errorf("load symbols: %w", err)
	}
	if err := nodeTransaction.finish(); err != nil {
		return results{}, fmt.Errorf("commit nodes: %w", err)
	}
	nodeDuration := time.Since(nodeStart)

	edgeTransaction := &transactionController{connection: connection, size: cfg.TransactionSize}
	edgeStart := time.Now()
	edges, err := loadJSONLines(ctx, filepath.Join(cfg.CorpusDir, "edges.jsonl"), func(record edgeRecord) error {
		statement, arguments, err := edgeStatement(prepared, record)
		if err != nil {
			return err
		}
		return executeOne(statement, connection, arguments, edgeTransaction)
	})
	if err != nil {
		edgeTransaction.rollback()
		return results{}, fmt.Errorf("load edges: %w", err)
	}
	if err := edgeTransaction.finish(); err != nil {
		return results{}, fmt.Errorf("commit edges: %w", err)
	}
	edgeDuration := time.Since(edgeStart)

	if repositories != corpus.Repositories || files != corpus.Files || symbols != corpus.Symbols || edges != corpus.Edges {
		return results{}, fmt.Errorf(
			"corpus counts differ from manifest: got repositories=%d files=%d symbols=%d edges=%d",
			repositories,
			files,
			symbols,
			edges,
		)
	}
	if err := verifyStoredCounts(connection, corpus); err != nil {
		return results{}, err
	}

	connection.Close()
	database.Close()
	databaseBytes, err := pathSize(cfg.DatabasePath)
	if err != nil {
		return results{}, err
	}
	totalDuration := nodeDuration + edgeDuration
	nodes := repositories + files + symbols
	result = results{
		Benchmark:        "ladybug-individual",
		Command:          strings.Join(os.Args, " "),
		Commit:           gitState(),
		GeneratedAt:      time.Now().UTC(),
		CorpusSeed:       corpus.Seed,
		TransactionSize:  cfg.TransactionSize,
		FullInitialScale: symbols >= initialScaleSymbols && edges >= initialScaleEdges,
		Repositories:     repositories,
		Files:            files,
		Symbols:          symbols,
		Nodes:            nodes,
		Edges:            edges,
		NodeDurationMS:   milliseconds(nodeDuration),
		EdgeDurationMS:   milliseconds(edgeDuration),
		TotalDurationMS:  milliseconds(totalDuration),
		NodesPerSecond:   throughput(nodes, nodeDuration),
		EdgesPerSecond:   throughput(edges, edgeDuration),
		RecordsPerSecond: throughput(nodes+edges, totalDuration),
		Transactions:     nodeTransaction.commits + edgeTransaction.commits,
		CommitDurationMS: milliseconds(nodeTransaction.commitDuration + edgeTransaction.commitDuration),
		DatabaseBytes:    databaseBytes,
		RSSBytes:         readRSSBytes(),
	}
	return result, nil
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

func ensureNewDatabasePath(path string) error {
	if path == "" || path == ":memory:" {
		return fmt.Errorf("benchmark requires a new on-disk database path")
	}
	_, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("database path already exists: %s", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect database path: %w", err)
	}
	return nil
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
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		if err := executeQuery(connection, statement); err != nil {
			return fmt.Errorf("apply schema statement: %w", err)
		}
	}
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
		{"Repository", "CREATE (:Repository {stable_key: $stable_key, name: $name, path: $path, language: $language})", &prepared.repository},
		{"File", "CREATE (:File {stable_key: $stable_key, repository_key: $repository_key, path: $path, content_hash: $content_hash, language: $language})", &prepared.file},
		{"Symbol", "CREATE (:Symbol {stable_key: $stable_key, repository_key: $repository_key, file_key: $file_key, name: $name, qualified_name: $qualified_name, kind: $kind, signature: $signature, start_line: $start_line, end_line: $end_line})", &prepared.symbol},
		{"CONTAINS", "MATCH (source:Repository {stable_key: $from}), (target:File {stable_key: $to}) CREATE (source)-[:CONTAINS {relation_kind: $relation_kind}]->(target)", &prepared.contains},
		{"DEFINES", "MATCH (source:File {stable_key: $from}), (target:Symbol {stable_key: $to}) CREATE (source)-[:DEFINES {relation_kind: $relation_kind}]->(target)", &prepared.defines},
		{"REFERENCES", "MATCH (source:Symbol {stable_key: $from}), (target:Symbol {stable_key: $to}) CREATE (source)-[:REFERENCES {evidence_kind: $evidence_kind, source_file_key: $source_file_key, target_file_key: $target_file_key}]->(target)", &prepared.references},
		{"CALLS_DIRECT", "MATCH (source:Symbol {stable_key: $from}), (target:Symbol {stable_key: $to}) CREATE (source)-[:CALLS_DIRECT {evidence_kind: $evidence_kind, source_file_key: $source_file_key, target_file_key: $target_file_key}]->(target)", &prepared.callsDirect},
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
	for _, statement := range []*lbug.PreparedStatement{
		prepared.repository,
		prepared.file,
		prepared.symbol,
		prepared.contains,
		prepared.defines,
		prepared.references,
		prepared.callsDirect,
	} {
		if statement != nil {
			statement.Close()
		}
	}
}

func edgeStatement(prepared statements, record edgeRecord) (*lbug.PreparedStatement, map[string]any, error) {
	arguments := map[string]any{"from": record.From, "to": record.To}
	switch record.Type {
	case "CONTAINS":
		arguments["relation_kind"] = record.RelationKind
		return prepared.contains, arguments, nil
	case "DEFINES":
		arguments["relation_kind"] = record.RelationKind
		return prepared.defines, arguments, nil
	case "REFERENCES":
		arguments["evidence_kind"] = record.EvidenceKind
		arguments["source_file_key"] = record.SourceFileKey
		arguments["target_file_key"] = record.TargetFileKey
		return prepared.references, arguments, nil
	case "CALLS_DIRECT":
		arguments["evidence_kind"] = record.EvidenceKind
		arguments["source_file_key"] = record.SourceFileKey
		arguments["target_file_key"] = record.TargetFileKey
		return prepared.callsDirect, arguments, nil
	default:
		return nil, nil, fmt.Errorf("unsupported edge type %q", record.Type)
	}
}

func executeOne(statement *lbug.PreparedStatement, connection *lbug.Connection, arguments map[string]any, transaction *transactionController) error {
	if err := transaction.beforeRecord(); err != nil {
		return err
	}
	result, err := connection.Execute(statement, arguments)
	if result != nil {
		result.Close()
	}
	if err != nil {
		return err
	}
	return transaction.afterRecord()
}

func (transaction *transactionController) beforeRecord() error {
	if transaction.size == 0 || transaction.active {
		return nil
	}
	if err := executeQuery(transaction.connection, "BEGIN TRANSACTION"); err != nil {
		return err
	}
	transaction.active = true
	return nil
}

func (transaction *transactionController) afterRecord() error {
	if transaction.size == 0 {
		return nil
	}
	transaction.pending++
	if transaction.pending < transaction.size {
		return nil
	}
	return transaction.commit()
}

func (transaction *transactionController) finish() error {
	if !transaction.active {
		return nil
	}
	return transaction.commit()
}

func (transaction *transactionController) commit() error {
	start := time.Now()
	if err := executeQuery(transaction.connection, "COMMIT"); err != nil {
		return err
	}
	transaction.commitDuration += time.Since(start)
	transaction.commits++
	transaction.pending = 0
	transaction.active = false
	return nil
}

func (transaction *transactionController) rollback() {
	if !transaction.active {
		return
	}
	_ = executeQuery(transaction.connection, "ROLLBACK")
	transaction.pending = 0
	transaction.active = false
}

func executeQuery(connection *lbug.Connection, query string) error {
	result, err := connection.Query(query)
	if result != nil {
		result.Close()
	}
	return err
}

func loadJSONLines[T any](ctx context.Context, path string, load func(T) error) (int, error) {
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
			if loadErr := load(record); loadErr != nil {
				return count, fmt.Errorf("record %d: %w", count+1, loadErr)
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
func verifyStoredCounts(connection *lbug.Connection, corpus manifest) error {
	checks := []struct {
		name     string
		query    string
		expected int
	}{
		{name: "Repository", query: "MATCH (value:Repository) RETURN count(value)", expected: corpus.Repositories},
		{name: "File", query: "MATCH (value:File) RETURN count(value)", expected: corpus.Files},
		{name: "Symbol", query: "MATCH (value:Symbol) RETURN count(value)", expected: corpus.Symbols},
		{name: "CONTAINS", query: "MATCH ()-[value:CONTAINS]->() RETURN count(value)", expected: corpus.EdgeCounts["CONTAINS"]},
		{name: "DEFINES", query: "MATCH ()-[value:DEFINES]->() RETURN count(value)", expected: corpus.EdgeCounts["DEFINES"]},
		{name: "REFERENCES", query: "MATCH ()-[value:REFERENCES]->() RETURN count(value)", expected: corpus.EdgeCounts["REFERENCES"]},
		{name: "CALLS_DIRECT", query: "MATCH ()-[value:CALLS_DIRECT]->() RETURN count(value)", expected: corpus.EdgeCounts["CALLS_DIRECT"]},
	}
	for _, check := range checks {
		actual, err := queryCount(connection, check.query)
		if err != nil {
			return fmt.Errorf("verify %s count: %w", check.name, err)
		}
		if actual != int64(check.expected) {
			return fmt.Errorf("verify %s count: got %d, want %d", check.name, actual, check.expected)
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
		return 0, fmt.Errorf("count query returned %T, want int64", value)
	}
	return count, nil
}

func writeOutputs(outputDir string, result results) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "results.json"), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write results: %w", err)
	}

	var report strings.Builder
	fmt.Fprintln(&report, "# LadybugDB individual insert benchmark")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- Command: `%s`\n", result.Command)
	fmt.Fprintf(&report, "- Commit: `%s`\n", result.Commit)
	fmt.Fprintf(&report, "- Generated at: `%s`\n", result.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&report, "- Corpus seed: `%d`\n", result.CorpusSeed)
	fmt.Fprintf(&report, "- Transaction size: `%d` records\n", result.TransactionSize)
	fmt.Fprintf(&report, "- Full initial scale: `%t`\n", result.FullInitialScale)
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Results")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Nodes | Edges | Nodes/s | Edges/s | Records/s | Transactions | Commit ms | RSS | Database bytes |")
	fmt.Fprintln(&report, "| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	fmt.Fprintf(&report, "| %d | %d | %.1f | %.1f | %.1f | %d | %.1f | %d | %d |\n",
		result.Nodes,
		result.Edges,
		result.NodesPerSecond,
		result.EdgesPerSecond,
		result.RecordsPerSecond,
		result.Transactions,
		result.CommitDurationMS,
		result.RSSBytes,
		result.DatabaseBytes,
	)
	fmt.Fprintln(&report)
	if !result.FullInitialScale {
		fmt.Fprintln(&report, "This recorded run uses the agreed reduced corpus. The full 100,000-symbol/1,000,000-edge qualification is deferred to the batched and bulk loaders.")
		fmt.Fprintln(&report)
	}
	fmt.Fprintln(&report, "Each node and edge is executed as one prepared statement. The transaction size only controls commit boundaries; it does not batch records into a statement. This is a baseline for comparison with batched and bulk loading.")
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
	if err != nil {
		return 0, fmt.Errorf("measure database size: %w", err)
	}
	return total, nil
}

func readRSSBytes() int64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 3 && fields[0] == "VmRSS:" {
			kilobytes, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil {
				return kilobytes * 1024
			}
		}
	}
	return 0
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

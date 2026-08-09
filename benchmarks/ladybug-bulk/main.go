//go:build ladybug && cgo

package main

import (
	"bufio"
	"context"
	"encoding/csv"
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
	"github.com/Luqueee/ladygraph/internal/procstat"
)

const (
	defaultCorpusDir        = "testdata/generated/synthetic"
	defaultDatabase         = "benchmarks/ladybug-bulk/graph.db"
	defaultSchema           = "schemas/ladybug/001-synthetic.cypher"
	defaultOutputDir        = "benchmarks/ladybug-bulk"
	defaultIndividualResult = "benchmarks/ladybug-individual/results.json"
	defaultBatchResult      = "benchmarks/ladybug-batch/results.json"
	initialSymbols          = 100_000
	initialEdges            = 1_000_000
	maxQualificationRSS     = int64(2 * 1024 * 1024 * 1024)
)

type config struct {
	CorpusDir      string
	DatabasePath   string
	SchemaPath     string
	OutputDir      string
	IndividualPath string
	BatchPath      string
}

type results struct {
	Benchmark             string         `json:"benchmark"`
	Command               string         `json:"command"`
	Commit                string         `json:"commit"`
	GeneratedAt           time.Time      `json:"generated_at"`
	CorpusSeed            int64          `json:"corpus_seed"`
	FullInitialScale      bool           `json:"full_initial_scale"`
	Repositories          int            `json:"repositories"`
	Files                 int            `json:"files"`
	Symbols               int            `json:"symbols"`
	Nodes                 int            `json:"nodes"`
	Edges                 int            `json:"edges"`
	CSVExportDurationMS   float64        `json:"csv_export_duration_ms"`
	NodeCopyDurationMS    float64        `json:"node_copy_duration_ms"`
	EdgeCopyDurationMS    float64        `json:"edge_copy_duration_ms"`
	CopyDurationMS        float64        `json:"copy_duration_ms"`
	EndToEndDurationMS    float64        `json:"end_to_end_duration_ms"`
	NodesPerSecond        float64        `json:"nodes_per_s"`
	EdgesPerSecond        float64        `json:"edges_per_s"`
	RecordsPerSecond      float64        `json:"records_per_s"`
	EndToEndRecordsPerSec float64        `json:"end_to_end_records_per_s"`
	PeakRSSBytes          int64          `json:"peak_rss_bytes"`
	DatabaseBytes         int64          `json:"database_bytes"`
	Comparison            []comparison   `json:"comparison,omitempty"`
	GateAssessment        gateAssessment `json:"gate_assessment"`
}

type comparison struct {
	Strategy         string  `json:"strategy"`
	Records          int     `json:"records"`
	RecordsPerSecond float64 `json:"records_per_s"`
	PeakRSSBytes     int64   `json:"peak_rss_bytes"`
	Comparable       bool    `json:"comparable"`
}

type gateAssessment struct {
	FullInitialScale bool `json:"full_initial_scale"`
	CountsVerified   bool `json:"counts_verified"`
	RSSWithin2GiB    bool `json:"rss_within_2_gib"`
	Passed           bool `json:"passed"`
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

type csvCorpus struct {
	directory string
	paths     map[string]string
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.CorpusDir, "corpus", defaultCorpusDir, "generated corpus directory")
	flag.StringVar(&cfg.DatabasePath, "database", defaultDatabase, "new LadybugDB database path")
	flag.StringVar(&cfg.SchemaPath, "schema", defaultSchema, "LadybugDB schema path")
	flag.StringVar(&cfg.OutputDir, "output", defaultOutputDir, "results output directory")
	flag.StringVar(&cfg.IndividualPath, "individual-results", defaultIndividualResult, "individual baseline results")
	flag.StringVar(&cfg.BatchPath, "batch-results", defaultBatchResult, "batch baseline results")
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
	fmt.Printf("COPY loaded %d nodes and %d edges at %.1f records/s (%.1f end-to-end)\n", result.Nodes, result.Edges, result.RecordsPerSecond, result.EndToEndRecordsPerSec)
}

func run(ctx context.Context, cfg config) (results, error) {
	corpus, err := readManifest(filepath.Join(cfg.CorpusDir, "manifest.json"))
	if err != nil {
		return results{}, err
	}
	if err := ensureNewDatabasePath(cfg.DatabasePath); err != nil {
		return results{}, err
	}
	csvStart := time.Now()
	csvFiles, err := exportCSV(ctx, cfg.CorpusDir)
	if err != nil {
		return results{}, err
	}
	defer os.RemoveAll(csvFiles.directory)
	csvDuration := time.Since(csvStart)
	peakRSS := readRSSBytes()

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

	nodeStart := time.Now()
	for _, table := range []string{"Repository", "File", "Symbol"} {
		if err := copyTable(connection, table, csvFiles.paths[table]); err != nil {
			return results{}, err
		}
		peakRSS = max(peakRSS, readRSSBytes())
	}
	nodeDuration := time.Since(nodeStart)
	edgeStart := time.Now()
	for _, table := range []string{"CONTAINS", "DEFINES", "REFERENCES", "CALLS_DIRECT"} {
		if err := copyTable(connection, table, csvFiles.paths[table]); err != nil {
			return results{}, err
		}
		peakRSS = max(peakRSS, readRSSBytes())
	}
	edgeDuration := time.Since(edgeStart)
	if err := verifyStoredCounts(connection, corpus); err != nil {
		return results{}, err
	}
	connection.Close()
	database.Close()
	databaseBytes, err := pathSize(cfg.DatabasePath)
	if err != nil {
		return results{}, err
	}

	nodes := corpus.Repositories + corpus.Files + corpus.Symbols
	copyDuration := nodeDuration + edgeDuration
	result := results{
		Benchmark:             "ladybug-bulk-copy",
		Command:               strings.Join(os.Args, " "),
		Commit:                gitState(),
		GeneratedAt:           time.Now().UTC(),
		CorpusSeed:            corpus.Seed,
		FullInitialScale:      corpus.Symbols >= initialSymbols && corpus.Edges >= initialEdges,
		Repositories:          corpus.Repositories,
		Files:                 corpus.Files,
		Symbols:               corpus.Symbols,
		Nodes:                 nodes,
		Edges:                 corpus.Edges,
		CSVExportDurationMS:   milliseconds(csvDuration),
		NodeCopyDurationMS:    milliseconds(nodeDuration),
		EdgeCopyDurationMS:    milliseconds(edgeDuration),
		CopyDurationMS:        milliseconds(copyDuration),
		EndToEndDurationMS:    milliseconds(csvDuration + copyDuration),
		NodesPerSecond:        throughput(nodes, nodeDuration),
		EdgesPerSecond:        throughput(corpus.Edges, edgeDuration),
		RecordsPerSecond:      throughput(nodes+corpus.Edges, copyDuration),
		EndToEndRecordsPerSec: throughput(nodes+corpus.Edges, csvDuration+copyDuration),
		PeakRSSBytes:          peakRSS,
		DatabaseBytes:         databaseBytes,
	}
	result.Comparison = loadComparison(cfg, nodes+corpus.Edges, result)
	result.GateAssessment = assessGate(result)
	return result, nil
}

func exportCSV(ctx context.Context, corpusDir string) (csvCorpus, error) {
	directory, err := os.MkdirTemp("", "ladygraph-ladybug-copy-")
	if err != nil {
		return csvCorpus{}, fmt.Errorf("create CSV directory: %w", err)
	}
	result := csvCorpus{directory: directory, paths: map[string]string{}}
	failed := true
	defer func() {
		if failed {
			_ = os.RemoveAll(directory)
		}
	}()

	if err := exportFile[repositoryRecord](ctx, filepath.Join(corpusDir, "repositories.jsonl"), filepath.Join(directory, "repositories.csv"), func(record repositoryRecord) []string {
		return []string{record.StableKey, record.Name, record.Path, record.Language}
	}); err != nil {
		return csvCorpus{}, fmt.Errorf("export repositories: %w", err)
	}
	result.paths["Repository"] = filepath.Join(directory, "repositories.csv")
	if err := exportFile[fileRecord](ctx, filepath.Join(corpusDir, "files.jsonl"), filepath.Join(directory, "files.csv"), func(record fileRecord) []string {
		return []string{record.StableKey, record.RepositoryKey, record.Path, record.ContentHash, record.Language}
	}); err != nil {
		return csvCorpus{}, fmt.Errorf("export files: %w", err)
	}
	result.paths["File"] = filepath.Join(directory, "files.csv")
	if err := exportFile[symbolRecord](ctx, filepath.Join(corpusDir, "symbols.jsonl"), filepath.Join(directory, "symbols.csv"), func(record symbolRecord) []string {
		return []string{record.StableKey, record.RepositoryKey, record.FileKey, record.Name, record.QualifiedName, record.Kind, record.Signature, strconv.FormatInt(record.StartLine, 10), strconv.FormatInt(record.EndLine, 10)}
	}); err != nil {
		return csvCorpus{}, fmt.Errorf("export symbols: %w", err)
	}
	result.paths["Symbol"] = filepath.Join(directory, "symbols.csv")
	if err := exportEdges(ctx, filepath.Join(corpusDir, "edges.jsonl"), directory, result.paths); err != nil {
		return csvCorpus{}, fmt.Errorf("export edges: %w", err)
	}
	failed = false
	return result, nil
}

func exportFile[T any](ctx context.Context, sourcePath, destinationPath string, row func(T) []string) error {
	destination, err := os.Create(destinationPath)
	if err != nil {
		return err
	}
	writer := csv.NewWriter(destination)
	_, scanErr := scanJSONLines(ctx, sourcePath, func(record T) error {
		return writer.Write(row(record))
	})
	writer.Flush()
	writeErr := writer.Error()
	closeErr := destination.Close()
	return errors.Join(scanErr, writeErr, closeErr)
}

func exportEdges(ctx context.Context, sourcePath, directory string, paths map[string]string) error {
	tables := []string{"CONTAINS", "DEFINES", "REFERENCES", "CALLS_DIRECT"}
	files := map[string]*os.File{}
	writers := map[string]*csv.Writer{}
	for _, table := range tables {
		path := filepath.Join(directory, strings.ToLower(table)+".csv")
		file, err := os.Create(path)
		if err != nil {
			return err
		}
		files[table] = file
		writers[table] = csv.NewWriter(file)
		paths[table] = path
	}
	_, scanErr := scanJSONLines(ctx, sourcePath, func(record edgeRecord) error {
		writer := writers[record.Type]
		if writer == nil {
			return fmt.Errorf("unsupported edge type %q", record.Type)
		}
		if record.Type == "CONTAINS" || record.Type == "DEFINES" {
			return writer.Write([]string{record.From, record.To, record.RelationKind})
		}
		return writer.Write([]string{record.From, record.To, record.EvidenceKind, record.SourceFileKey, record.TargetFileKey})
	})
	var finalErrors []error
	finalErrors = append(finalErrors, scanErr)
	for _, table := range tables {
		writers[table].Flush()
		finalErrors = append(finalErrors, writers[table].Error(), files[table].Close())
	}
	return errors.Join(finalErrors...)
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

func copyTable(connection *lbug.Connection, table, path string) error {
	query := fmt.Sprintf("COPY %s FROM %s", table, cypherString(path))
	start := time.Now()
	result, err := connection.Query(query)
	if result != nil {
		result.Close()
	}
	if err != nil {
		return fmt.Errorf("COPY %s after %s: %w", table, time.Since(start), err)
	}
	return nil
}

func cypherString(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
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

func assessGate(result results) gateAssessment {
	assessment := gateAssessment{
		FullInitialScale: result.FullInitialScale,
		CountsVerified:   true,
		RSSWithin2GiB:    result.PeakRSSBytes <= maxQualificationRSS,
	}
	assessment.Passed = assessment.FullInitialScale && assessment.CountsVerified && assessment.RSSWithin2GiB
	return assessment
}

func loadComparison(cfg config, records int, copyResult results) []comparison {
	values := []comparison{{Strategy: "COPY", Records: records, RecordsPerSecond: copyResult.RecordsPerSecond, PeakRSSBytes: copyResult.PeakRSSBytes, Comparable: true}}
	if data, err := os.ReadFile(cfg.IndividualPath); err == nil {
		var previous struct {
			Nodes            int     `json:"nodes"`
			Edges            int     `json:"edges"`
			RecordsPerSecond float64 `json:"records_per_s"`
			RSSBytes         int64   `json:"rss_bytes"`
		}
		if json.Unmarshal(data, &previous) == nil {
			previousRecords := previous.Nodes + previous.Edges
			values = append(values, comparison{Strategy: "CREATE", Records: previousRecords, RecordsPerSecond: previous.RecordsPerSecond, PeakRSSBytes: previous.RSSBytes, Comparable: previousRecords == records})
		}
	}
	if data, err := os.ReadFile(cfg.BatchPath); err == nil {
		var previous struct {
			Recommended int `json:"recommended_batch_size"`
			Scenarios   []struct {
				BatchSize        int     `json:"batch_size"`
				Nodes            int     `json:"nodes"`
				Edges            int     `json:"edges"`
				RecordsPerSecond float64 `json:"records_per_s"`
				PeakRSSBytes     int64   `json:"peak_rss_bytes"`
			} `json:"scenarios"`
		}
		if json.Unmarshal(data, &previous) == nil {
			for _, scenario := range previous.Scenarios {
				if scenario.BatchSize == previous.Recommended {
					previousRecords := scenario.Nodes + scenario.Edges
					values = append(values, comparison{Strategy: fmt.Sprintf("batch transaction (%d)", scenario.BatchSize), Records: previousRecords, RecordsPerSecond: scenario.RecordsPerSecond, PeakRSSBytes: scenario.PeakRSSBytes, Comparable: previousRecords == records})
				}
			}
		}
	}
	return values
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
	fmt.Fprintln(&report, "# LadybugDB bulk load benchmark")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- Command: `%s`\n- Commit: `%s`\n- Generated at: `%s`\n- Corpus seed: `%d`\n- Full initial scale: `%t`\n\n", result.Command, result.Commit, result.GeneratedAt.Format(time.RFC3339), result.CorpusSeed, result.FullInitialScale)
	fmt.Fprintln(&report, "## COPY result")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Nodes | Edges | CSV export ms | COPY ms | COPY records/s | End-to-end records/s | Peak RSS | Database bytes |")
	fmt.Fprintln(&report, "| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	fmt.Fprintf(&report, "| %d | %d | %.1f | %.1f | %.1f | %.1f | %d | %d |\n\n", result.Nodes, result.Edges, result.CSVExportDurationMS, result.CopyDurationMS, result.RecordsPerSecond, result.EndToEndRecordsPerSec, result.PeakRSSBytes, result.DatabaseBytes)
	fmt.Fprintln(&report, "## Strategy comparison")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Strategy | Records | Records/s | Peak RSS | Comparable corpus |")
	fmt.Fprintln(&report, "| --- | ---: | ---: | ---: | :---: |")
	for _, value := range result.Comparison {
		fmt.Fprintf(&report, "| %s | %d | %.1f | %d | %t |\n", value.Strategy, value.Records, value.RecordsPerSecond, value.PeakRSSBytes, value.Comparable)
	}
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Gate `LADYBUG_BULK_LOAD_PASS`")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- Full initial scale: `%t`\n", result.GateAssessment.FullInitialScale)
	fmt.Fprintf(&report, "- Stored counts verified: `%t`\n", result.GateAssessment.CountsVerified)
	fmt.Fprintf(&report, "- RSS within 2 GiB: `%t`\n", result.GateAssessment.RSSWithin2GiB)
	fmt.Fprintf(&report, "- Result: `%t`\n", result.GateAssessment.Passed)
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "COPY throughput excludes deterministic JSONL-to-CSV export. End-to-end throughput includes it. Stored node and relationship counts are verified before results are written.")
	return os.WriteFile(filepath.Join(outputDir, "report.md"), []byte(report.String()), 0o644)
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
	_, err := os.Stat(path)
	if err == nil {
		return fmt.Errorf("database path already exists: %s", path)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create database parent: %w", err)
	}
	return nil
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

// readRSSBytes samples the resident set size of this process. The scenario
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

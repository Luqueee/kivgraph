//go:build ladybug && cgo

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"time"

	"github.com/Luqueee/luque/internal/storage/ladybug"
)

const (
	defaultDatabase = "benchmarks/ladybug-bulk/graph.db"
	defaultCorpus   = "testdata/generated/synthetic"
	defaultOutput   = "benchmarks/ladybug-incremental"
)

type config struct {
	DatabasePath string
	CorpusDir    string
	OutputDir    string
}

type results struct {
	Benchmark         string              `json:"benchmark"`
	Command           string              `json:"command"`
	Commit            string              `json:"commit"`
	GeneratedAt       time.Time           `json:"generated_at"`
	GoVersion         string              `json:"go_version"`
	GOOS              string              `json:"goos"`
	GOARCH            string              `json:"goarch"`
	Corpus            manifest            `json:"corpus"`
	BaseDatabaseBytes int64               `json:"base_database_bytes"`
	Probes            []probeResult       `json:"probes"`
	Integrity         integrityAssessment `json:"integrity"`
}

type probeResult struct {
	Probe           string                 `json:"probe"`
	DurationMS      float64                `json:"duration_ms"`
	Mutation        ladybug.MutationResult `json:"mutation"`
	ExpectedFailure string                 `json:"expected_failure,omitempty"`
}

type integrityAssessment struct {
	DuplicateSymbolsRejected    bool `json:"duplicate_symbols_rejected"`
	DuplicateReferencesRejected bool `json:"duplicate_references_rejected"`
	NoGhostEdges                bool `json:"no_ghost_edges"`
	AtomicityVerified           bool `json:"atomicity_verified"`
	RollbackVerified            bool `json:"rollback_verified"`
	Passed                      bool `json:"passed"`
}

type manifest struct {
	SchemaVersion string `json:"schema_version"`
	Seed          int64  `json:"seed"`
	Repositories  int    `json:"repositories"`
	Files         int    `json:"files"`
	Symbols       int    `json:"symbols"`
	Edges         int    `json:"edges"`
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.DatabasePath, "database", defaultDatabase, "loaded LadybugDB database to copy")
	flag.StringVar(&cfg.CorpusDir, "corpus", defaultCorpus, "synthetic corpus directory")
	flag.StringVar(&cfg.OutputDir, "output", defaultOutput, "results output directory")
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
	for _, probe := range result.Probes {
		fmt.Printf("%s: %.3f ms\n", probe.Probe, probe.DurationMS)
	}
	fmt.Printf("integrity pass: %t\n", result.Integrity.Passed)
}

func run(ctx context.Context, cfg config) (results, error) {
	corpus, err := readManifest(filepath.Join(cfg.CorpusDir, "manifest.json"))
	if err != nil {
		return results{}, err
	}
	baseInfo, err := os.Stat(cfg.DatabasePath)
	if err != nil {
		return results{}, fmt.Errorf("stat base database: %w", err)
	}
	if !baseInfo.Mode().IsRegular() {
		return results{}, errors.New("base database must be a regular file")
	}
	workDir, err := os.MkdirTemp("", "luque-ladybug-incremental-")
	if err != nil {
		return results{}, err
	}
	defer os.RemoveAll(workDir)
	workDatabase := filepath.Join(workDir, "graph.db")
	if err := copyFile(cfg.DatabasePath, workDatabase); err != nil {
		return results{}, fmt.Errorf("copy base database: %w", err)
	}

	database, err := ladybug.Open(ctx, workDatabase, ladybug.DefaultConfig())
	if err != nil {
		return results{}, fmt.Errorf("open work database: %w", err)
	}
	defer database.Close()
	reader, err := database.OpenReader(ctx)
	if err != nil {
		return results{}, fmt.Errorf("open reader: %w", err)
	}
	defer reader.Close()
	writer, err := database.OpenWriter(ctx)
	if err != nil {
		return results{}, fmt.Errorf("open writer: %w", err)
	}
	defer writer.Close()

	result := results{
		Benchmark:         "ladybug-incremental",
		Command:           strings.Join(os.Args, " "),
		Commit:            gitState(),
		GeneratedAt:       time.Now().UTC(),
		GoVersion:         runtime.Version(),
		GOOS:              runtime.GOOS,
		GOARCH:            runtime.GOARCH,
		Corpus:            corpus,
		BaseDatabaseBytes: baseInfo.Size(),
	}

	single := incrementalSymbol("incremental-single", 0)
	probe, err := applyProbe(ctx, writer, "add_1_symbol", ladybug.Delta{AddSymbols: []ladybug.Symbol{single}})
	if err != nil {
		return results{}, err
	}
	result.Probes = append(result.Probes, probe)

	batch := make([]ladybug.Symbol, 1_000)
	for index := range batch {
		batch[index] = incrementalSymbol(fmt.Sprintf("incremental-%04d", index), index+1)
	}
	probe, err = applyProbe(ctx, writer, "add_1000_symbols", ladybug.Delta{AddSymbols: batch})
	if err != nil {
		return results{}, err
	}
	result.Probes = append(result.Probes, probe)

	initialReferences := []ladybug.Reference{
		incrementalReference(single, batch[0], ladybug.ReferenceKindReferences),
		incrementalReference(single, batch[1], ladybug.ReferenceKindCallsDirect),
		incrementalReference(batch[2], single, ladybug.ReferenceKindReferences),
	}
	probe, err = applyProbe(ctx, writer, "add_edges", ladybug.Delta{AddReferences: initialReferences})
	if err != nil {
		return results{}, err
	}
	result.Probes = append(result.Probes, probe)

	probe, err = applyProbe(ctx, writer, "delete_edges", ladybug.Delta{DeleteReferences: []ladybug.ReferenceKey{{
		SourceKey: batch[2].StableKey, TargetKey: single.StableKey, Kind: ladybug.ReferenceKindReferences,
	}}})
	if err != nil {
		return results{}, err
	}
	result.Probes = append(result.Probes, probe)

	updated := single
	updated.Name = "incremental_renamed"
	updated.QualifiedName = "synthetic.incremental_renamed"
	updated.Signature = "incremental_renamed(value string)"
	updated.StartLine = 100
	updated.EndLine = 120
	probe, err = applyProbe(ctx, writer, "update_properties", ladybug.Delta{UpdateSymbols: []ladybug.Symbol{updated}})
	if err != nil {
		return results{}, err
	}
	result.Probes = append(result.Probes, probe)

	replacement := []ladybug.Reference{
		incrementalReference(updated, batch[3], ladybug.ReferenceKindCallsDirect),
		incrementalReference(updated, batch[4], ladybug.ReferenceKindReferences),
	}
	probe, err = applyProbe(ctx, writer, "replace_outgoing", ladybug.Delta{ReplaceOutgoing: []ladybug.OutgoingReplacement{{
		SourceKey: updated.StableKey, References: replacement,
	}}})
	if err != nil {
		return results{}, err
	}
	result.Probes = append(result.Probes, probe)

	probe, err = applyProbe(ctx, writer, "delete_symbol", ladybug.Delta{DeleteSymbolKeys: []string{batch[4].StableKey}})
	if err != nil {
		return results{}, err
	}
	result.Probes = append(result.Probes, probe)

	rollbackSymbol := incrementalSymbol("incremental-must-rollback", 2_000)
	rollbackReference := incrementalReference(rollbackSymbol, incrementalSymbol("incremental-missing", 2_001), ladybug.ReferenceKindReferences)
	start := time.Now()
	_, rollbackErr := writer.Apply(ctx, ladybug.Delta{
		AddSymbols:    []ladybug.Symbol{rollbackSymbol},
		AddReferences: []ladybug.Reference{rollbackReference},
	})
	rollbackDuration := time.Since(start)
	if !errors.Is(rollbackErr, ladybug.ErrNotFound) {
		return results{}, fmt.Errorf("rollback probe error = %v, want ErrNotFound", rollbackErr)
	}
	result.Probes = append(result.Probes, probeResult{
		Probe: "rollback_after_late_failure", DurationMS: milliseconds(rollbackDuration), ExpectedFailure: "ErrNotFound",
	})

	assessment, err := verifyIntegrity(ctx, writer, reader, updated, batch, replacement, rollbackSymbol)
	if err != nil {
		return results{}, err
	}
	result.Integrity = assessment
	return result, nil
}

func applyProbe(ctx context.Context, writer ladybug.Writer, name string, delta ladybug.Delta) (probeResult, error) {
	start := time.Now()
	mutation, err := writer.Apply(ctx, delta)
	duration := time.Since(start)
	if err != nil {
		return probeResult{}, fmt.Errorf("probe %s: %w", name, err)
	}
	return probeResult{Probe: name, DurationMS: milliseconds(duration), Mutation: mutation}, nil
}

func verifyIntegrity(ctx context.Context, writer ladybug.Writer, reader ladybug.Reader, updated ladybug.Symbol, batch []ladybug.Symbol, replacement []ladybug.Reference, rollbackSymbol ladybug.Symbol) (integrityAssessment, error) {
	assessment := integrityAssessment{}
	for _, symbol := range []ladybug.Symbol{updated, batch[0], batch[500], batch[len(batch)-1]} {
		persisted, found, err := reader.GetSymbol(ctx, symbol.StableKey)
		if err != nil {
			return assessment, err
		}
		if !found || !reflect.DeepEqual(persisted, symbol) {
			return assessment, fmt.Errorf("symbol %s = %#v, found=%t", symbol.StableKey, persisted, found)
		}
	}
	if _, found, err := reader.GetSymbol(ctx, batch[4].StableKey); err != nil || found {
		return assessment, fmt.Errorf("deleted symbol %s found=%t error=%v", batch[4].StableKey, found, err)
	}
	if _, found, err := reader.GetSymbol(ctx, rollbackSymbol.StableKey); err != nil || found {
		return assessment, fmt.Errorf("rolled-back symbol %s found=%t error=%v", rollbackSymbol.StableKey, found, err)
	}
	assessment.AtomicityVerified = true
	assessment.RollbackVerified = true

	outgoing, err := reader.OutgoingReferences(ctx, updated.StableKey, ladybug.MaxReferenceResults)
	if err != nil {
		return assessment, err
	}
	wantOutgoing := []ladybug.Reference{replacement[0]}
	if !reflect.DeepEqual(outgoing, wantOutgoing) {
		return assessment, fmt.Errorf("final outgoing references = %#v, want %#v", outgoing, wantOutgoing)
	}
	assessment.NoGhostEdges = true

	if _, err := writer.Apply(ctx, ladybug.Delta{AddSymbols: []ladybug.Symbol{updated}}); !errors.Is(err, ladybug.ErrAlreadyExists) {
		return assessment, fmt.Errorf("duplicate symbol error = %v, want ErrAlreadyExists", err)
	}
	assessment.DuplicateSymbolsRejected = true
	if _, err := writer.Apply(ctx, ladybug.Delta{AddReferences: []ladybug.Reference{replacement[0]}}); !errors.Is(err, ladybug.ErrAlreadyExists) {
		return assessment, fmt.Errorf("duplicate reference error = %v, want ErrAlreadyExists", err)
	}
	assessment.DuplicateReferencesRejected = true

	outgoingAfterDuplicates, err := reader.OutgoingReferences(ctx, updated.StableKey, ladybug.MaxReferenceResults)
	if err != nil {
		return assessment, err
	}
	if !reflect.DeepEqual(outgoingAfterDuplicates, wantOutgoing) {
		return assessment, fmt.Errorf("duplicates changed outgoing references: %#v", outgoingAfterDuplicates)
	}
	assessment.Passed = assessment.DuplicateSymbolsRejected && assessment.DuplicateReferencesRejected && assessment.NoGhostEdges && assessment.AtomicityVerified && assessment.RollbackVerified
	return assessment, nil
}

func incrementalSymbol(stableKey string, index int) ladybug.Symbol {
	return ladybug.Symbol{
		StableKey: stableKey, RepositoryKey: "repository-0000", FileKey: "file-00000000",
		Name: stableKey, QualifiedName: "synthetic." + stableKey, Kind: "function",
		Signature: stableKey + "()", StartLine: int64(index + 1), EndLine: int64(index + 5),
	}
}

func incrementalReference(source, target ladybug.Symbol, kind string) ladybug.Reference {
	return ladybug.Reference{
		SourceKey: source.StableKey, TargetKey: target.StableKey, Kind: kind,
		EvidenceKind: "incremental_probe", SourceFileKey: source.FileKey, TargetFileKey: target.FileKey,
	}
}

func copyFile(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		return err
	}
	return target.Close()
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
	fmt.Fprintln(&report, "# LadybugDB incremental update probes")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- Command: `%s`\n", result.Command)
	fmt.Fprintf(&report, "- Commit: `%s`\n", result.Commit)
	fmt.Fprintf(&report, "- Generated at: `%s`\n", result.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&report, "- Platform: `%s/%s`, `%s`\n", result.GOOS, result.GOARCH, result.GoVersion)
	fmt.Fprintf(&report, "- Corpus: seed %d, %d symbols, %d edges\n", result.Corpus.Seed, result.Corpus.Symbols, result.Corpus.Edges)
	fmt.Fprintf(&report, "- Base database bytes: `%d`\n", result.BaseDatabaseBytes)
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Probe | Duration ms | Added symbols | Updated symbols | Deleted symbols | Added references | Deleted references | Replaced sources | Expected failure |")
	fmt.Fprintln(&report, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |")
	for _, probe := range result.Probes {
		fmt.Fprintf(&report, "| %s | %.3f | %d | %d | %d | %d | %d | %d | %s |\n", probe.Probe, probe.DurationMS, probe.Mutation.AddedSymbols, probe.Mutation.UpdatedSymbols, probe.Mutation.DeletedSymbols, probe.Mutation.AddedReferences, probe.Mutation.DeletedReferences, probe.Mutation.ReplacedSources, probe.ExpectedFailure)
	}
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Integrity")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- Duplicate symbols rejected: `%t`\n", result.Integrity.DuplicateSymbolsRejected)
	fmt.Fprintf(&report, "- Duplicate references rejected: `%t`\n", result.Integrity.DuplicateReferencesRejected)
	fmt.Fprintf(&report, "- No ghost edges: `%t`\n", result.Integrity.NoGhostEdges)
	fmt.Fprintf(&report, "- Atomicity verified: `%t`\n", result.Integrity.AtomicityVerified)
	fmt.Fprintf(&report, "- Rollback verified: `%t`\n", result.Integrity.RollbackVerified)
	fmt.Fprintf(&report, "- Result: `%t`\n", result.Integrity.Passed)
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "The probe sequence runs against one temporary copy of the full synthetic LadybugDB database. Timings cover the transactional database mutation only; HotSnapshot construction and publication are not implemented in this phase.")
	return os.WriteFile(filepath.Join(outputDir, "report.md"), []byte(report.String()), 0o644)
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration) / float64(time.Millisecond)
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

//go:build ladybug && cgo

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/indexer"
	"github.com/Luqueee/kivgraph/internal/rebuild"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
)

const (
	defaultOutputDir      = "benchmarks/ladybug-incremental"
	defaultSamples        = 5
	defaultFiles          = 1_000
	defaultSymbolsPerFile = 10
	resolverVersion       = "incremental-benchmark-v1"
)

type config struct {
	OutputDir      string
	Samples        int
	Files          int
	SymbolsPerFile int
}

type results struct {
	Benchmark   string           `json:"benchmark"`
	Command     string           `json:"command"`
	Commit      string           `json:"commit"`
	GeneratedAt time.Time        `json:"generated_at"`
	GoVersion   string           `json:"go_version"`
	GOOS        string           `json:"goos"`
	GOARCH      string           `json:"goarch"`
	Samples     int              `json:"samples"`
	Corpus      corpusStats      `json:"corpus"`
	Scenarios   []scenarioResult `json:"scenarios"`
	Gate        gateAssessment   `json:"gate"`
	Limitations []string         `json:"limitations"`
}

type corpusStats struct {
	Seed         int64 `json:"seed"`
	Repositories int   `json:"repositories"`
	Packages     int   `json:"packages"`
	Files        int   `json:"files"`
	Symbols      int   `json:"symbols"`
	Evidence     int   `json:"evidence"`
	Edges        int   `json:"edges"`
}

type scenarioResult struct {
	Scenario      string              `json:"scenario"`
	ExpectedRoute indexer.Route       `json:"expected_route"`
	Samples       []sampleResult      `json:"samples"`
	Summary       summary             `json:"summary"`
	Integrity     integrityAssessment `json:"integrity"`
}

type sampleResult struct {
	Sample    int                             `json:"sample"`
	SetupMS   float64                         `json:"baseline_setup_ms"`
	UpdateMS  float64                         `json:"update_publish_ms"`
	Route     indexer.Route                   `json:"route"`
	Mutation  ladybug.CanonicalMutationResult `json:"mutation,omitempty"`
	Integrity integrityAssessment             `json:"integrity"`
}

type summary struct {
	P50MS float64 `json:"p50_ms"`
	P95MS float64 `json:"p95_ms"`
	MinMS float64 `json:"min_ms"`
	MaxMS float64 `json:"max_ms"`
}

type integrityAssessment struct {
	Passed     bool  `json:"passed"`
	Violations int64 `json:"violations"`
}

type gateAssessment struct {
	SimpleFileP95MS       float64 `json:"simple_file_p95_ms"`
	ImportsExportsP95MS   float64 `json:"imports_exports_p95_ms"`
	ManifestP95MS         float64 `json:"manifest_p95_ms"`
	SimpleFileWithin750MS bool    `json:"simple_file_within_750_ms"`
	ImportsWithin2S       bool    `json:"imports_exports_within_2_s"`
	ManifestWithin5S      bool    `json:"manifest_within_5_s"`
	NoGhostEdges          bool    `json:"no_ghost_edges"`
	Passed                bool    `json:"passed"`
}

type scenario struct {
	Name          string
	ExpectedRoute indexer.Route
	Plan          indexer.InvalidationPlan
	Mutate        func(facts.Set) facts.Set
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.OutputDir, "output", defaultOutputDir, "results output directory")
	flag.IntVar(&cfg.Samples, "samples", defaultSamples, "independent samples per scenario")
	flag.IntVar(&cfg.Files, "files", defaultFiles, "files in the deterministic benchmark corpus")
	flag.IntVar(&cfg.SymbolsPerFile, "symbols-per-file", defaultSymbolsPerFile, "symbols per source file")
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
	fmt.Printf("simple file p95: %.1f ms; imports/exports p95: %.1f ms; manifest p95: %.1f ms; gate: %t\n",
		result.Gate.SimpleFileP95MS, result.Gate.ImportsExportsP95MS, result.Gate.ManifestP95MS, result.Gate.Passed)
	if !result.Gate.Passed {
		fmt.Fprintln(os.Stderr, "INCREMENTAL_INDEXING_PASS not emitted; see results.json and report.md")
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) (results, error) {
	if cfg.Samples < 1 {
		return results{}, errors.New("samples must be positive")
	}
	if cfg.Files < 1 {
		return results{}, errors.New("files must be positive")
	}
	if cfg.SymbolsPerFile < 2 {
		return results{}, errors.New("symbols-per-file must be at least 2")
	}

	base := benchmarkCorpus(cfg.Files, cfg.SymbolsPerFile)
	if err := base.Validate(); err != nil {
		return results{}, fmt.Errorf("validate benchmark corpus: %w", err)
	}
	stats := corpusStats{
		Seed: 42, Repositories: len(base.Repositories), Packages: len(base.Packages), Files: len(base.Files),
		Symbols: len(base.Symbols), Evidence: len(base.Evidence), Edges: len(base.Edges),
	}
	result := results{
		Benchmark:   "ladybug-incremental",
		Command:     strings.Join(os.Args, " "),
		Commit:      gitState(),
		GeneratedAt: time.Now().UTC(),
		GoVersion:   runtime.Version(), GOOS: runtime.GOOS, GOARCH: runtime.GOARCH,
		Samples: cfg.Samples, Corpus: stats,
		Limitations: []string{
			"The corpus is deterministic and generated in memory; source parsing and language-engine normalization are not part of this executable because indexer.Update accepts canonical facts after those stages.",
			"Each sample builds an isolated baseline generation before timing one complete Update call. Baseline construction is reported separately and is excluded from the update latency gate.",
			"DELTA samples include canonical mutation, row-count digest refresh, complete HotSnapshot rebuild and atomic in-memory publication. The manifest sample measures the forced full REPUBLISH path.",
			"Results describe the pinned LadybugDB build, Linux amd64 and the configured corpus size; they are not a guarantee for another filesystem or repository shape.",
		},
	}
	for _, scenario := range benchmarkScenarios(base) {
		measured := scenarioResult{Scenario: scenario.Name, ExpectedRoute: scenario.ExpectedRoute}
		for sampleIndex := 1; sampleIndex <= cfg.Samples; sampleIndex++ {
			measurement, err := measureScenario(ctx, base, scenario, sampleIndex)
			if err != nil {
				return results{}, fmt.Errorf("measure %s sample %d: %w", scenario.Name, sampleIndex, err)
			}
			measured.Samples = append(measured.Samples, measurement)
		}
		measured.Summary = summarizeSamples(measured.Samples)
		measured.Integrity = summarizeIntegrity(measured.Samples)
		result.Scenarios = append(result.Scenarios, measured)
	}
	result.Gate = assessGate(result.Scenarios)
	return result, nil
}

func measureScenario(ctx context.Context, base facts.Set, scenario scenario, sample int) (sampleResult, error) {
	root, err := os.MkdirTemp("", "kivgraph-incremental-benchmark-")
	if err != nil {
		return sampleResult{}, err
	}
	defer os.RemoveAll(root)

	setupStart := time.Now()
	if _, err := rebuild.Run(ctx, rebuild.Options{
		Root: root, GenerationID: "000001", Facts: base, ResolverVersion: resolverVersion, SnapshotID: 1,
		Store: generation.DefaultConfig(),
	}); err != nil {
		return sampleResult{}, fmt.Errorf("build baseline: %w", err)
	}
	setupMS := milliseconds(time.Since(setupStart))

	next := scenario.Mutate(cloneSet(base))
	if err := next.Validate(); err != nil {
		return sampleResult{}, fmt.Errorf("validate next facts: %w", err)
	}
	start := time.Now()
	update, err := indexer.Update(ctx, indexer.UpdateOptions{
		Root: root, Store: generation.DefaultConfig(), Plans: []indexer.InvalidationPlan{scenario.Plan},
		Previous: base, Next: next, GenerationID: "000002", ResolverVersion: resolverVersion,
		SnapshotID: 2, SnapshotStore: hotsnapshot.NewSnapshotStore(nil),
	})
	updateMS := milliseconds(time.Since(start))
	if err != nil {
		return sampleResult{}, fmt.Errorf("update: %w", err)
	}
	if update.Decision.Route != scenario.ExpectedRoute {
		return sampleResult{}, fmt.Errorf("route = %s, want %s", update.Decision.Route, scenario.ExpectedRoute)
	}
	if !update.Passed {
		return sampleResult{}, errors.New("update returned Passed=false")
	}
	if update.Generation.DatabasePath == "" {
		return sampleResult{}, errors.New("update returned no serving database")
	}

	integrity, err := ladybug.VerifyCanonicalIntegrity(ctx, update.Generation.DatabasePath)
	if err != nil {
		return sampleResult{}, fmt.Errorf("verify integrity: %w", err)
	}
	assessment := integrityAssessment{Passed: integrity.Passed, Violations: integrity.Violations()}
	if !assessment.Passed || assessment.Violations != 0 {
		return sampleResult{}, fmt.Errorf("integrity failed: passed=%t violations=%d", assessment.Passed, assessment.Violations)
	}
	return sampleResult{
		Sample: sample, SetupMS: setupMS, UpdateMS: updateMS, Route: update.Decision.Route,
		Mutation: update.Mutation, Integrity: assessment,
	}, nil
}

func benchmarkScenarios(base facts.Set) []scenario {
	repositoryKey := base.Repositories[0].Key
	packageKey := base.Packages[0].Key
	bodyFile := base.Files[1]
	importFile := base.Files[2]
	manifestFile := base.Files[0]
	return []scenario{
		{
			Name: "simple_file", ExpectedRoute: indexer.RouteDelta,
			Plan: indexer.InvalidationPlan{
				Language: facts.LanguageTypeScript, RepositoryKey: repositoryKey, PackageKey: packageKey,
				FileKey: bodyFile.Key, Path: bodyFile.Path, Class: indexer.ChangeBodyOnly,
				Actions: []indexer.InvalidationAction{indexer.ActionReindexFile},
			},
			Mutate: func(set facts.Set) facts.Set {
				for index := range set.Files {
					if set.Files[index].Key == bodyFile.Key {
						set.Files[index].ContentHash = "body-v2"
					}
				}
				set.Sort()
				return set
			},
		},
		{
			Name: "imports_exports", ExpectedRoute: indexer.RouteDelta,
			Plan: indexer.InvalidationPlan{
				Language: facts.LanguageTypeScript, RepositoryKey: repositoryKey, PackageKey: packageKey,
				FileKey: importFile.Key, Path: importFile.Path, Class: indexer.ChangeImportsChanged,
				Actions: []indexer.InvalidationAction{indexer.ActionReindexFile, indexer.ActionInvalidateModuleResolution, indexer.ActionResolveReferences},
			},
			Mutate: func(set facts.Set) facts.Set {
				source := symbolForFile(set, importFile.Key, 0)
				target := symbolForFile(set, base.Files[3].Key, 0)
				importEvidence := benchmarkEvidence(importFile.Key, 700_001, "import/export")
				exportEvidence := benchmarkEvidence(importFile.Key, 700_002, "import/export")
				set.Evidence = append(set.Evidence, importEvidence, exportEvidence)
				set.Edges = append(set.Edges,
					facts.Edge{Kind: facts.ImportsSymbol, SourceKey: source.Key, TargetKey: target.Key, Confidence: facts.ExactTypechecked, Provenance: facts.TypeScriptChecker, EvidenceKey: importEvidence.Key},
					facts.Edge{Kind: facts.Exports, SourceKey: source.Key, TargetKey: target.Key, Confidence: facts.ExactTypechecked, Provenance: facts.TypeScriptChecker, EvidenceKey: exportEvidence.Key},
				)
				set.Sort()
				return set
			},
		},
		{
			Name: "manifest", ExpectedRoute: indexer.RouteRepublish,
			Plan: indexer.InvalidationPlan{
				Language: facts.LanguageTypeScript, RepositoryKey: repositoryKey, PackageKey: packageKey,
				FileKey: manifestFile.Key, Path: manifestFile.Path, Class: indexer.ChangeManifestChanged,
				Actions: []indexer.InvalidationAction{indexer.ActionRebuildRegistry, indexer.ActionInvalidateModuleResolution, indexer.ActionReindexProject},
			},
			Mutate: func(set facts.Set) facts.Set {
				for index := range set.Packages {
					if set.Packages[index].Key == packageKey {
						set.Packages[index].Version = "2.0.0"
					}
				}
				for index := range set.Files {
					if set.Files[index].Key == manifestFile.Key {
						set.Files[index].ContentHash = "manifest-v2"
					}
				}
				set.Sort()
				return set
			},
		},
	}
}

func benchmarkCorpus(fileCount, symbolsPerFile int) facts.Set {
	const repoName = "benchmark/incremental"
	repositoryKey := facts.RepositoryKey(repoName)
	packageKey := facts.PackageKey(facts.LanguageTypeScript, repositoryKey, "incremental")
	set := facts.Set{
		Repositories: []facts.Repository{{Key: repositoryKey, Name: repoName, RootPath: "/synthetic/benchmark/incremental", Branch: "main", Languages: []facts.Language{facts.LanguageTypeScript}}},
		Packages:     []facts.Package{{Key: packageKey, RepositoryKey: repositoryKey, Language: facts.LanguageTypeScript, Name: "incremental", Version: "1.0.0", RootPath: "/synthetic/benchmark/incremental", ManifestPath: "package.json"}},
	}
	set.Edges = append(set.Edges, facts.Edge{Kind: facts.ContainsPackage, SourceKey: repositoryKey, TargetKey: packageKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest})
	totalSymbols := fileCount * symbolsPerFile
	for fileIndex := 0; fileIndex < fileCount; fileIndex++ {
		path := fmt.Sprintf("src/file-%05d.ts", fileIndex)
		if fileIndex == 0 {
			path = "package.json"
		}
		fileKey := facts.FileKey(repositoryKey, path)
		set.Files = append(set.Files, facts.File{Key: fileKey, RepositoryKey: repositoryKey, PackageKey: packageKey, Path: path, Language: facts.LanguageTypeScript, ContentHash: fmt.Sprintf("file-%05d-v1", fileIndex)})
		set.Edges = append(set.Edges, facts.Edge{Kind: facts.ContainsFile, SourceKey: packageKey, TargetKey: fileKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest})
		for symbolIndex := 0; symbolIndex < symbolsPerFile; symbolIndex++ {
			global := fileIndex*symbolsPerFile + symbolIndex
			name := fmt.Sprintf("symbol_%05d_%02d", fileIndex, symbolIndex)
			symbolKey := fmt.Sprintf("symbol:typescript:%s:%s", repositoryKey, name)
			set.Symbols = append(set.Symbols, facts.Symbol{
				Key: symbolKey, CanonicalIdentity: symbolKey, RepositoryKey: repositoryKey, PackageKey: packageKey, FileKey: fileKey,
				Language: facts.LanguageTypeScript, Name: name, QualifiedName: "incremental." + name, Kind: "function", Exported: symbolIndex == 0,
				Signature: name + "()", Start: facts.Position{Line: symbolIndex*4 + 1, Offset: global * 20}, End: facts.Position{Line: symbolIndex*4 + 3, Offset: global*20 + 18},
			})
			set.Edges = append(set.Edges, facts.Edge{Kind: facts.Defines, SourceKey: fileKey, TargetKey: symbolKey, Confidence: facts.StructuralCertain, Provenance: facts.TypeScriptChecker})
		}
	}
	for global := 0; global < totalSymbols; global++ {
		source := set.Symbols[global].Key
		target := set.Symbols[(global+1)%totalSymbols].Key
		sourceFile := set.Symbols[global].FileKey
		evidence := benchmarkEvidence(sourceFile, global+1, "calls")
		set.Evidence = append(set.Evidence, evidence)
		set.Edges = append(set.Edges, facts.Edge{Kind: facts.CallsDirect, SourceKey: source, TargetKey: target, Confidence: facts.ExactTypechecked, Provenance: facts.TypeScriptChecker, EvidenceKey: evidence.Key})
	}
	set.Sort()
	return set
}

func benchmarkEvidence(fileKey string, offset int, text string) facts.Evidence {
	return facts.Evidence{Key: facts.EvidenceKey(fileKey, offset, offset+len(text)), RepositoryKey: repositoryFromFileKey(fileKey), FileKey: fileKey, Start: facts.Position{Line: offset, Offset: offset}, End: facts.Position{Line: offset, Offset: offset + len(text)}, Text: text}
}

func repositoryFromFileKey(_ string) string {
	return "repository:benchmark/incremental"
}

func symbolForFile(set facts.Set, fileKey string, index int) facts.Symbol {
	matches := make([]facts.Symbol, 0, 1)
	for _, symbol := range set.Symbols {
		if symbol.FileKey == fileKey {
			matches = append(matches, symbol)
		}
	}
	sort.Slice(matches, func(left, right int) bool { return matches[left].Key < matches[right].Key })
	return matches[index%len(matches)]
}

func cloneSet(set facts.Set) facts.Set {
	return facts.Set{
		Repositories: append([]facts.Repository(nil), set.Repositories...), Packages: append([]facts.Package(nil), set.Packages...),
		Files: append([]facts.File(nil), set.Files...), Symbols: append([]facts.Symbol(nil), set.Symbols...),
		Evidence: append([]facts.Evidence(nil), set.Evidence...), Edges: append([]facts.Edge(nil), set.Edges...),
		Unresolved: append([]facts.UnresolvedReference(nil), set.Unresolved...),
	}
}

func summarizeSamples(samples []sampleResult) summary {
	values := make([]float64, len(samples))
	for index, sample := range samples {
		values[index] = sample.UpdateMS
	}
	sortedValues := append([]float64(nil), values...)
	sort.Float64s(sortedValues)
	return summary{P50MS: percentile(values, 0.50), P95MS: percentile(values, 0.95), MinMS: sortedValues[0], MaxMS: sortedValues[len(sortedValues)-1]}
}

func summarizeIntegrity(samples []sampleResult) integrityAssessment {
	assessment := integrityAssessment{Passed: true}
	for _, sample := range samples {
		assessment.Violations += sample.Integrity.Violations
		assessment.Passed = assessment.Passed && sample.Integrity.Passed && sample.Integrity.Violations == 0
	}
	return assessment
}

func percentile(values []float64, quantile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	copied := append([]float64(nil), values...)
	sort.Float64s(copied)
	index := int(float64(len(copied)-1)*quantile + 0.999999)
	return copied[index]
}

func assessGate(scenarios []scenarioResult) gateAssessment {
	gate := gateAssessment{}
	for _, scenario := range scenarios {
		switch scenario.Scenario {
		case "simple_file":
			gate.SimpleFileP95MS = scenario.Summary.P95MS
		case "imports_exports":
			gate.ImportsExportsP95MS = scenario.Summary.P95MS
		case "manifest":
			gate.ManifestP95MS = scenario.Summary.P95MS
		}
		gate.NoGhostEdges = gate.NoGhostEdges || scenario.Integrity.Passed
	}
	gate.SimpleFileWithin750MS = gate.SimpleFileP95MS > 0 && gate.SimpleFileP95MS <= 750
	gate.ImportsWithin2S = gate.ImportsExportsP95MS > 0 && gate.ImportsExportsP95MS <= 2_000
	gate.ManifestWithin5S = gate.ManifestP95MS > 0 && gate.ManifestP95MS <= 5_000
	gate.NoGhostEdges = len(scenarios) == 3 && gate.NoGhostEdges
	for _, scenario := range scenarios {
		gate.NoGhostEdges = gate.NoGhostEdges && scenario.Integrity.Passed && scenario.Integrity.Violations == 0
	}
	gate.Passed = gate.SimpleFileWithin750MS && gate.ImportsWithin2S && gate.ManifestWithin5S && gate.NoGhostEdges
	return gate
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
	fmt.Fprintln(&report, "# Benchmark de indexación incremental")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- Commit medido: `%s`\n", result.Commit)
	fmt.Fprintf(&report, "- Fecha: `%s`\n", result.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&report, "- Plataforma: `%s/%s`, `%s`\n", result.GOOS, result.GOARCH, result.GoVersion)
	fmt.Fprintf(&report, "- Muestras por escenario: `%d`\n", result.Samples)
	fmt.Fprintf(&report, "- Corpus: `%d` repositorio, `%d` paquete, `%d` archivos, `%d` símbolos, `%d` evidencias, `%d` aristas\n", result.Corpus.Repositories, result.Corpus.Packages, result.Corpus.Files, result.Corpus.Symbols, result.Corpus.Evidence, result.Corpus.Edges)
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Resultados")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "Los tiempos son la llamada completa a `indexer.Update`: cálculo del delta, decisión de ruta, mutación o republicación, digest, reconstrucción del HotSnapshot cuando corresponde y publicación atómica.")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Escenario | Ruta | p50 ms | p95 ms | mínimo ms | máximo ms | setup base p50 ms | integridad |")
	fmt.Fprintln(&report, "| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |")
	for _, scenario := range result.Scenarios {
		setupValues := make([]float64, len(scenario.Samples))
		for index, sample := range scenario.Samples {
			setupValues[index] = sample.SetupMS
		}
		fmt.Fprintf(&report, "| `%s` | `%s` | %.1f | %.1f | %.1f | %.1f | %.1f | `%t`, %d violaciones |\n",
			scenario.Scenario, scenario.ExpectedRoute, scenario.Summary.P50MS, scenario.Summary.P95MS, scenario.Summary.MinMS, scenario.Summary.MaxMS,
			percentile(setupValues, 0.50), scenario.Integrity.Passed, scenario.Integrity.Violations)
	}
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Gate `INCREMENTAL_INDEXING_PASS`")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- archivo simple p95: `%.1f ms` (límite `≤ 750 ms`) — `%t`\n", result.Gate.SimpleFileP95MS, result.Gate.SimpleFileWithin750MS)
	fmt.Fprintf(&report, "- imports/exports p95: `%.1f ms` (límite `≤ 2 s`) — `%t`\n", result.Gate.ImportsExportsP95MS, result.Gate.ImportsWithin2S)
	fmt.Fprintf(&report, "- manifest p95: `%.1f ms` (límite `≤ 5 s`) — `%t`\n", result.Gate.ManifestP95MS, result.Gate.ManifestWithin5S)
	fmt.Fprintf(&report, "- ghost edges: `0` — `%t`\n", result.Gate.NoGhostEdges)
	fmt.Fprintf(&report, "- Resultado: `%t`\n", result.Gate.Passed)
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Limitaciones")
	fmt.Fprintln(&report)
	for _, limitation := range result.Limitations {
		fmt.Fprintf(&report, "- %s\n", limitation)
	}
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
	if err := exec.Command("git", "diff", "--quiet").Run(); err != nil {
		return commit + "-dirty"
	}
	return commit
}

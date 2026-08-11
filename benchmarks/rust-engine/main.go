// Command rust-engine measures what the Rust path costs and how that cost is
// split between the external analyzer and Ladygraph's own work.
//
// The two numbers are reported separately on purpose: `rust-analyzer` is the
// dominant term, and a total that hides it would read as if the normalisation
// were slow.
//
// Unlike the exactness audit, this harness records timings, so its artifacts
// are not byte identical between runs. Every figure carries the environment it
// was measured in.
//
// Usage:
//
//	go run ./benchmarks/rust-engine
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/Luqueee/ladygraph/internal/indexer"
	"github.com/Luqueee/ladygraph/internal/rustloader"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

const (
	fixtureRoot     = "testdata/rust/cross-repository"
	outputDirectory = "benchmarks/rust-engine"
	command         = "go run ./benchmarks/rust-engine"
)

type environment struct {
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	CPUs            int    `json:"cpus"`
	Go              string `json:"go"`
	AnalyzerVersion string `json:"rust_analyzer_version"`
	CargoVersion    string `json:"cargo_version"`
}

type measurement struct {
	Name         string  `json:"name"`
	Milliseconds float64 `json:"milliseconds"`
	Detail       string  `json:"detail,omitempty"`
}

type report struct {
	Command      string        `json:"command"`
	Corpus       string        `json:"corpus"`
	Repositories int           `json:"repositories"`
	Crates       int           `json:"crates"`
	Symbols      int           `json:"symbols"`
	Edges        int           `json:"edges"`
	Environment  environment   `json:"environment"`
	Measurements []measurement `json:"measurements"`
	Limitations  []string      `json:"limitations"`
}

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "rust-engine: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	if _, err := exec.LookPath("rust-analyzer"); err != nil {
		return fmt.Errorf("rust-analyzer is required to measure the Rust path: %w", err)
	}
	root, err := os.MkdirTemp("", "ladygraph-rust-bench-*")
	if err != nil {
		return fmt.Errorf("create benchmark directory: %w", err)
	}
	defer os.RemoveAll(root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	if err := os.CopyFS(root, os.DirFS(fixtureRoot)); err != nil {
		return fmt.Errorf("copy corpus: %w", err)
	}
	repositories := []workspace.Repository{
		{Name: "provider", Path: filepath.Join(root, "provider"), RealPath: filepath.Join(root, "provider"), Languages: []string{"rust"}},
		{Name: "consumer", Path: filepath.Join(root, "consumer"), RealPath: filepath.Join(root, "consumer"), Languages: []string{"rust"}},
	}

	measured := report{
		Command:      command,
		Corpus:       fixtureRoot,
		Repositories: len(repositories),
		Environment:  collectEnvironment(),
		Limitations: []string{
			"El corpus son tres crates: mide el reparto del coste, no la escala.",
			"El caché del toolchain y del sysroot están calientes; una máquina fría paga más.",
			"Las cifras dependen de la versión de rust-analyzer instalada.",
		},
	}

	// The analyzer alone, over the larger of the two workspaces.
	analyzerOutput := filepath.Join(root, "analyzer-output")
	if err := os.MkdirAll(analyzerOutput, 0o700); err != nil {
		return fmt.Errorf("create analyzer output: %w", err)
	}
	analyzerStart := time.Now()
	result, err := rustloader.Run(ctx, rustloader.RunOptions{
		Workspace:       filepath.Join(root, "provider"),
		OutputDirectory: analyzerOutput,
		AnalyzerCommand: "rust-analyzer",
		TargetDirectory: filepath.Join(root, "target"),
		BuildScripts:    true,
		ProcMacros:      true,
		Sysroot:         "discover",
	})
	if err != nil {
		return fmt.Errorf("run the analyzer: %w", err)
	}
	measured.Measurements = append(measured.Measurements, measurement{
		Name:         "analyzer.provider",
		Milliseconds: milliseconds(time.Since(analyzerStart)),
		Detail:       fmt.Sprintf("documents=%d", len(result.Index.Documents)),
	})

	cacheDirectory := filepath.Join(root, "factcache")
	coldStart := time.Now()
	set, indexReport, err := indexer.Full(ctx, fullOptions(repositories, root, cacheDirectory, indexer.CacheOn))
	if err != nil {
		return fmt.Errorf("cold index: %w", err)
	}
	measured.Measurements = append(measured.Measurements, measurement{
		Name:         "index.cold",
		Milliseconds: milliseconds(time.Since(coldStart)),
		Detail:       fmt.Sprintf("workspaces=%d", indexReport.RustWorkspaces),
	})
	measured.Crates = indexReport.RustCrates
	measured.Symbols = len(set.Symbols)
	measured.Edges = len(set.Edges)

	warmStart := time.Now()
	warmSet, warmReport, err := indexer.Full(ctx, fullOptions(repositories, root, cacheDirectory, indexer.CacheOn))
	if err != nil {
		return fmt.Errorf("warm index: %w", err)
	}
	measured.Measurements = append(measured.Measurements, measurement{
		Name:         "index.warm",
		Milliseconds: milliseconds(time.Since(warmStart)),
		Detail:       fmt.Sprintf("cache_hits=%d misses=%d", warmReport.Cache.Hits, warmReport.Cache.Misses),
	})
	if len(warmSet.Symbols) != len(set.Symbols) || len(warmSet.Edges) != len(set.Edges) {
		return fmt.Errorf("the warm pass published a different graph: %d/%d symbols, %d/%d edges",
			len(warmSet.Symbols), len(set.Symbols), len(warmSet.Edges), len(set.Edges))
	}

	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return fmt.Errorf("create %q: %w", outputDirectory, err)
	}
	encoded, err := json.MarshalIndent(measured, "", "  ")
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDirectory, "results.json"), append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write results: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDirectory, "report.md"), []byte(render(measured)), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	fmt.Println("RUST_ENGINE_MEASURED")
	return nil
}

func fullOptions(repositories []workspace.Repository, root, cache string, mode indexer.CacheMode) indexer.FullOptions {
	return indexer.FullOptions{
		Repositories:        repositories,
		RustAnalyzer:        "rust-analyzer",
		RustTargetDirectory: filepath.Join(root, "target"),
		RustBuildScripts:    true,
		RustProcMacros:      true,
		RustSysroot:         "discover",
		WorkingDirectory:    root,
		CacheMode:           mode,
		CacheDirectory:      cache,
	}
}

func collectEnvironment() environment {
	return environment{
		OS:              runtime.GOOS,
		Arch:            runtime.GOARCH,
		CPUs:            runtime.NumCPU(),
		Go:              runtime.Version(),
		AnalyzerVersion: commandVersion("rust-analyzer"),
		CargoVersion:    commandVersion("cargo"),
	}
}

func commandVersion(name string) string {
	output, err := exec.Command(name, "--version").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(output))
}

func milliseconds(duration time.Duration) float64 {
	return float64(duration.Microseconds()) / 1000
}

func render(measured report) string {
	var out strings.Builder
	out.WriteString("# Coste del motor Rust\n\n")
	out.WriteString("Medición de LUQUE-1816. Se regenera con `" + measured.Command + "`.\n\n")
	out.WriteString("## Corpus\n\n")
	fmt.Fprintf(&out, "- `%s`: %d repositorios, %d crates, %d símbolos, %d aristas.\n\n",
		measured.Corpus, measured.Repositories, measured.Crates, measured.Symbols, measured.Edges)
	out.WriteString("## Entorno\n\n")
	fmt.Fprintf(&out, "- %s/%s, %d CPUs, %s\n", measured.Environment.OS, measured.Environment.Arch,
		measured.Environment.CPUs, measured.Environment.Go)
	fmt.Fprintf(&out, "- `%s`\n- `%s`\n\n", measured.Environment.AnalyzerVersion, measured.Environment.CargoVersion)
	out.WriteString("## Medidas\n\n")
	out.WriteString("| Medida | ms | Detalle |\n| --- | --- | --- |\n")
	for _, entry := range measured.Measurements {
		fmt.Fprintf(&out, "| %s | %.1f | %s |\n", entry.Name, entry.Milliseconds, entry.Detail)
	}
	out.WriteString("\nEl analizador externo es el término dominante: la normalización, la\n")
	out.WriteString("atribución con Tree-sitter y el merge ocurren sobre un índice ya construido.\n")
	out.WriteString("\n## Limitaciones\n\n")
	for _, limitation := range measured.Limitations {
		out.WriteString("- " + limitation + "\n")
	}
	return out.String()
}

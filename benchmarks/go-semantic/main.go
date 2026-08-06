// Command go-semantic measures the precision of Go cross-repository
// resolution over the fixtures of LUQUE-0811 and LUQUE-0812.
//
// The artifacts are deterministic: no timestamps and no machine paths, so a
// rerun on another host produces byte identical files and a regression is
// visible in the diff.
//
// Usage:
//
//	go run ./benchmarks/go-semantic
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Luqueee/ladygraph/internal/goloader"
)

const (
	fixtureRoot     = "testdata/go"
	outputDirectory = "benchmarks/go-semantic"
	command         = "go run ./benchmarks/go-semantic"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintf(os.Stderr, "go-semantic: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	report, err := goloader.MeasurePrecision(ctx, fixtureRoot)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outputDirectory, 0o755); err != nil {
		return fmt.Errorf("create %q: %w", outputDirectory, err)
	}

	encoded, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("encode results: %w", err)
	}
	resultsPath := filepath.Join(outputDirectory, "results.json")
	if err := os.WriteFile(resultsPath, append(encoded, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", resultsPath, err)
	}
	reportPath := filepath.Join(outputDirectory, "report.md")
	if err := os.WriteFile(reportPath, []byte(render(report)), 0o644); err != nil {
		return fmt.Errorf("write %q: %w", reportPath, err)
	}

	fmt.Println(report.Gate)
	if report.Gate != goloader.PrecisionGate {
		return fmt.Errorf("gate not met: %s", report.Gate)
	}
	return nil
}

func render(report goloader.PrecisionReport) string {
	lines := []string{
		"# Precisión semántica Go",
		"",
		"Medición de LUQUE-0813 sobre los fixtures de LUQUE-0811 y LUQUE-0812.",
		"Se regenera con `" + command + "`.",
		"",
		"## Fixtures",
		"",
	}
	for _, fixture := range report.Fixtures {
		lines = append(lines, "- `"+filepath.ToSlash(fixture)+"`")
	}
	lines = append(lines, "", "## Totales", "")
	for _, line := range goloader.PrecisionSummary(report.Totals) {
		lines = append(lines, "- "+line)
	}
	lines = append(lines,
		"",
		"## Casos",
		"",
		"| Caso | Aristas esperadas | TP | FP | FN | Precisión | Recall | No resueltas correctas |",
		"| --- | --- | --- | --- | --- | --- | --- | --- |",
	)
	for _, entry := range report.Cases {
		lines = append(lines, goloader.PrecisionCaseRow(entry))
	}
	lines = append(lines, "", "## Gate", "", "```text", report.Gate, "```", "")
	return strings.Join(lines, "\n")
}

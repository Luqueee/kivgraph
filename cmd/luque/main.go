package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/Luqueee/luque/internal/facts"
	mcpserver "github.com/Luqueee/luque/internal/mcp"
	"github.com/Luqueee/luque/internal/rebuild"
	"github.com/Luqueee/luque/internal/storage/generation"
	"github.com/Luqueee/luque/internal/storage/ladybug"
	"github.com/Luqueee/luque/internal/synthetic"
	"github.com/Luqueee/luque/internal/version"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "serve" {
		if err := mcpserver.Run(context.Background()); err != nil {
			fmt.Fprintf(os.Stderr, "serve: %v\n", err)
			os.Exit(1)
		}
		return
	}

	os.Exit(run(os.Args, os.Stdout, os.Stderr))
}

type storageDiagnoser func(context.Context, string) (ladybug.StorageDiagnosis, error)

type graphRebuilder func(context.Context, rebuild.Options) (rebuild.Report, error)

func run(args []string, stdout, stderr io.Writer) int {
	return runWithStorageDiagnoser(args, stdout, stderr, ladybug.DiagnoseStorage)
}

func runWithStorageDiagnoser(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser) int {
	return runWithGraphRebuilder(args, stdout, stderr, diagnose, rebuild.Run)
}

func runWithGraphRebuilder(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser, rebuilder graphRebuilder) int {
	if len(args) == 2 && args[1] == "version" {
		fmt.Fprintln(stdout, version.Value)
		return 0
	}
	if len(args) >= 3 && args[1] == "doctor" && args[2] == "storage" {
		return runDoctorStorage(args[3:], stdout, stderr, diagnose)
	}
	if len(args) >= 3 && args[1] == "benchmark" && args[2] == "generate-graph" {
		return runGenerateGraph(args[3:], stdout, stderr)
	}
	if len(args) >= 2 && args[1] == "rebuild" {
		return runRebuild(args[2:], stdout, stderr, rebuilder)
	}

	fmt.Fprintf(stderr, "usage: %s version|serve|doctor storage --database PATH|benchmark generate-graph [flags]|rebuild --facts PATH --root PATH --generation ID --resolver-version STRING [flags]\n", args[0])
	return 2
}

func runDoctorStorage(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser) int {
	flags := flag.NewFlagSet("doctor storage", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var databasePath string
	flags.StringVar(&databasePath, "database", "", "existing LadybugDB database path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "doctor storage: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	if databasePath == "" {
		fmt.Fprintln(stderr, "doctor storage: --database is required")
		return 2
	}

	diagnosis, err := diagnose(context.Background(), databasePath)
	if err != nil {
		fmt.Fprintf(stderr, "doctor storage: %v\n", err)
		return 1
	}
	state := "FAIL"
	if diagnosis.Healthy {
		state = "PASS"
	}
	fmt.Fprintf(stdout, "storage doctor: %s\n", state)
	fmt.Fprintf(stdout, "database: %s\n", diagnosis.Path)
	for _, check := range diagnosis.Checks {
		fmt.Fprintf(stdout, "[%s] %s: %s\n", check.Status, check.Name, check.Detail)
	}
	if diagnosis.Healthy {
		return 0
	}
	return 1
}

func runGenerateGraph(args []string, stdout, stderr io.Writer) int {
	config := synthetic.DefaultConfig()
	flags := flag.NewFlagSet("generate-graph", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.IntVar(&config.Repositories, "repositories", config.Repositories, "number of repositories")
	flags.IntVar(&config.Files, "files", config.Files, "number of files")
	flags.IntVar(&config.Symbols, "symbols", config.Symbols, "number of symbols")
	flags.IntVar(&config.Edges, "edges", config.Edges, "number of total edges")
	flags.Int64Var(&config.Seed, "seed", config.Seed, "deterministic corpus seed")
	flags.StringVar(&config.OutputDir, "output", config.OutputDir, "output directory")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "generate-graph: unexpected arguments: %v\n", flags.Args())
		return 2
	}

	manifest, err := synthetic.Generate(context.Background(), config)
	if err != nil {
		fmt.Fprintf(stderr, "generate-graph: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "generated %d repositories, %d files, %d symbols, %d edges at %s (seed %d)\n",
		manifest.Repositories,
		manifest.Files,
		manifest.Symbols,
		manifest.Edges,
		config.OutputDir,
		manifest.Seed,
	)
	return 0
}

func runRebuild(args []string, stdout, stderr io.Writer, rebuilder graphRebuilder) int {
	flags := flag.NewFlagSet("rebuild", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var (
		factsPath       string
		root            string
		generationID    string
		resolverVersion string
		snapshotID      int64
	)
	flags.StringVar(&factsPath, "facts", "", "JSON file with a serialized facts.Set")
	flags.StringVar(&root, "root", "", "generation store root directory")
	flags.StringVar(&generationID, "generation", "", "six digit generation id to publish")
	flags.StringVar(&resolverVersion, "resolver-version", "", "resolver version stamped on every semantic edge")
	flags.Int64Var(&snapshotID, "snapshot-id", 0, "snapshot id stamped on every semantic edge")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "rebuild: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	switch {
	case factsPath == "":
		fmt.Fprintln(stderr, "rebuild: --facts is required")
		return 2
	case root == "":
		fmt.Fprintln(stderr, "rebuild: --root is required")
		return 2
	case generationID == "":
		fmt.Fprintln(stderr, "rebuild: --generation is required")
		return 2
	case resolverVersion == "":
		fmt.Fprintln(stderr, "rebuild: --resolver-version is required")
		return 2
	}

	factsData, err := os.ReadFile(factsPath)
	if err != nil {
		fmt.Fprintf(stderr, "rebuild: read facts: %v\n", err)
		return 1
	}
	var set facts.Set
	if err := json.Unmarshal(factsData, &set); err != nil {
		fmt.Fprintf(stderr, "rebuild: decode facts: %v\n", err)
		return 1
	}

	report, err := rebuilder(context.Background(), rebuild.Options{
		Root:            root,
		GenerationID:    generationID,
		Facts:           set,
		ResolverVersion: resolverVersion,
		SnapshotID:      snapshotID,
		Store:           generation.DefaultConfig(),
	})
	writeRebuildReport(stdout, report)
	if err != nil {
		fmt.Fprintf(stderr, "rebuild: %v\n", err)
		return 1
	}
	if !report.Passed {
		fmt.Fprintf(stderr, "rebuild: %s\n", rebuildFailureReason(report))
		return 1
	}
	return 0
}

// writeRebuildReport transcribes the pipeline report Run already computed;
// it never re-derives pass/fail so stdout and the exit code cannot disagree.
func writeRebuildReport(stdout io.Writer, report rebuild.Report) {
	for _, stage := range report.Stages {
		fmt.Fprintf(stdout, "[%s] %s: %.2fms", rebuildState(stage.Passed), stage.Name, stage.DurationMS)
		if stage.Detail != "" {
			fmt.Fprintf(stdout, " - %s", stage.Detail)
		}
		fmt.Fprintln(stdout)
	}
	for _, check := range report.Integrity {
		if check.Passed {
			continue
		}
		fmt.Fprintf(stdout, "[FAIL] integrity %s: expected %d, observed %d\n", check.Table, check.Expected, check.Observed)
	}
	for _, probe := range report.Probes {
		if probe.Passed {
			continue
		}
		fmt.Fprintf(stdout, "[FAIL] probe %s: %s\n", probe.Probe, probe.Detail)
	}
	if report.SnapshotDigest != "" {
		fmt.Fprintf(stdout, "snapshot digest: %s\n", report.SnapshotDigest)
	} else {
		fmt.Fprintln(stdout, "snapshot digest: none")
	}
	if report.Publication.Generation.ID != "" {
		fmt.Fprintf(stdout, "generation published: %s (%s)\n", report.Publication.Generation.ID, report.Publication.Generation.Path)
	} else {
		fmt.Fprintln(stdout, "generation published: none")
	}
}

// rebuildFailureReason finds the first broken stage, check or probe so the
// exit path always names a concrete cause instead of a generic failure.
func rebuildFailureReason(report rebuild.Report) string {
	for _, stage := range report.Stages {
		if !stage.Passed {
			return fmt.Sprintf("stage %q failed: %s", stage.Name, stage.Detail)
		}
	}
	for _, check := range report.Integrity {
		if !check.Passed {
			return fmt.Sprintf("integrity check %q failed: expected %d, observed %d", check.Table, check.Expected, check.Observed)
		}
	}
	for _, probe := range report.Probes {
		if !probe.Passed {
			return fmt.Sprintf("probe %q failed: %s", probe.Probe, probe.Detail)
		}
	}
	return "full rebuild failed"
}

func rebuildState(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}

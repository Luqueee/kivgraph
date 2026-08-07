package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Luqueee/ladygraph/internal/app"
	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/logging"
	mcpserver "github.com/Luqueee/ladygraph/internal/mcp"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
	"github.com/Luqueee/ladygraph/internal/synthetic"
	"github.com/Luqueee/ladygraph/internal/version"
)

type mcpRunner func(context.Context) error

func main() {
	logger := logging.New(os.Stderr)
	if len(os.Args) == 2 && os.Args[1] == "serve" {
		logger.Info("starting MCP server", "command", "serve")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runServe(ctx, mcpserver.Run); err != nil {
			logger.Error("MCP server stopped with error", "command", "serve", "error", err)
			os.Exit(1)
		}
		logger.Info("MCP server stopped", "command", "serve")
		return
	}

	exitCode := run(os.Args, os.Stdout, logging.NewErrorWriter(logger))
	if exitCode != 0 {
		logger.Error("command failed", "command", "cli", "exit_code", exitCode)
	}
	os.Exit(exitCode)
}

// runServe owns the MCP loop through the application lifecycle. MCP's stdio
// session is closed by cancelling the shared context; future long-lived
// components register their watcher, worker, connections, and database with
// the same lifecycle before this function waits.
func runServe(ctx context.Context, runMCP mcpRunner) error {
	if ctx == nil {
		ctx = context.Background()
	}
	lifecycle := app.NewLifecycle(ctx)
	if err := lifecycle.Go("mcp", app.RunFunc(runMCP)); err != nil {
		return err
	}

	runDone := make(chan error, 1)
	go func() { runDone <- lifecycle.Wait() }()
	select {
	case runErr := <-runDone:
		return errors.Join(runErr, lifecycle.Shutdown(context.Background()))
	case <-ctx.Done():
		return lifecycle.Shutdown(context.Background())
	}
}

type storageDiagnoser func(context.Context, string) (ladybug.StorageDiagnosis, error)

type graphRebuilder func(context.Context, rebuild.Options) (rebuild.Report, error)

type graphVerifier func(context.Context, string) (ladybug.CanonicalIntegrityReport, error)

type graphRoleResolver func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error)

type graphRollbacker func(context.Context, rebuild.RollbackOptions) (rebuild.RollbackReport, error)

type snapshotBuilder func(context.Context, rebuild.GenerationSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error)

func run(args []string, stdout, stderr io.Writer) int {
	return runWithStorageDiagnoser(args, stdout, stderr, ladybug.DiagnoseStorage)
}

func runWithStorageDiagnoser(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser) int {
	return runWithGraphRebuilder(args, stdout, stderr, diagnose, rebuild.Run)
}

func runWithGraphRebuilder(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser, rebuilder graphRebuilder) int {
	return runWithGraphVerifier(args, stdout, stderr, diagnose, rebuilder, ladybug.VerifyCanonicalIntegrity)
}

func runWithGraphVerifier(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser, rebuilder graphRebuilder, verify graphVerifier) int {
	return runWithGraphRoles(args, stdout, stderr, diagnose, rebuilder, verify, rebuild.Roles)
}

func runWithGraphRoles(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser, rebuilder graphRebuilder, verify graphVerifier, roles graphRoleResolver) int {
	return runWithGraphRollback(args, stdout, stderr, diagnose, rebuilder, verify, roles, rebuild.Rollback)
}

func runWithGraphRollback(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser, rebuilder graphRebuilder, verify graphVerifier, roles graphRoleResolver, rollback graphRollbacker) int {
	return runWithSnapshotBuilder(args, stdout, stderr, diagnose, rebuilder, verify, roles, rollback, rebuild.SnapshotGeneration)
}

func runWithSnapshotBuilder(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser, rebuilder graphRebuilder, verify graphVerifier, roles graphRoleResolver, rollback graphRollbacker, build snapshotBuilder) int {
	if len(args) >= 2 && args[1] == "version" {
		if len(args) == 2 {
			fmt.Fprintln(stdout, version.Value)
			return 0
		}
		if len(args) == 3 && args[2] == "--json" {
			return runVersionJSON(stdout, stderr)
		}
	}
	if len(args) >= 3 && args[1] == "doctor" && args[2] == "storage" {
		return runDoctorStorage(args[3:], stdout, stderr, diagnose)
	}
	if len(args) >= 3 && args[1] == "doctor" && args[2] == "graph" {
		return runDoctorGraph(args[3:], stdout, stderr, verify)
	}
	if len(args) >= 3 && args[1] == "benchmark" && args[2] == "generate-graph" {
		return runGenerateGraph(args[3:], stdout, stderr)
	}
	if len(args) >= 2 && args[1] == "rebuild" {
		return runRebuild(args[2:], stdout, stderr, rebuilder)
	}
	if len(args) >= 3 && args[1] == "graph" && args[2] == "status" {
		return runGraphStatus(args[3:], stdout, stderr, roles)
	}
	if len(args) >= 2 && args[1] == "rollback" {
		return runRollback(args[2:], stdout, stderr, rollback)
	}
	if len(args) >= 2 && args[1] == "snapshot" {
		return runSnapshot(args[2:], stdout, stderr, build)
	}

	fmt.Fprintf(stderr, "usage: %s version [--json]|serve|doctor storage --database PATH|doctor graph --database PATH|benchmark generate-graph [flags]|rebuild --facts PATH --root PATH --generation ID --resolver-version STRING [flags]|graph status --root PATH|rollback --root PATH [--generation ID]|snapshot --root PATH [--generation ID] [--snapshot-id N]\n", args[0])
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
	// A diagnosis that does not say which layout it validated cannot be
	// interpreted: the same path can hold either schema.
	if diagnosis.Schema == ladybug.SchemaCanonical {
		fmt.Fprintf(stdout, "schema: %s (version %d)\n", diagnosis.Schema, diagnosis.SchemaVersion)
	} else {
		fmt.Fprintf(stdout, "schema: %s\n", diagnosis.Schema)
	}
	for _, check := range diagnosis.Checks {
		fmt.Fprintf(stdout, "[%s] %s: %s\n", check.Status, check.Name, check.Detail)
	}
	if diagnosis.Healthy {
		return 0
	}
	return 1
}

func runDoctorGraph(args []string, stdout, stderr io.Writer, verify graphVerifier) int {
	flags := flag.NewFlagSet("doctor graph", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var databasePath string
	flags.StringVar(&databasePath, "database", "", "published canonical LadybugDB database path")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "doctor graph: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	if databasePath == "" {
		fmt.Fprintln(stderr, "doctor graph: --database is required")
		return 2
	}

	report, err := verify(context.Background(), databasePath)
	if err != nil {
		fmt.Fprintf(stderr, "doctor graph: %v\n", err)
		return 1
	}
	writeIntegrityReport(stdout, databasePath, report)
	if report.Passed {
		return 0
	}
	return 1
}

// writeIntegrityReport prints one line per canonical integrity rule with its
// PASS/FAIL state and violation count and, under every failed rule, the
// offending samples VerifyCanonicalIntegrity already bounded: table, key and
// detail.
func writeIntegrityReport(stdout io.Writer, databasePath string, report ladybug.CanonicalIntegrityReport) {
	state := "FAIL"
	if report.Passed {
		state = "PASS"
	}
	fmt.Fprintf(stdout, "graph doctor: %s\n", state)
	fmt.Fprintf(stdout, "database: %s\n", databasePath)
	writeIntegrityFindings(stdout, report.Findings)
}

// writeIntegrityFindings prints one line per rule with its PASS/FAIL state
// and violation count and, under every failed rule, the offending samples,
// shared by doctor graph and rollback so both name a broken invariant
// exactly the same way.
func writeIntegrityFindings(stdout io.Writer, findings []ladybug.IntegrityFinding) {
	for _, finding := range findings {
		findingState := "FAIL"
		if finding.Passed {
			findingState = "PASS"
		}
		fmt.Fprintf(stdout, "[%s] %s: %d violation(s)\n", findingState, finding.Rule, finding.Violations)
		if finding.Passed {
			continue
		}
		for _, sample := range finding.Samples {
			fmt.Fprintf(stdout, "    %s %s: %s\n", sample.Table, sample.Key, sample.Detail)
		}
	}
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
	for _, finding := range report.Invariants.Findings {
		if finding.Passed {
			continue
		}
		fmt.Fprintf(stdout, "[FAIL] invariant %s: %d violation(s)\n", finding.Rule, finding.Violations)
		for _, sample := range finding.Samples {
			fmt.Fprintf(stdout, "    %s %s: %s\n", sample.Table, sample.Key, sample.Detail)
		}
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
	if len(report.Pruned) != 0 {
		fmt.Fprintf(stdout, "generations pruned: %s\n", strings.Join(report.Pruned, ", "))
	} else {
		fmt.Fprintln(stdout, "generations pruned: none")
	}
}

// rebuildFailureReason finds the first broken stage, check, invariant or
// probe so the exit path always names a concrete cause instead of a
// generic failure.
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
	for _, finding := range report.Invariants.Findings {
		if !finding.Passed {
			return fmt.Sprintf("invariant %q failed: %d violation(s)", finding.Rule, finding.Violations)
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

func runGraphStatus(args []string, stdout, stderr io.Writer, roles graphRoleResolver) int {
	flags := flag.NewFlagSet("graph status", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var root string
	flags.StringVar(&root, "root", "", "generation store root directory")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "graph status: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	if root == "" {
		fmt.Fprintln(stderr, "graph status: --root is required")
		return 2
	}

	layout, err := roles(context.Background(), rebuild.LayoutOptions{Root: root, Store: generation.DefaultConfig()})
	if err != nil {
		fmt.Fprintf(stderr, "graph status: %v\n", err)
		return 1
	}
	writeGraphStatus(stdout, root, layout)
	return 0
}

// writeGraphStatus prints the three roles LUQUE-0905 requires
// (graph.active, graph.next, graph.backup) with the path each one names on
// disk, plus the full retained set. graph.active and graph.backup print
// "none" when the store has never published (respectively backed up) a
// generation: that is a legitimate layout, not a rendering error, matching
// the exit code runGraphStatus already returns for it (0).
func writeGraphStatus(stdout io.Writer, root string, layout rebuild.Layout) {
	fmt.Fprintf(stdout, "%s: ", rebuild.RoleActive)
	if layout.Active.ID == "" {
		fmt.Fprintln(stdout, "none")
	} else {
		fmt.Fprintf(stdout, "%s (%s)\n", layout.Active.ID, layout.Active.Path)
	}

	// graph.next never exists on disk until a rebuild actually publishes:
	// this is where generation.Store.Publish would build it, following the
	// documented <root>/generations/<id>.tmp layout.
	generationsDir := filepath.Join(root, "generations")
	if absRoot, err := filepath.Abs(root); err == nil {
		generationsDir = filepath.Join(absRoot, "generations")
	}
	fmt.Fprintf(stdout, "%s: %s\n", rebuild.RoleNext, filepath.Join(generationsDir, layout.NextID+".tmp"))

	fmt.Fprintf(stdout, "%s: ", rebuild.RoleBackup)
	if !layout.HasBackup {
		fmt.Fprintln(stdout, "none")
	} else {
		fmt.Fprintf(stdout, "%s (%s)\n", layout.Backup.ID, layout.Backup.Path)
	}

	if len(layout.Retained) == 0 {
		fmt.Fprintln(stdout, "retained: none")
		return
	}
	fmt.Fprintf(stdout, "retained: %s\n", strings.Join(layout.Retained, ", "))
}

func runRollback(args []string, stdout, stderr io.Writer, rollback graphRollbacker) int {
	flags := flag.NewFlagSet("rollback", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var root, generationID string
	flags.StringVar(&root, "root", "", "generation store root directory")
	flags.StringVar(&generationID, "generation", "", "six digit generation id to roll back to; defaults to the registered graph.backup")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "rollback: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	if root == "" {
		fmt.Fprintln(stderr, "rollback: --root is required")
		return 2
	}

	report, err := rollback(context.Background(), rebuild.RollbackOptions{
		Root:         root,
		Store:        generation.DefaultConfig(),
		GenerationID: generationID,
	})
	writeRollbackReport(stdout, report)
	if err != nil {
		fmt.Fprintf(stderr, "rollback: %v\n", err)
		return 1
	}
	if !report.Passed {
		fmt.Fprintln(stderr, "rollback: report did not pass despite no error")
		return 1
	}
	return 0
}

// writeRollbackReport prints where the rollback moved from and to, the
// digest it expected (the generation's own snapshot.sha256) against the
// one it recomputed from live table counts, and the invariant verdict, so
// a failed rollback is diagnosable from stdout alone even though it never
// reaches the passed state runRollback checks for the exit code.
func writeRollbackReport(stdout io.Writer, report rebuild.RollbackReport) {
	fmt.Fprintf(stdout, "rollback: %s -> %s\n", orNone(report.From.ID), orNone(report.To.ID))
	fmt.Fprintf(stdout, "digest expected: %s\n", orNone(report.Expected))
	fmt.Fprintf(stdout, "digest observed: %s\n", orNone(report.Digest))
	if len(report.Invariants.Findings) == 0 {
		fmt.Fprintln(stdout, "invariants: not evaluated")
	} else {
		invariantState := "FAIL"
		if report.Invariants.Passed {
			invariantState = "PASS"
		}
		fmt.Fprintf(stdout, "invariants: %s\n", invariantState)
		writeIntegrityFindings(stdout, report.Invariants.Findings)
	}
	fmt.Fprintf(stdout, "rollback result: %s\n", rebuildState(report.Passed))
}

func orNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func runSnapshot(args []string, stdout, stderr io.Writer, build snapshotBuilder) int {
	flags := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var root, generationID string
	var snapshotID int64
	flags.StringVar(&root, "root", "", "generation store root directory")
	flags.StringVar(&generationID, "generation", "", "six digit generation id to snapshot; defaults to the registered graph.active")
	flags.Int64Var(&snapshotID, "snapshot-id", 0, "snapshot id stamped on the built HotSnapshot")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "snapshot: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	if root == "" {
		fmt.Fprintln(stderr, "snapshot: --root is required")
		return 2
	}

	_, report, err := build(context.Background(), rebuild.GenerationSnapshotOptions{
		Root:         root,
		Store:        generation.DefaultConfig(),
		GenerationID: generationID,
		SnapshotID:   uint64(snapshotID),
	})
	writeSnapshotReport(stdout, report)
	if err != nil {
		fmt.Fprintf(stderr, "snapshot: %v\n", err)
		return 1
	}
	if !report.Passed {
		fmt.Fprintln(stderr, "snapshot: report did not pass despite no error")
		return 1
	}
	return 0
}

// writeSnapshotReport prints the account BuildSnapshot/SnapshotGeneration
// already computed: id, row format version, content digest, per table
// counts, and the edges that cannot be represented in the CSR (structural
// and Package to Package relations — see README.md), so an operator can
// tell a healthy generation from a broken one without a debugger.
func writeSnapshotReport(stdout io.Writer, report rebuild.SnapshotReport) {
	fmt.Fprintf(stdout, "snapshot: %s\n", rebuildState(report.Passed))
	fmt.Fprintf(stdout, "snapshot id: %d\n", report.SnapshotID)
	fmt.Fprintf(stdout, "version: %d\n", report.Version)
	fmt.Fprintf(stdout, "digest: %s\n", orNone(report.Digest))
	fmt.Fprintf(stdout, "repositories: %d\n", report.Stats.Repositories)
	fmt.Fprintf(stdout, "packages: %d\n", report.Stats.Packages)
	fmt.Fprintf(stdout, "files: %d\n", report.Stats.Files)
	fmt.Fprintf(stdout, "symbols: %d\n", report.Stats.Symbols)
	fmt.Fprintf(stdout, "evidence: %d\n", report.Stats.Evidence)
	fmt.Fprintf(stdout, "edges: %d\n", report.Stats.Edges)
	fmt.Fprintf(stdout, "edges not represented in the CSR: %d\n", report.Stats.SkippedEdges)
}

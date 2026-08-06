// Package rebuild orchestrates the full rebuild: facts become the canonical
// LadybugDB graph and that graph replaces CURRENT, or nothing changes.
//
// The pipeline is eight checkpoints — facts, staging, graph.next, bulk load,
// integrity, snapshot, golden probes, publish — and every one of them is
// recorded on the returned Report, in that order, whether or not it passed.
// The integrity checkpoint itself has two gates: canonical table row counts
// must match what the fact set implies, and the semantic invariants of
// LUQUE-0904 (no dangling exact edges, no missing evidence, no duplicate
// stable keys, no unknown confidence or provenance, no invalid repository
// ownership) must hold over the graph that was just loaded. The snapshot
// checkpoint also has two gates: it writes the deterministic digest of the
// candidate's per-table counts (snapshot.sha256, next to the candidate
// database), and it builds the real in-memory HotSnapshot from the
// candidate's definitive graph (BuildSnapshot) — a graph the latter cannot
// convert is never published, however clean its integrity gate looked.
// The package never talks to LadybugDB itself: staging, loading, counting,
// scanning, probing and invariant checking are hooks the caller supplies
// (Options.Load/Counts/Scan/Probes/Integrity), which default to the real
// ladybug implementations. That is what lets the orchestration be
// exercised without cgo.
package rebuild

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
)

// ErrRebuildFailed reports that a full rebuild did not reach a published
// generation. The wrapped detail names the stage that stopped it, so a
// caller can report exactly what failed without parsing the Report.
var ErrRebuildFailed = errors.New("full rebuild failed")

// StageName identifies one checkpoint of the rebuild pipeline.
type StageName string

const (
	StageFacts     StageName = "facts"
	StageStaging   StageName = "staging"
	StageGraphNext StageName = "graph.next"
	StageBulkLoad  StageName = "bulk load"
	StageIntegrity StageName = "integrity"
	StageSnapshot  StageName = "snapshot"
	StageProbes    StageName = "golden probes"
	StagePublish   StageName = "publish"
)

// Stage is one recorded checkpoint of a rebuild run.
type Stage struct {
	Name       StageName
	Passed     bool
	Detail     string
	DurationMS float64
}

// IntegrityCheck compares the row count LadybugDB reports for one canonical
// table against the count the fact set implies.
type IntegrityCheck struct {
	Table    string
	Expected int64
	Observed int64
	Passed   bool
}

// Report is the full account of one rebuild run, successful or not.
type Report struct {
	GenerationID   string
	Stages         []Stage
	Load           ladybug.LoadReport
	Integrity      []IntegrityCheck
	Invariants     ladybug.CanonicalIntegrityReport
	Probes         []ladybug.CanonicalProbeResult
	SnapshotDigest string
	Snapshot       SnapshotReport
	Publication    generation.Publication
	Pruned         []string
	Passed         bool
}

// Options configures one full rebuild run.
type Options struct {
	Root            string
	GenerationID    string
	Facts           facts.Set
	ResolverVersion string
	SnapshotID      int64
	Store           generation.Config

	// Load, Counts, Probes, Integrity and Scan default to the ladybug
	// implementations; tests substitute them so the orchestration is
	// exercised without cgo.
	Load      func(context.Context, string, facts.Set, ladybug.CanonicalLoadOptions) (ladybug.LoadReport, error)
	Counts    func(context.Context, string) (map[string]int64, error)
	Probes    func(context.Context, string, []ladybug.CanonicalProbe) ([]ladybug.CanonicalProbeResult, error)
	Integrity func(context.Context, string) (ladybug.CanonicalIntegrityReport, error)
	Scan      func(context.Context, string) (ladybug.CanonicalGraph, error)
}

// snapshotFileName is the digest file Run writes next to the candidate
// database, inside the generation directory generation.Store.Publish builds.
const snapshotFileName = "snapshot.sha256"

// Run drives one full rebuild: it stages and bulk loads the fact set into a
// fresh candidate database, verifies it, snapshots it, and publishes it
// through the generation store's atomic swap. A failure at any stage aborts
// the publish, so the previous CURRENT generation is left untouched.
func Run(ctx context.Context, options Options) (Report, error) {
	report := Report{GenerationID: options.GenerationID}

	if err := ctx.Err(); err != nil {
		return report, fmt.Errorf("%w: %v", ErrRebuildFailed, err)
	}
	if options.GenerationID == "" {
		return report, fmt.Errorf("%w: generation id is required", ErrRebuildFailed)
	}
	if options.ResolverVersion == "" {
		return report, fmt.Errorf("%w: resolver version is required", ErrRebuildFailed)
	}

	// facts: an invalid set aborts before the store is even opened, so a
	// bad rebuild never leaves a stray directory behind.
	factsStart := time.Now()
	if err := options.Facts.Validate(); err != nil {
		report.Stages = append(report.Stages, Stage{
			Name:       StageFacts,
			Detail:     err.Error(),
			DurationMS: elapsedMS(factsStart),
		})
		return report, fmt.Errorf("%w: facts stage: %v", ErrRebuildFailed, err)
	}
	report.Stages = append(report.Stages, Stage{
		Name:       StageFacts,
		Passed:     true,
		Detail:     factsSummary(options.Facts),
		DurationMS: elapsedMS(factsStart),
	})

	load := options.Load
	if load == nil {
		load = ladybug.LoadCanonical
	}
	counts := options.Counts
	if counts == nil {
		counts = ladybug.CanonicalTableCounts
	}
	runProbes := options.Probes
	if runProbes == nil {
		runProbes = ladybug.RunCanonicalProbes
	}
	verifyIntegrity := options.Integrity
	if verifyIntegrity == nil {
		verifyIntegrity = ladybug.VerifyCanonicalIntegrity
	}
	scan := options.Scan
	if scan == nil {
		scan = ladybug.ScanCanonical
	}
	storeConfig := options.Store
	if storeConfigIsZero(storeConfig) {
		storeConfig = generation.DefaultConfig()
	}

	store, err := generation.New(options.Root, storeConfig)
	if err != nil {
		return report, fmt.Errorf("%w: open generation store: %v", ErrRebuildFailed, err)
	}

	loadOptions := ladybug.CanonicalLoadOptions{
		SnapshotID:      options.SnapshotID,
		ResolverVersion: options.ResolverVersion,
	}
	// Golden probes are derived once, up front, from the same fact set every
	// other stage sees: no hand written keys, no randomness.
	goldenProbes := deriveGoldenProbes(options.Facts)

	// The middle six stages are populated from inside the Build and Validate
	// closures below, which generation.Store.Publish calls in order. They
	// start as named placeholders so a Report is well formed even if Publish
	// fails before reaching the closure that would have completed them.
	stagingStage := Stage{Name: StageStaging}
	graphNextStage := Stage{Name: StageGraphNext}
	bulkLoadStage := Stage{Name: StageBulkLoad}
	snapshotStage := Stage{Name: StageSnapshot}
	integrityStage := Stage{Name: StageIntegrity}
	probesStage := Stage{Name: StageProbes}

	var (
		loadReport       ladybug.LoadReport
		integrityChecks  []IntegrityCheck
		invariantsReport ladybug.CanonicalIntegrityReport
		probeResults     []ladybug.CanonicalProbeResult
		snapshotDigest   string
		snapshotReport   SnapshotReport
	)

	build := func(buildCtx context.Context, candidatePath string) error {
		// graph.next: the candidate directory already exists by the time
		// Publish calls us, so this stage just records where it landed.
		graphNextStart := time.Now()
		graphNextStage = Stage{
			Name:       StageGraphNext,
			Passed:     true,
			Detail:     candidatePath,
			DurationMS: elapsedMS(graphNextStart),
		}

		databasePath := filepath.Join(candidatePath, storeConfig.DatabaseFile)
		loadStart := time.Now()
		result, loadErr := load(buildCtx, databasePath, options.Facts, loadOptions)
		loadReport = result
		if loadErr != nil {
			// The loader does not report where staging ended and copying
			// began when it fails, so the whole elapsed time is charged to
			// staging and bulk load is recorded as never having started.
			detail := loadErr.Error()
			stagingStage = Stage{Name: StageStaging, Detail: detail, DurationMS: elapsedMS(loadStart)}
			bulkLoadStage = Stage{Name: StageBulkLoad, Detail: detail}
			return fmt.Errorf("load canonical graph: %w", loadErr)
		}
		stagingStage = Stage{
			Name:       StageStaging,
			Passed:     true,
			Detail:     fmt.Sprintf("staged %d canonical table(s) from the fact set", len(result.Tables)),
			DurationMS: result.StagingMS,
		}
		bulkLoadStage = Stage{
			Name:       StageBulkLoad,
			Passed:     true,
			Detail:     fmt.Sprintf("copied %d node(s) and %d edge(s) into %s", result.Nodes, result.Edges, databasePath),
			DurationMS: result.CopyMS,
		}

		// snapshot: digest the graph the load just produced, from the same
		// per-table counts the loader reported, and write it beside the
		// candidate database before Publish ever renames the directory.
		// That digest only proves the row counts match what the fact set
		// implies; it says nothing about whether the definitive graph the
		// candidate now holds can actually become the HotSnapshot the
		// serving path reads. Build it here, from the candidate database
		// itself, and abort the publish if it cannot: a graph that cannot
		// become a snapshot must not become CURRENT.
		snapshotStart := time.Now()
		digest, digestErr := writeSnapshotDigest(candidatePath, result.Tables)
		if digestErr != nil {
			snapshotStage = Stage{Name: StageSnapshot, Detail: digestErr.Error(), DurationMS: elapsedMS(snapshotStart)}
			return fmt.Errorf("write snapshot digest: %w", digestErr)
		}
		snapshotDigest = digest

		_, hotSnapshotReport, hotSnapshotErr := BuildSnapshot(buildCtx, BuildSnapshotOptions{
			DatabasePath: databasePath,
			SnapshotID:   uint64(options.SnapshotID),
			Scan:         scan,
		})
		snapshotReport = hotSnapshotReport
		if hotSnapshotErr != nil {
			detail := fmt.Sprintf("wrote %s with digest %s; %v", snapshotFileName, digest, hotSnapshotErr)
			snapshotStage = Stage{Name: StageSnapshot, Detail: detail, DurationMS: elapsedMS(snapshotStart)}
			return fmt.Errorf("build hot snapshot: %w", hotSnapshotErr)
		}
		snapshotStage = Stage{
			Name:   StageSnapshot,
			Passed: true,
			Detail: fmt.Sprintf(
				"wrote %s with digest %s; hot snapshot %d built (%d repositories, %d packages, %d files, %d symbols, %d symbol edges, %d package edges, %d unresolved reference(s), %d edge(s) not represented in the CSR)",
				snapshotFileName, digest, hotSnapshotReport.SnapshotID,
				hotSnapshotReport.Stats.Repositories, hotSnapshotReport.Stats.Packages, hotSnapshotReport.Stats.Files,
				hotSnapshotReport.Stats.Symbols, hotSnapshotReport.Stats.Edges, hotSnapshotReport.Stats.PackageEdges,
				hotSnapshotReport.Stats.Unresolved, hotSnapshotReport.Stats.SkippedEdges,
			),
			DurationMS: elapsedMS(snapshotStart),
		}
		return nil
	}

	validate := func(validateCtx context.Context, candidate generation.Generation) error {
		// integrity: two gates, both required. Every canonical table,
		// including the ones the fact set implies zero rows for, must match
		// what LadybugDB actually holds, and the semantic invariants (no
		// dangling exact edges, no missing evidence, no duplicate stable
		// keys, no unknown confidence or provenance, no invalid repository
		// ownership) must hold over the graph that was just loaded. Both
		// halves always run, so a failure on either always reports how the
		// other half fared too.
		integrityStart := time.Now()
		expected, expectedErr := ladybug.CanonicalTableRows(options.Facts, loadOptions)
		if expectedErr != nil {
			integrityStage = Stage{Name: StageIntegrity, Detail: expectedErr.Error(), DurationMS: elapsedMS(integrityStart)}
			return fmt.Errorf("derive expected canonical counts: %w", expectedErr)
		}
		observed, countsErr := counts(validateCtx, candidate.DatabasePath)
		if countsErr != nil {
			integrityStage = Stage{Name: StageIntegrity, Detail: countsErr.Error(), DurationMS: elapsedMS(integrityStart)}
			return fmt.Errorf("read canonical table counts: %w", countsErr)
		}
		tableNames := ladybug.CanonicalTableNames()
		checks := make([]IntegrityCheck, 0, len(tableNames))
		mismatched := 0
		for _, table := range tableNames {
			expectedCount := int64(len(expected[table]))
			observedCount := observed[table]
			passed := expectedCount == observedCount
			if !passed {
				mismatched++
			}
			checks = append(checks, IntegrityCheck{Table: table, Expected: expectedCount, Observed: observedCount, Passed: passed})
		}
		invariants, invariantsErr := verifyIntegrity(validateCtx, candidate.DatabasePath)
		if invariantsErr != nil {
			integrityStage = Stage{Name: StageIntegrity, Detail: invariantsErr.Error(), DurationMS: elapsedMS(integrityStart)}
			return fmt.Errorf("verify canonical integrity: %w", invariantsErr)
		}
		integrityChecks = checks
		invariantsReport = invariants
		integrityStage = Stage{
			Name:       StageIntegrity,
			Passed:     mismatched == 0 && invariants.Passed,
			Detail:     integrityDetail(checks, mismatched, invariants),
			DurationMS: elapsedMS(integrityStart),
		}
		if mismatched != 0 || !invariants.Passed {
			return fmt.Errorf("integrity check failed: %d of %d canonical table(s) mismatched, %d invariant violation(s)", mismatched, len(tableNames), invariants.Violations())
		}

		// golden probes: a handful of reads against the candidate graph,
		// derived from the fact set itself. Nothing to probe is a pass, not
		// a skip: an empty graph has nothing to contradict.
		probesStart := time.Now()
		if len(goldenProbes) == 0 {
			probesStage = Stage{
				Name:       StageProbes,
				Passed:     true,
				Detail:     "fact set has no symbols or edges to probe",
				DurationMS: elapsedMS(probesStart),
			}
			return nil
		}
		results, probesErr := runProbes(validateCtx, candidate.DatabasePath, goldenProbes)
		if probesErr != nil {
			probesStage = Stage{Name: StageProbes, Detail: probesErr.Error(), DurationMS: elapsedMS(probesStart)}
			return fmt.Errorf("run golden probes: %w", probesErr)
		}
		if len(results) != len(goldenProbes) {
			detail := fmt.Sprintf("expected %d probe result(s), got %d", len(goldenProbes), len(results))
			probesStage = Stage{Name: StageProbes, Detail: detail, DurationMS: elapsedMS(probesStart)}
			return errors.New(detail)
		}
		probeResults = results
		failed := 0
		for _, result := range results {
			if !result.Passed {
				failed++
			}
		}
		probesStage = Stage{
			Name:       StageProbes,
			Passed:     failed == 0,
			Detail:     probesDetail(results, failed),
			DurationMS: elapsedMS(probesStart),
		}
		if failed != 0 {
			return fmt.Errorf("golden probes failed: %d of %d probe(s) did not pass", failed, len(results))
		}
		return nil
	}

	publishStart := time.Now()
	publication, publishErr := store.Publish(ctx, generation.PublishRequest{
		ID:       options.GenerationID,
		Build:    build,
		Validate: validate,
	})
	publishDuration := elapsedMS(publishStart)

	report.Stages = append(report.Stages, stagingStage, graphNextStage, bulkLoadStage, integrityStage, snapshotStage, probesStage)
	report.Load = loadReport
	report.Integrity = integrityChecks
	report.Invariants = invariantsReport
	report.Probes = probeResults
	report.SnapshotDigest = snapshotDigest
	report.Snapshot = snapshotReport

	if publishErr != nil {
		report.Stages = append(report.Stages, Stage{Name: StagePublish, Detail: publishErr.Error(), DurationMS: publishDuration})
		return report, fmt.Errorf("%w: publish stage: %v", ErrRebuildFailed, publishErr)
	}

	report.Publication = publication
	detail := fmt.Sprintf("published generation %s (previous %s)", publication.Generation.ID, previousGenerationLabel(publication.PreviousID))

	// Retention prunes every generation that is neither the one just
	// published nor its new backup (the generation that was active a
	// moment ago), so the store holds exactly graph.active and
	// graph.backup once a publish lands. It only runs after Publish has
	// already returned success, so CURRENT is durable before anything is
	// removed. A pruning failure (a directory that would not remove, a
	// transient I/O error) must not turn an effective publish into a
	// reported failure: the active graph is already correct and serving,
	// and a stray retained generation is a disk cost, not a correctness
	// one — the next successful rebuild's own prune retries it. So the
	// failure is folded into this stage's Detail, for an operator to see,
	// instead of into Passed.
	pruned, pruneErr := store.Prune(ctx)
	report.Pruned = pruned
	if pruneErr != nil {
		detail += fmt.Sprintf("; prune failed: %v", pruneErr)
	} else if len(pruned) != 0 {
		detail += fmt.Sprintf("; pruned generation(s) %s", strings.Join(pruned, ", "))
	}

	report.Stages = append(report.Stages, Stage{
		Name:       StagePublish,
		Passed:     true,
		Detail:     detail,
		DurationMS: elapsedMS(publishStart),
	})
	report.Passed = allStagesPassed(report.Stages)
	return report, nil
}

func elapsedMS(start time.Time) float64 {
	return float64(time.Since(start)) / float64(time.Millisecond)
}

func allStagesPassed(stages []Stage) bool {
	for _, stage := range stages {
		if !stage.Passed {
			return false
		}
	}
	return true
}

// storeConfigIsZero reports whether the caller left Options.Store at its
// zero value, so Run knows to fall back to generation.DefaultConfig. Config
// carries a func field, so it cannot be compared with ==.
func storeConfigIsZero(config generation.Config) bool {
	return config.ReserveBytes == 0 &&
		config.MarginBytes == 0 &&
		config.FreePermille == 0 &&
		config.DatabaseFile == "" &&
		config.FaultInjector == nil
}

// openGenerationStore opens the generation store rooted at root, defaulting
// config to generation.DefaultConfig() exactly like Run does when the
// caller leaves its own Store field at the zero value. Roles and Rollback
// both resolve a store this same way, so LayoutOptions.Store and
// RollbackOptions.Store behave identically to Options.Store.
func openGenerationStore(root string, config generation.Config) (*generation.Store, error) {
	if storeConfigIsZero(config) {
		config = generation.DefaultConfig()
	}
	return generation.New(root, config)
}

func previousGenerationLabel(id string) string {
	if id == "" {
		return "<none>"
	}
	return id
}

func factsSummary(set facts.Set) string {
	return fmt.Sprintf(
		"validated %d repositories, %d packages, %d files, %d symbols, %d edges, %d unresolved references",
		len(set.Repositories), len(set.Packages), len(set.Files), len(set.Symbols), len(set.Edges), len(set.Unresolved),
	)
}

// integrityDetail summarizes both halves of the integrity stage: how many
// canonical tables matched their expected row count, and how many semantic
// invariant violations VerifyCanonicalIntegrity found over the graph that
// was just loaded. Every broken rule is named, so a failure is diagnosable
// from the stage Detail alone, without a separate doctor graph run.
func integrityDetail(checks []IntegrityCheck, mismatched int, invariants ladybug.CanonicalIntegrityReport) string {
	matched := len(checks) - mismatched
	detail := fmt.Sprintf("%d of %d canonical table(s) matched their expected count", matched, len(checks))
	if mismatched != 0 {
		mismatches := make([]string, 0, mismatched)
		for _, check := range checks {
			if !check.Passed {
				mismatches = append(mismatches, fmt.Sprintf("%s expected=%d observed=%d", check.Table, check.Expected, check.Observed))
			}
		}
		detail += fmt.Sprintf(" (%s)", strings.Join(mismatches, "; "))
	}
	detail += fmt.Sprintf("; %d invariant violation(s)", invariants.Violations())
	if failedRules := failedIntegrityRuleNames(invariants); len(failedRules) != 0 {
		detail += fmt.Sprintf(" in rule(s) %s", strings.Join(failedRules, ", "))
	}
	return detail
}

// failedIntegrityRuleNames names every rule with at least one violation, in
// the order the report already carries — CanonicalIntegrityRules fixes that
// order — so the integrity stage Detail never depends on map iteration.
func failedIntegrityRuleNames(report ladybug.CanonicalIntegrityReport) []string {
	names := make([]string, 0, len(report.Findings))
	for _, finding := range report.Findings {
		if !finding.Passed {
			names = append(names, string(finding.Rule))
		}
	}
	return names
}

func probesDetail(results []ladybug.CanonicalProbeResult, failed int) string {
	if failed == 0 {
		return fmt.Sprintf("%d golden probe(s) passed", len(results))
	}
	failures := make([]string, 0, failed)
	for _, result := range results {
		if !result.Passed {
			failures = append(failures, fmt.Sprintf("%s: %s", result.Probe, result.Detail))
		}
	}
	return fmt.Sprintf("%d of %d golden probe(s) failed: %s", failed, len(results), strings.Join(failures, "; "))
}

// writeSnapshotDigest computes the deterministic digest of the graph the
// build stage just produced and writes it next to the candidate database,
// inside the directory generation.Store.Publish will rename into place.
func writeSnapshotDigest(candidatePath string, tables map[string]int64) (string, error) {
	digest := canonicalSnapshotDigest(tables)
	path := filepath.Join(candidatePath, snapshotFileName)
	if err := os.WriteFile(path, []byte(digest+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}
	return digest, nil
}

// RefreshSnapshotDigest recomputes snapshot.sha256 for an already published
// generation whose graph was mutated in place by an incremental delta.
//
// Rollback revalidates a destination by recomputing this digest from the
// live row counts and comparing it against the recorded file, so a
// generation mutated after publication must have its digest rewritten or it
// can never be rolled back to again. Both paths share writeSnapshotDigest,
// so an in-place update and a fresh publish can never disagree about what a
// generation's digest means.
func RefreshSnapshotDigest(generationPath string, tables map[string]int64) (string, error) {
	return writeSnapshotDigest(generationPath, tables)
}

// canonicalSnapshotDigest hashes the canonical schema version together with
// the row count of every canonical table, sorted by table name, so two
// rebuilds of the same fact set always agree byte for byte: no machine path,
// no clock, following the pattern of benchmarks/ladybug-recovery's
// diagnosisGoldenDigest.
func canonicalSnapshotDigest(tables map[string]int64) string {
	hash := sha256.New()
	fmt.Fprintf(hash, "schema_version=%d\n", ladybug.CanonicalSchemaVersion)
	for _, table := range ladybug.CanonicalTableNames() {
		fmt.Fprintf(hash, "table:%s=%d\n", table, tables[table])
	}
	return hex.EncodeToString(hash.Sum(nil))
}

// sortedFacts returns a copy of set whose collections are ordered by durable
// key, without mutating the caller's slices: golden probe derivation must be
// deterministic even when the caller hands Run an unsorted fact set.
func sortedFacts(set facts.Set) facts.Set {
	clone := facts.Set{
		Repositories: append([]facts.Repository(nil), set.Repositories...),
		Packages:     append([]facts.Package(nil), set.Packages...),
		Files:        append([]facts.File(nil), set.Files...),
		Symbols:      append([]facts.Symbol(nil), set.Symbols...),
		Evidence:     append([]facts.Evidence(nil), set.Evidence...),
		Edges:        append([]facts.Edge(nil), set.Edges...),
		Unresolved:   append([]facts.UnresolvedReference(nil), set.Unresolved...),
	}
	clone.Sort()
	return clone
}

// symbolRelationKinds is the subset of facts.EdgeKind whose canonical table
// connects Symbol to Symbol. RunCanonicalProbes always matches both probe
// endpoints against the Symbol node table, so only these kinds can back an
// outgoing-edge or edge-to-edge probe; containment and package level edges
// (CONTAINS_PACKAGE, CONTAINS_FILE, DEFINES, PACKAGE_DEPENDS_ON,
// MODULE_DEPENDS_ON) never can, whatever their source key looks like.
var symbolRelationKinds = map[facts.EdgeKind]struct{}{
	facts.ImportsSymbol:    {},
	facts.Exports:          {},
	facts.Reexports:        {},
	facts.References:       {},
	facts.CallsDirect:      {},
	facts.PassesAsCallback: {},
	facts.AssignsFunction:  {},
	facts.ReturnsFunction:  {},
	facts.TypeUses:         {},
	facts.Implements:       {},
	facts.Extends:          {},
	facts.Embeds:           {},
	facts.Overrides:        {},
}

// symbolToSymbolEdges keeps only the edges a Symbol-anchored probe can read.
func symbolToSymbolEdges(edges []facts.Edge) []facts.Edge {
	filtered := make([]facts.Edge, 0, len(edges))
	for _, edge := range edges {
		if _, ok := symbolRelationKinds[edge.Kind]; ok {
			filtered = append(filtered, edge)
		}
	}
	return filtered
}

// deriveGoldenProbes builds a deterministic, minimal read plan straight from
// the fact set: no hand written keys, no randomness, so the same set always
// produces the same probes.
//
//   - symbol existence: the first symbol in key order (empty EdgeTable is
//     what RunCanonicalProbes reads as a plain existence check).
//   - busiest outgoing table: the (symbol, edge table) pair with the most
//     outgoing edges among the Symbol to Symbol relations, ties broken by
//     key then table name.
//   - one concrete edge: the source and target of the first Symbol to
//     Symbol edge in the same order facts.Set.Sort uses.
func deriveGoldenProbes(set facts.Set) []ladybug.CanonicalProbe {
	sorted := sortedFacts(set)
	symbolEdges := symbolToSymbolEdges(sorted.Edges)
	if len(sorted.Symbols) == 0 && len(symbolEdges) == 0 {
		return nil
	}

	probes := make([]ladybug.CanonicalProbe, 0, 3)

	if len(sorted.Symbols) > 0 {
		first := sorted.Symbols[0]
		probes = append(probes, ladybug.CanonicalProbe{
			Name:      "symbol exists: " + first.Key,
			SymbolKey: first.Key,
			MinRows:   1,
		})
	}

	if len(symbolEdges) > 0 {
		source, table, count := busiestOutgoingTable(symbolEdges)
		probes = append(probes, ladybug.CanonicalProbe{
			Name:      fmt.Sprintf("busiest outgoing table: %s from %s", table, source),
			SymbolKey: source,
			EdgeTable: table,
			MinRows:   count,
		})

		edge := symbolEdges[0]
		probes = append(probes, ladybug.CanonicalProbe{
			Name:      fmt.Sprintf("edge %s: %s -> %s", edge.Kind, edge.SourceKey, edge.TargetKey),
			SymbolKey: edge.SourceKey,
			TargetKey: edge.TargetKey,
			EdgeTable: string(edge.Kind),
			MinRows:   1,
		})
	}

	return probes
}

// busiestOutgoingTable finds the (source, table) pair with the most outgoing
// edges. Ties break on source key then table name, so the winner never
// depends on map iteration order.
func busiestOutgoingTable(edges []facts.Edge) (source, table string, count int64) {
	type sourceTable struct{ source, table string }
	counts := make(map[sourceTable]int64, len(edges))
	for _, edge := range edges {
		counts[sourceTable{source: edge.SourceKey, table: string(edge.Kind)}]++
	}
	keys := make([]sourceTable, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		if keys[i].source != keys[j].source {
			return keys[i].source < keys[j].source
		}
		return keys[i].table < keys[j].table
	})
	winner := keys[0]
	return winner.source, winner.table, counts[winner]
}

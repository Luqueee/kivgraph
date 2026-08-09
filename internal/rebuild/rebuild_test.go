package rebuild

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/metrics"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
)

// sampleFacts is a small but complete, Validate-passing fact set: one
// repository, one package, two files, two symbols, and both a containment
// and a semantic (CALLS_DIRECT) edge, so every rebuild stage — including
// golden probes — has real work to do.
func sampleFacts() facts.Set {
	repoKey := facts.RepositoryKey("acme/widgets")
	pkgKey := facts.PackageKey(facts.LanguageGo, repoKey, "widgets")
	fileAKey := facts.FileKey(repoKey, "widgets.go")
	fileBKey := facts.FileKey(repoKey, "helper.go")
	symbolNewKey := "symbol:go:acme/widgets.New"
	symbolHelperKey := "symbol:go:acme/widgets.Helper"

	return facts.Set{
		Repositories: []facts.Repository{
			{Key: repoKey, Name: "acme/widgets", RootPath: "/repos/widgets", Languages: []facts.Language{facts.LanguageGo}},
		},
		Packages: []facts.Package{
			{Key: pkgKey, RepositoryKey: repoKey, Language: facts.LanguageGo, Name: "widgets", RootPath: "/repos/widgets"},
		},
		Files: []facts.File{
			{Key: fileAKey, RepositoryKey: repoKey, PackageKey: pkgKey, Path: "widgets.go", Language: facts.LanguageGo},
			{Key: fileBKey, RepositoryKey: repoKey, PackageKey: pkgKey, Path: "helper.go", Language: facts.LanguageGo},
		},
		Symbols: []facts.Symbol{
			{
				Key: symbolNewKey, CanonicalIdentity: "go:acme/widgets.New", RepositoryKey: repoKey,
				PackageKey: pkgKey, FileKey: fileAKey, Language: facts.LanguageGo, Name: "New",
				QualifiedName: "widgets.New", Kind: "func", Exported: true,
			},
			{
				Key: symbolHelperKey, CanonicalIdentity: "go:acme/widgets.Helper", RepositoryKey: repoKey,
				PackageKey: pkgKey, FileKey: fileBKey, Language: facts.LanguageGo, Name: "Helper",
				QualifiedName: "widgets.Helper", Kind: "func", Exported: false,
			},
		},
		Edges: []facts.Edge{
			{Kind: facts.ContainsPackage, SourceKey: repoKey, TargetKey: pkgKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsFile, SourceKey: pkgKey, TargetKey: fileAKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsFile, SourceKey: pkgKey, TargetKey: fileBKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.Defines, SourceKey: fileAKey, TargetKey: symbolNewKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.Defines, SourceKey: fileBKey, TargetKey: symbolHelperKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.CallsDirect, SourceKey: symbolNewKey, TargetKey: symbolHelperKey, Confidence: facts.ExactTypechecked, Provenance: facts.GoTypesUse},
		},
	}
}

// buildOptions wires the real canonical rendering (ladybug.CanonicalTableRows,
// landed with no build tag) behind fake Load/Counts/Probes/Integrity/Scan
// hooks, so these tests exercise Run's orchestration without cgo while
// still catching a real mismatch between what gets "loaded" and what
// integrity expects.
func buildOptions(t *testing.T, root, generationID string, set facts.Set) Options {
	t.Helper()
	return Options{
		Root:            root,
		GenerationID:    generationID,
		Facts:           set,
		ResolverVersion: "resolver-test-1",
		SnapshotID:      7,
		Load:            fakeLoad,
		Counts:          fakeCounts,
		Probes:          fakePassingProbes,
		Integrity:       fakeIntegrityPassing,
		Scan:            fakeScan,
	}
}

// requireSpaceOrSkip turns the one failure that belongs to the host rather
// than to the code under test into a skip.
//
// Publishing refuses a filesystem more than 85% full whatever the size of the
// database, so on such a host these tests never reach the behaviour they are
// about. The policy itself is covered by the generation package, which builds
// its store with an explicit one.
func requireSpaceOrSkip(t *testing.T, err error) {
	t.Helper()
	if errors.Is(err, generation.ErrInsufficientSpace) {
		t.Skipf("host filesystem cannot satisfy the default space policy: %v", err)
	}
}

// seedGeneration publishes the generation the rollback and failure tests
// compare against.
func seedGeneration(t *testing.T, root string) {
	t.Helper()
	if _, err := Run(context.Background(), buildOptions(t, root, "000001", sampleFacts())); err != nil {
		requireSpaceOrSkip(t, err)
		t.Fatalf("seed Run() error = %v", err)
	}
}

// fakeScan stands in for ladybug.ScanCanonical: a small, valid, hand
// written definitive graph (fakeCanonicalGraph, in snapshot_test.go) so
// Run's snapshot stage has real content to convert, decoupled from
// whatever facts.Set the test under test seeds — the same "canned, not
// derived from the input" stance fakeIntegrityPassing and
// fakePassingProbes already take toward the fact set they're paired with.
func fakeScan(context.Context, string) (ladybug.CanonicalGraph, error) {
	return fakeCanonicalGraph(), nil
}

// fakeLoad stands in for ladybug.LoadCanonical: it renders the set through
// the real CanonicalTableRows, drops a placeholder file where the database
// belongs (generation.Store requires a regular file there) and remembers the
// row counts on the side so fakeCounts can read them back.
func fakeLoad(_ context.Context, path string, set facts.Set, options ladybug.CanonicalLoadOptions) (ladybug.LoadReport, error) {
	rows, err := ladybug.CanonicalTableRows(set, options)
	if err != nil {
		return ladybug.LoadReport{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return ladybug.LoadReport{}, err
	}
	if err := os.WriteFile(path, []byte("fake canonical graph\n"), 0o600); err != nil {
		return ladybug.LoadReport{}, err
	}
	nodeTables := canonicalNodeTableNames()
	tables := make(map[string]int64, len(rows))
	var nodes, edges int64
	for table, tableRows := range rows {
		count := int64(len(tableRows))
		tables[table] = count
		if nodeTables[table] {
			nodes += count
		} else {
			edges += count
		}
	}
	if err := writeFakeCounts(path, tables); err != nil {
		return ladybug.LoadReport{}, err
	}
	return ladybug.LoadReport{Tables: tables, Nodes: nodes, Edges: edges, StagingMS: 3.5, CopyMS: 6.25}, nil
}

// fakeCounts stands in for ladybug.CanonicalTableCounts: it reads back
// exactly what fakeLoad wrote for this database path.
func fakeCounts(_ context.Context, path string) (map[string]int64, error) {
	return readFakeCounts(path)
}

// fakeCountsWithMismatch corrupts one table's observed count, independent of
// what was actually loaded, to force an integrity failure deterministically.
func fakeCountsWithMismatch(table string, delta int64) func(context.Context, string) (map[string]int64, error) {
	return func(ctx context.Context, path string) (map[string]int64, error) {
		counts, err := fakeCounts(ctx, path)
		if err != nil {
			return nil, err
		}
		counts[table] += delta
		return counts, nil
	}
}

// fakePassingProbes stands in for ladybug.RunCanonicalProbes: every probe
// reads exactly its MinRows, so every probe passes.
func fakePassingProbes(_ context.Context, _ string, probes []ladybug.CanonicalProbe) ([]ladybug.CanonicalProbeResult, error) {
	results := make([]ladybug.CanonicalProbeResult, len(probes))
	for index, probe := range probes {
		results[index] = ladybug.CanonicalProbeResult{Probe: probe.Name, Rows: probe.MinRows, Passed: true, Detail: "fake probe passed"}
	}
	return results, nil
}

// fakeFailingProbes fails the last probe deterministically, whatever it is.
func fakeFailingProbes(_ context.Context, _ string, probes []ladybug.CanonicalProbe) ([]ladybug.CanonicalProbeResult, error) {
	results := make([]ladybug.CanonicalProbeResult, len(probes))
	for index, probe := range probes {
		if index == len(probes)-1 {
			results[index] = ladybug.CanonicalProbeResult{Probe: probe.Name, Rows: 0, Passed: false, Detail: "fake probe forced to fail"}
			continue
		}
		results[index] = ladybug.CanonicalProbeResult{Probe: probe.Name, Rows: probe.MinRows, Passed: true, Detail: "fake probe passed"}
	}
	return results, nil
}

// fakeIntegrityPassing stands in for ladybug.VerifyCanonicalIntegrity: every
// rule in CanonicalIntegrityRules reports zero violations, so the invariant
// gate never blocks a rebuild on its own.
func fakeIntegrityPassing(context.Context, string) (ladybug.CanonicalIntegrityReport, error) {
	rules := ladybug.CanonicalIntegrityRules()
	findings := make([]ladybug.IntegrityFinding, 0, len(rules))
	for _, rule := range rules {
		findings = append(findings, ladybug.IntegrityFinding{Rule: rule, Passed: true})
	}
	return ladybug.CanonicalIntegrityReport{Findings: findings, Passed: true}, nil
}

// fakeIntegrityWithViolation reports every rule passing except the one
// given, which carries a single sample violation. It forces an invariant
// failure deterministically, independent of what was actually loaded — the
// same role fakeCountsWithMismatch plays for table counts.
func fakeIntegrityWithViolation(rule ladybug.IntegrityRule, sample ladybug.IntegrityViolation) func(context.Context, string) (ladybug.CanonicalIntegrityReport, error) {
	return func(context.Context, string) (ladybug.CanonicalIntegrityReport, error) {
		rules := ladybug.CanonicalIntegrityRules()
		findings := make([]ladybug.IntegrityFinding, 0, len(rules))
		for _, candidate := range rules {
			if candidate != rule {
				findings = append(findings, ladybug.IntegrityFinding{Rule: candidate, Passed: true})
				continue
			}
			findings = append(findings, ladybug.IntegrityFinding{
				Rule:       rule,
				Violations: 1,
				Samples:    []ladybug.IntegrityViolation{sample},
				Passed:     false,
			})
		}
		return ladybug.CanonicalIntegrityReport{Findings: findings, Passed: false}, nil
	}
}

func canonicalNodeTableNames() map[string]bool {
	names := make(map[string]bool)
	for _, table := range ladybug.CanonicalNodeTables() {
		names[table.Name] = true
	}
	return names
}

func fakeCountsSidecar(databasePath string) string {
	return databasePath + ".counts.json"
}

func writeFakeCounts(databasePath string, tables map[string]int64) error {
	data, err := json.Marshal(tables)
	if err != nil {
		return err
	}
	return os.WriteFile(fakeCountsSidecar(databasePath), data, 0o600)
}

func readFakeCounts(databasePath string) (map[string]int64, error) {
	data, err := os.ReadFile(fakeCountsSidecar(databasePath))
	if err != nil {
		return nil, err
	}
	var tables map[string]int64
	if err := json.Unmarshal(data, &tables); err != nil {
		return nil, err
	}
	return tables, nil
}

func stageByName(stages []Stage, name StageName) (Stage, bool) {
	for _, stage := range stages {
		if stage.Name == name {
			return stage, true
		}
	}
	return Stage{}, false
}

func currentGenerationID(t *testing.T, root string) string {
	t.Helper()
	store, err := generation.New(root, generation.DefaultConfig())
	if err != nil {
		t.Fatalf("generation.New() error = %v", err)
	}
	current, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("store.Current() error = %v", err)
	}
	return current.ID
}

// TestRunPublishesHappyPathWithAllStages is the (a) contract: the happy path
// publishes, CURRENT points at the new generation, and every one of the
// eight stages is recorded, in contract order, all passed.
func TestRunPublishesHappyPathWithAllStages(t *testing.T) {
	root := t.TempDir()
	options := buildOptions(t, root, "000001", sampleFacts())

	report, err := Run(context.Background(), options)
	requireSpaceOrSkip(t, err)
	if err != nil {
		requireSpaceOrSkip(t, err)
		t.Fatalf("Run() error = %v", err)
	}
	if !report.Passed {
		t.Fatalf("Report.Passed = false, want true; stages=%+v", report.Stages)
	}
	if report.Publication.Generation.ID != "000001" {
		t.Fatalf("Publication.Generation.ID = %q, want 000001", report.Publication.Generation.ID)
	}

	wantOrder := []StageName{
		StageFacts, StageStaging, StageGraphNext, StageBulkLoad,
		StageIntegrity, StageSnapshot, StageProbes, StagePublish,
	}
	if len(report.Stages) != len(wantOrder) {
		t.Fatalf("len(Stages) = %d, want %d: %+v", len(report.Stages), len(wantOrder), report.Stages)
	}
	for index, stage := range report.Stages {
		if stage.Name != wantOrder[index] {
			t.Fatalf("Stages[%d].Name = %q, want %q", index, stage.Name, wantOrder[index])
		}
		if !stage.Passed {
			t.Fatalf("Stages[%d] (%s) did not pass: %+v", index, stage.Name, stage)
		}
	}

	if len(report.Probes) != 3 {
		t.Fatalf("len(Probes) = %d, want 3 golden probes for the sample fact set", len(report.Probes))
	}
	if len(report.SnapshotDigest) != 64 {
		t.Fatalf("SnapshotDigest = %q, want a 64 character sha256 hex digest", report.SnapshotDigest)
	}

	if got := currentGenerationID(t, root); got != "000001" {
		t.Fatalf("CURRENT = %q, want 000001", got)
	}

	digestBytes, err := os.ReadFile(filepath.Join(report.Publication.Generation.Path, snapshotFileName))
	if err != nil {
		t.Fatalf("read %s: %v", snapshotFileName, err)
	}
	if got := strings.TrimSpace(string(digestBytes)); got != report.SnapshotDigest {
		t.Fatalf("%s content = %q, want %q", snapshotFileName, got, report.SnapshotDigest)
	}
	if !report.Snapshot.Passed {
		t.Fatalf("Report.Snapshot.Passed = false, want true: %+v", report.Snapshot)
	}
	if report.Snapshot.Stats.Symbols == 0 {
		t.Fatalf("Report.Snapshot.Stats = %+v, want a non-empty snapshot built from the definitive graph", report.Snapshot.Stats)
	}
	if len(report.Snapshot.Digest) != 64 {
		t.Fatalf("Report.Snapshot.Digest = %q, want a 64 character sha256 hex digest", report.Snapshot.Digest)
	}
	snapshotStage, ok := stageByName(report.Stages, StageSnapshot)
	if !ok || !strings.Contains(snapshotStage.Detail, "hot snapshot") {
		t.Fatalf("snapshot stage Detail = %q, want it to mention the hot snapshot build", snapshotStage.Detail)
	}
}

// TestRunInvalidFactsAbortsWithoutTouchingDisk is the (b) contract: an
// invalid fact set fails at the facts stage and never opens the store.
func TestRunInvalidFactsAbortsWithoutTouchingDisk(t *testing.T) {
	root := t.TempDir()
	invalid := sampleFacts()
	invalid.Edges = append(invalid.Edges, facts.Edge{
		Kind: facts.References, SourceKey: "symbol:does-not-exist", TargetKey: invalid.Symbols[0].Key,
		Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest,
	})

	report, err := Run(context.Background(), buildOptions(t, root, "000001", invalid))
	requireSpaceOrSkip(t, err)
	if err == nil {
		t.Fatal("Run() error = nil, want an invalid facts error")
	}
	if !errors.Is(err, ErrRebuildFailed) {
		t.Fatalf("errors.Is(err, ErrRebuildFailed) = false; err = %v", err)
	}
	if report.Passed {
		t.Fatal("Report.Passed = true, want false")
	}
	if len(report.Stages) != 1 || report.Stages[0].Name != StageFacts || report.Stages[0].Passed {
		t.Fatalf("Stages = %+v, want a single failed facts stage", report.Stages)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatalf("ReadDir(root) error = %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("root is not empty after a rejected rebuild: %v", entries)
	}
}

// TestRunIntegrityMismatchPreservesCurrentGeneration is the (c) contract: an
// injected integrity discrepancy blocks publication and CURRENT keeps
// pointing at the previous generation.
func TestRunIntegrityMismatchPreservesCurrentGeneration(t *testing.T) {
	root := t.TempDir()
	set := sampleFacts()

	if _, err := Run(context.Background(), buildOptions(t, root, "000001", set)); err != nil {
		requireSpaceOrSkip(t, err)
		t.Fatalf("first Run() error = %v", err)
	}

	second := buildOptions(t, root, "000002", set)
	second.Counts = fakeCountsWithMismatch("Symbol", 1)

	report, err := Run(context.Background(), second)
	requireSpaceOrSkip(t, err)
	if err == nil {
		t.Fatal("Run() error = nil, want an integrity mismatch error")
	}
	if !errors.Is(err, ErrRebuildFailed) {
		t.Fatalf("errors.Is(err, ErrRebuildFailed) = false; err = %v", err)
	}
	if report.Passed {
		t.Fatal("Report.Passed = true, want false")
	}
	integrityStage, ok := stageByName(report.Stages, StageIntegrity)
	if !ok || integrityStage.Passed {
		t.Fatalf("integrity stage = %+v, want a recorded failure", integrityStage)
	}

	if got := currentGenerationID(t, root); got != "000001" {
		t.Fatalf("CURRENT = %q, want 000001 (unchanged)", got)
	}
	if _, err := os.Stat(filepath.Join(root, "generations", "000002")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("generation 000002 should not exist: stat err = %v", err)
	}
}

// TestRunInvariantViolationBlocksPublishDespiteMatchingCounts is the (a)
// contract from LUQUE-0904: a semantic invariant violation blocks
// publication and CURRENT keeps pointing at the previous generation, even
// though every canonical table count matches what the fact set implies.
func TestRunInvariantViolationBlocksPublishDespiteMatchingCounts(t *testing.T) {
	root := t.TempDir()
	set := sampleFacts()

	if _, err := Run(context.Background(), buildOptions(t, root, "000001", set)); err != nil {
		requireSpaceOrSkip(t, err)
		t.Fatalf("first Run() error = %v", err)
	}

	second := buildOptions(t, root, "000002", set)
	second.Integrity = fakeIntegrityWithViolation(ladybug.RuleDuplicateStableKey, ladybug.IntegrityViolation{
		Rule:   ladybug.RuleDuplicateStableKey,
		Table:  "Package",
		Key:    "pkg:go:acme/widgets:widgets",
		Detail: "also used by table File",
	})

	report, err := Run(context.Background(), second)
	requireSpaceOrSkip(t, err)
	if err == nil {
		t.Fatal("Run() error = nil, want an invariant violation error")
	}
	if !errors.Is(err, ErrRebuildFailed) {
		t.Fatalf("errors.Is(err, ErrRebuildFailed) = false; err = %v", err)
	}
	if report.Passed {
		t.Fatal("Report.Passed = true, want false")
	}
	integrityStage, ok := stageByName(report.Stages, StageIntegrity)
	if !ok || integrityStage.Passed {
		t.Fatalf("integrity stage = %+v, want a recorded failure", integrityStage)
	}
	for _, check := range report.Integrity {
		if !check.Passed {
			t.Fatalf("table count check %+v failed, want every count to still match: the failure must come from the invariant report alone", check)
		}
	}
	if report.Invariants.Passed {
		t.Fatal("Report.Invariants.Passed = true, want false")
	}
	if report.Invariants.Violations() != 1 {
		t.Fatalf("Report.Invariants.Violations() = %d, want 1", report.Invariants.Violations())
	}

	if got := currentGenerationID(t, root); got != "000001" {
		t.Fatalf("CURRENT = %q, want 000001 (unchanged)", got)
	}
	if _, err := os.Stat(filepath.Join(root, "generations", "000002")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("generation 000002 should not exist: stat err = %v", err)
	}
}

// TestIntegrityStageDetailNamesFailedInvariantRules is the (b) contract: the
// integrity stage Detail names every broken invariant rule, not just a
// violation count, so a failure is diagnosable without a second doctor
// graph run.
func TestIntegrityStageDetailNamesFailedInvariantRules(t *testing.T) {
	root := t.TempDir()
	options := buildOptions(t, root, "000001", sampleFacts())
	options.Integrity = fakeIntegrityWithViolation(ladybug.RuleUnknownConfidence, ladybug.IntegrityViolation{
		Rule:   ladybug.RuleUnknownConfidence,
		Table:  "CALLS_DIRECT",
		Key:    "symbol:go:acme/widgets.New->symbol:go:acme/widgets.Helper",
		Detail: `confidence "BOGUS" is not a known facts.Confidence`,
	})

	report, err := Run(context.Background(), options)
	requireSpaceOrSkip(t, err)
	if err == nil {
		t.Fatal("Run() error = nil, want an invariant violation error")
	}
	if !errors.Is(err, ErrRebuildFailed) {
		t.Fatalf("errors.Is(err, ErrRebuildFailed) = false; err = %v", err)
	}
	integrityStage, ok := stageByName(report.Stages, StageIntegrity)
	if !ok {
		t.Fatal("integrity stage not recorded")
	}
	if !strings.Contains(integrityStage.Detail, string(ladybug.RuleUnknownConfidence)) {
		t.Fatalf("integrity stage Detail = %q, want it to name %q", integrityStage.Detail, ladybug.RuleUnknownConfidence)
	}
}

// TestRunDefaultsIntegrityToLadybugVerifyCanonicalIntegrity confirms Options
// wires its Integrity default exactly like Load, Counts and Probes already
// do: a caller that leaves it nil reaches the real
// ladybug.VerifyCanonicalIntegrity, not a silently skipped check.
//
// The assertion cannot name the failure text, because it differs per build:
// the stub reports ErrUnavailable while the native build fails opening the
// path the fake loader never turned into a database. What both share is the
// ladybug error wrapper, and only ladybug can produce it here.
func TestRunDefaultsIntegrityToLadybugVerifyCanonicalIntegrity(t *testing.T) {
	root := t.TempDir()
	options := buildOptions(t, root, "000001", sampleFacts())
	options.Integrity = nil

	report, err := Run(context.Background(), options)
	requireSpaceOrSkip(t, err)
	if err == nil {
		t.Fatal("Run() error = nil, want the default ladybug.VerifyCanonicalIntegrity to reject a database the fake loader never wrote")
	}
	if !errors.Is(err, ErrRebuildFailed) {
		t.Fatalf("errors.Is(err, ErrRebuildFailed) = false; err = %v", err)
	}
	integrityStage, ok := stageByName(report.Stages, StageIntegrity)
	if !ok || integrityStage.Passed {
		t.Fatalf("integrity stage = %+v, want a recorded failure", integrityStage)
	}
	if !strings.HasPrefix(integrityStage.Detail, "ladybug ") {
		t.Fatalf("integrity stage Detail = %q, want a ladybug error: that prefix is what proves Options.Integrity defaulted to ladybug.VerifyCanonicalIntegrity", integrityStage.Detail)
	}
}

// TestRunFailingProbePreventsPublish is the (d) contract: a failing golden
// probe blocks publication even though integrity already passed.
func TestRunFailingProbePreventsPublish(t *testing.T) {
	root := t.TempDir()
	set := sampleFacts()

	if _, err := Run(context.Background(), buildOptions(t, root, "000001", set)); err != nil {
		requireSpaceOrSkip(t, err)
		t.Fatalf("first Run() error = %v", err)
	}

	second := buildOptions(t, root, "000002", set)
	second.Probes = fakeFailingProbes

	report, err := Run(context.Background(), second)
	requireSpaceOrSkip(t, err)
	if err == nil {
		t.Fatal("Run() error = nil, want a golden probe failure")
	}
	if !errors.Is(err, ErrRebuildFailed) {
		t.Fatalf("errors.Is(err, ErrRebuildFailed) = false; err = %v", err)
	}
	if report.Passed {
		t.Fatal("Report.Passed = true, want false")
	}
	probesStage, ok := stageByName(report.Stages, StageProbes)
	if !ok || probesStage.Passed {
		t.Fatalf("probes stage = %+v, want a recorded failure", probesStage)
	}
	integrityStage, ok := stageByName(report.Stages, StageIntegrity)
	if !ok || !integrityStage.Passed {
		t.Fatalf("integrity stage = %+v, want it to have passed before probes ran", integrityStage)
	}

	if got := currentGenerationID(t, root); got != "000001" {
		t.Fatalf("CURRENT = %q, want 000001 (unchanged)", got)
	}
}

// TestCanonicalSnapshotDigestIsDeterministic is the core of the (e) contract:
// the digest is a pure function of the per-table counts.
func TestCanonicalSnapshotDigestIsDeterministic(t *testing.T) {
	tablesA := map[string]int64{"Symbol": 2, "CALLS_DIRECT": 1, "Repository": 1}
	tablesB := map[string]int64{"Symbol": 2, "CALLS_DIRECT": 1, "Repository": 1}
	digestA := canonicalSnapshotDigest(tablesA)
	digestB := canonicalSnapshotDigest(tablesB)
	if digestA != digestB {
		t.Fatalf("digest differs for identical counts: %s != %s", digestA, digestB)
	}
	if len(digestA) != 64 {
		t.Fatalf("digest length = %d, want 64 (sha256 hex)", len(digestA))
	}

	tablesC := map[string]int64{"Symbol": 3, "CALLS_DIRECT": 1, "Repository": 1}
	if digestC := canonicalSnapshotDigest(tablesC); digestC == digestA {
		t.Fatal("digest did not change when a table count changed")
	}
}

// TestRunProducesDeterministicSnapshotDigestAcrossRuns is the (e) contract at
// the Run level: two rebuilds of the same fact set agree on SnapshotDigest.
func TestRunProducesDeterministicSnapshotDigestAcrossRuns(t *testing.T) {
	root := t.TempDir()
	set := sampleFacts()

	first, err := Run(context.Background(), buildOptions(t, root, "000001", set))
	requireSpaceOrSkip(t, err)
	if err != nil {
		requireSpaceOrSkip(t, err)
		t.Fatalf("first Run() error = %v", err)
	}
	second, err := Run(context.Background(), buildOptions(t, root, "000002", set))
	requireSpaceOrSkip(t, err)
	if err != nil {
		requireSpaceOrSkip(t, err)
		t.Fatalf("second Run() error = %v", err)
	}
	if first.SnapshotDigest != second.SnapshotDigest {
		t.Fatalf("digest changed across identical runs: %s != %s", first.SnapshotDigest, second.SnapshotDigest)
	}
	if first.Snapshot.Digest != second.Snapshot.Digest {
		t.Fatalf("hot snapshot digest changed across identical runs: %s != %s", first.Snapshot.Digest, second.Snapshot.Digest)
	}
	if first.Snapshot.Stats != second.Snapshot.Stats {
		t.Fatalf("hot snapshot stats changed across identical runs: %+v != %+v", first.Snapshot.Stats, second.Snapshot.Stats)
	}
}

// TestRunRequiresGenerationIDAndResolverVersion covers the point 3 defaults:
// both are validated before the store is ever touched.
func TestRunRequiresGenerationIDAndResolverVersion(t *testing.T) {
	root := t.TempDir()
	set := sampleFacts()

	missingID := buildOptions(t, root, "", set)
	if _, err := Run(context.Background(), missingID); !errors.Is(err, ErrRebuildFailed) {
		t.Fatalf("missing GenerationID: errors.Is(err, ErrRebuildFailed) = false; err = %v", err)
	}

	missingResolver := buildOptions(t, root, "000001", set)
	missingResolver.ResolverVersion = ""
	if _, err := Run(context.Background(), missingResolver); !errors.Is(err, ErrRebuildFailed) {
		t.Fatalf("missing ResolverVersion: errors.Is(err, ErrRebuildFailed) = false; err = %v", err)
	}
}

// TestRunProbesStagePassesWhenFactSetHasNothingToProbe defends the documented
// edge case: no symbols and no Symbol to Symbol edges is a pass, not a skip,
// and Probes must never even be called.
func TestRunProbesStagePassesWhenFactSetHasNothingToProbe(t *testing.T) {
	root := t.TempDir()
	set := facts.Set{
		Repositories: []facts.Repository{{Key: facts.RepositoryKey("acme/empty"), Name: "acme/empty", RootPath: "/repos/empty"}},
	}
	options := buildOptions(t, root, "000001", set)
	options.Probes = func(context.Context, string, []ladybug.CanonicalProbe) ([]ladybug.CanonicalProbeResult, error) {
		t.Fatal("Probes hook invoked even though the fact set has nothing to probe")
		return nil, nil
	}

	report, err := Run(context.Background(), options)
	requireSpaceOrSkip(t, err)
	if err != nil {
		requireSpaceOrSkip(t, err)
		t.Fatalf("Run() error = %v", err)
	}
	if !report.Passed {
		t.Fatalf("Report.Passed = false, want true; stages=%+v", report.Stages)
	}
	probesStage, ok := stageByName(report.Stages, StageProbes)
	if !ok || !probesStage.Passed {
		t.Fatalf("probes stage = %+v, want a pass", probesStage)
	}
	if probesStage.Detail == "" {
		t.Fatal("probes stage Detail is empty, want an explanation")
	}
}

// TestDeriveGoldenProbesIsDeterministicAndSymbolAnchored defends the probe
// derivation itself: repeatable, always anchored on a real symbol key, and
// EdgeTable is either empty (existence) or one of the Symbol to Symbol
// relations RunCanonicalProbes actually understands.
func TestDeriveGoldenProbesIsDeterministicAndSymbolAnchored(t *testing.T) {
	set := sampleFacts()
	first := deriveGoldenProbes(set)
	second := deriveGoldenProbes(set)
	if len(first) != 3 {
		t.Fatalf("len(probes) = %d, want 3 for the sample fact set", len(first))
	}
	if len(first) != len(second) {
		t.Fatalf("probe count changed across calls: %d != %d", len(first), len(second))
	}
	for index := range first {
		if first[index] != second[index] {
			t.Fatalf("probe %d differs across calls: %+v != %+v", index, first[index], second[index])
		}
	}
	for _, probe := range first {
		if probe.SymbolKey == "" {
			t.Fatalf("probe %+v has an empty SymbolKey", probe)
		}
		if probe.EdgeTable == "" {
			continue
		}
		if _, ok := symbolRelationKinds[facts.EdgeKind(probe.EdgeTable)]; !ok {
			t.Fatalf("probe %+v uses EdgeTable %q, which is not a Symbol to Symbol relation", probe, probe.EdgeTable)
		}
	}

	if probes := deriveGoldenProbes(facts.Set{}); probes != nil {
		t.Fatalf("deriveGoldenProbes(empty set) = %+v, want nil", probes)
	}
}

// TestRunSnapshotStageFailsAndPreservesCurrentGenerationWhenGraphCannotConvert
// is the (f) contract: a candidate whose definitive graph cannot become a
// HotSnapshot fails the snapshot stage and is never published.
func TestRunSnapshotStageFailsAndPreservesCurrentGenerationWhenGraphCannotConvert(t *testing.T) {
	root := t.TempDir()
	set := sampleFacts()

	if _, err := Run(context.Background(), buildOptions(t, root, "000001", set)); err != nil {
		requireSpaceOrSkip(t, err)
		t.Fatalf("first Run() error = %v", err)
	}

	broken := fakeCanonicalGraph()
	broken.Edges[len(broken.Edges)-1].Confidence = "BOGUS_CONFIDENCE"
	second := buildOptions(t, root, "000002", set)
	second.Scan = fixedScan(broken)

	report, err := Run(context.Background(), second)
	requireSpaceOrSkip(t, err)
	if err == nil {
		t.Fatal("Run() error = nil, want a hot snapshot build failure")
	}
	if !errors.Is(err, ErrRebuildFailed) {
		t.Fatalf("errors.Is(err, ErrRebuildFailed) = false; err = %v", err)
	}
	if report.Passed {
		t.Fatal("Report.Passed = true, want false")
	}
	snapshotStage, ok := stageByName(report.Stages, StageSnapshot)
	if !ok || snapshotStage.Passed {
		t.Fatalf("snapshot stage = %+v, want a recorded failure", snapshotStage)
	}
	if report.Snapshot.Passed {
		t.Fatalf("Report.Snapshot.Passed = true, want false: %+v", report.Snapshot)
	}

	if got := currentGenerationID(t, root); got != "000001" {
		t.Fatalf("CURRENT = %q, want 000001 (unchanged)", got)
	}
	if _, err := os.Stat(filepath.Join(root, "generations", "000002")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("generation 000002 should not exist: stat err = %v", err)
	}
}

// TestRunDefaultsScanToLadybugScanCanonical confirms Options wires its Scan
// default exactly like Load, Counts, Probes and Integrity already do: a
// caller that leaves it nil reaches the real ladybug.ScanCanonical, not a
// silently skipped snapshot build.
func TestRunDefaultsScanToLadybugScanCanonical(t *testing.T) {
	root := t.TempDir()
	options := buildOptions(t, root, "000001", sampleFacts())
	options.Scan = nil

	report, err := Run(context.Background(), options)
	requireSpaceOrSkip(t, err)
	if err == nil {
		t.Fatal("Run() error = nil, want the default ladybug.ScanCanonical to reject a database the fake loader never wrote")
	}
	if !errors.Is(err, ErrRebuildFailed) {
		t.Fatalf("errors.Is(err, ErrRebuildFailed) = false; err = %v", err)
	}
	snapshotStage, ok := stageByName(report.Stages, StageSnapshot)
	if !ok || snapshotStage.Passed {
		t.Fatalf("snapshot stage = %+v, want a recorded failure", snapshotStage)
	}
}

func TestRunRecordsMetrics(t *testing.T) {
	root := t.TempDir()
	set := sampleFacts()
	registry := metrics.NewRegistry()
	options := buildOptions(t, root, "000001", set)
	options.Metrics = registry

	report, err := Run(context.Background(), options)
	requireSpaceOrSkip(t, err)
	if err != nil {
		requireSpaceOrSkip(t, err)
		t.Fatalf("Run() error = %v", err)
	}
	if !report.Passed {
		t.Fatalf("report = %+v, want a passing rebuild", report)
	}

	observed := registry.Report()
	if observed.Index.Files != uint64(len(set.Files)) ||
		observed.Index.Symbols != uint64(len(set.Symbols)) ||
		observed.Index.Edges != uint64(len(set.Edges)) ||
		observed.Index.Unresolved != uint64(len(set.Unresolved)) ||
		observed.Index.Duration <= 0 {
		t.Fatalf("index metrics = %+v", observed.Index)
	}
	if observed.Ladybug.Transactions != 1 || observed.Ladybug.TransactionTotal <= 0 || observed.Ladybug.DatabaseBytes <= 0 {
		t.Fatalf("Ladybug metrics = %+v", observed.Ladybug)
	}
	if observed.Snapshot.ID != uint64(options.SnapshotID) ||
		observed.Snapshot.BuildDuration <= 0 ||
		observed.Snapshot.Bytes <= 0 {
		t.Fatalf("snapshot metrics = %+v", observed.Snapshot)
	}
}

// A rebuild must announce each stage as it starts, in pipeline order, so a
// caller can show which stage is running instead of a silent minute.
func TestRunReportsEveryStageAsItStarts(t *testing.T) {
	root := t.TempDir()
	options := buildOptions(t, root, "000001", sampleFacts())
	var announced []StageName
	options.Progress = func(stage StageName) {
		announced = append(announced, stage)
	}

	report, err := Run(context.Background(), options)
	requireSpaceOrSkip(t, err)
	if err != nil {
		requireSpaceOrSkip(t, err)
		t.Fatalf("Run() error = %v", err)
	}
	if !report.Passed {
		t.Fatalf("Run() report.Passed = false, stages = %#v", report.Stages)
	}

	want := []StageName{
		StageFacts, StagePublish, StageGraphNext, StageStaging,
		StageBulkLoad, StageSnapshot, StageIntegrity, StageProbes,
	}
	if len(announced) != len(want) {
		t.Fatalf("announced stages = %v, want %v", announced, want)
	}
	for index, stage := range want {
		if announced[index] != stage {
			t.Fatalf("announced[%d] = %q, want %q (all = %v)", index, announced[index], stage, announced)
		}
	}
}

// Progress is optional: a nil callback must not change the outcome.
func TestRunWithoutProgressCallbackStillPublishes(t *testing.T) {
	root := t.TempDir()
	options := buildOptions(t, root, "000001", sampleFacts())
	options.Progress = nil

	report, err := Run(context.Background(), options)
	requireSpaceOrSkip(t, err)
	if err != nil || !report.Passed {
		t.Fatalf("Run() = %v, passed = %v", err, report.Passed)
	}
}

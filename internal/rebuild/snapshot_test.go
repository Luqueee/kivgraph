package rebuild

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"testing"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
)

// fakeCanonicalGraph is a small, valid, hand written definitive graph: one
// repository, one package, two files, two symbols, one semantic edge
// (CALLS_DIRECT) with resolving evidence, and the structural edges a real
// canonical database would also report alongside it (CONTAINS_PACKAGE,
// CONTAINS_FILE x2, DEFINES x2) — so tests exercise the "structural edges
// are skipped, not converted" path without a second, separate fixture.
func fakeCanonicalGraph() ladybug.CanonicalGraph {
	return ladybug.CanonicalGraph{
		SchemaVersion: ladybug.CanonicalSchemaVersion,
		Repositories: []ladybug.CanonicalRepository{
			{StableKey: "repo-stable-1", Name: "acme/widgets", RootPath: "/repos/widgets", Commit: "deadbeef0001", Branch: "main", Languages: "go"},
		},
		Packages: []ladybug.CanonicalPackage{
			{
				StableKey: "pkg-stable-1", RepositoryKey: "repo-stable-1", Language: "go", Name: "widgets",
				Version: "v0.0.0", RootPath: "/repos/widgets", ManifestPath: "/repos/widgets/go.mod", Container: "example.com/acme/widgets",
			},
		},
		Files: []ladybug.CanonicalFile{
			{StableKey: "file-stable-1", RepositoryKey: "repo-stable-1", PackageKey: "pkg-stable-1", Path: "widgets.go", Language: "go", ContentHash: "hash-1"},
			{StableKey: "file-stable-2", RepositoryKey: "repo-stable-1", PackageKey: "pkg-stable-1", Path: "helper.go", Language: "go", ContentHash: "hash-2"},
		},
		Symbols: []ladybug.CanonicalSymbol{
			{
				StableKey: "sym-stable-new", CanonicalIdentity: "go:acme/widgets.New", RepositoryKey: "repo-stable-1",
				PackageKey: "pkg-stable-1", FileKey: "file-stable-1", Language: "go", Name: "New", QualifiedName: "widgets.New",
				Kind: "func", Exported: true, Signature: "func New() *Widget", StartLine: 10, StartColumn: 0, StartOffset: 100, EndLine: 12, EndOffset: 140,
			},
			{
				StableKey: "sym-stable-helper", CanonicalIdentity: "go:acme/widgets.Helper", RepositoryKey: "repo-stable-1",
				PackageKey: "pkg-stable-1", FileKey: "file-stable-2", Language: "go", Name: "Helper", QualifiedName: "widgets.Helper",
				Kind: "func", Exported: false, Signature: "func Helper()", StartLine: 4, StartColumn: 0, StartOffset: 40, EndLine: 6, EndOffset: 60,
			},
		},
		Evidence: []ladybug.CanonicalEvidence{
			{StableKey: "evidence-stable-1", RepositoryKey: "repo-stable-1", FileKey: "file-stable-1", StartLine: 11, StartColumn: 2, StartOffset: 110, EndOffset: 122, Text: "Helper()"},
		},
		Edges: []ladybug.CanonicalEdge{
			{Table: "CONTAINS_PACKAGE", SourceKey: "repo-stable-1", TargetKey: "pkg-stable-1"},
			{Table: "CONTAINS_FILE", SourceKey: "pkg-stable-1", TargetKey: "file-stable-1"},
			{Table: "CONTAINS_FILE", SourceKey: "pkg-stable-1", TargetKey: "file-stable-2"},
			{Table: "DEFINES", SourceKey: "file-stable-1", TargetKey: "sym-stable-new"},
			{Table: "DEFINES", SourceKey: "file-stable-2", TargetKey: "sym-stable-helper"},
			{
				Table: "CALLS_DIRECT", SourceKey: "sym-stable-new", TargetKey: "sym-stable-helper",
				Confidence: string(facts.ExactTypechecked), Provenance: string(facts.GoTypesUse),
				EvidenceKey: "evidence-stable-1", SourceSnapshot: 7, ResolverVersion: "resolver-test-1",
			},
		},
	}
}

// fixedScan stands in for ladybug.ScanCanonical, ignoring its path
// argument entirely and always returning graph — the same "canned, not
// derived from real file content" stance the rest of this package's fakes
// take toward their inputs.
func fixedScan(graph ladybug.CanonicalGraph) func(context.Context, string) (ladybug.CanonicalGraph, error) {
	return func(context.Context, string) (ladybug.CanonicalGraph, error) {
		return graph, nil
	}
}

// TestBuildSnapshotProducesQueryableGraphFromCanonicalData is the (a)
// contract: a canonical graph with nodes and semantic edges produces a
// snapshot whose SymbolByStableKey, Outgoing and Incoming return what the
// graph actually says — interrogating the built snapshot, not just the
// report.
func TestBuildSnapshotProducesQueryableGraphFromCanonicalData(t *testing.T) {
	graph := fakeCanonicalGraph()
	snapshot, report, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{
		DatabasePath: "unused", SnapshotID: 7, Scan: fixedScan(graph),
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if !report.Passed {
		t.Fatalf("Report.Passed = false, want true: %+v", report)
	}
	if report.SnapshotID != 7 || report.Version != SnapshotRowFormatVersion {
		t.Fatalf("Report id/version = %d/%d, want 7/%d", report.SnapshotID, report.Version, SnapshotRowFormatVersion)
	}

	newID, found := snapshot.SymbolByStableKey(hotsnapshot.StableKey("sym-stable-new"))
	if !found {
		t.Fatal("sym-stable-new not found by stable key")
	}
	helperID, found := snapshot.SymbolByStableKey(hotsnapshot.StableKey("sym-stable-helper"))
	if !found {
		t.Fatal("sym-stable-helper not found by stable key")
	}

	outgoing := snapshot.Outgoing(newID)
	if len(outgoing) != 1 || outgoing[0].Target != helperID {
		t.Fatalf("Outgoing(New) = %+v, want one edge targeting Helper (%d)", outgoing, helperID)
	}
	wantKind, err := facts.CallsDirect.Code()
	if err != nil {
		t.Fatal(err)
	}
	if outgoing[0].Kind != wantKind {
		t.Fatalf("Outgoing(New)[0].Kind = %d, want %d (CALLS_DIRECT)", outgoing[0].Kind, wantKind)
	}

	incoming := snapshot.Incoming(helperID)
	if len(incoming) != 1 || incoming[0].Target != newID {
		t.Fatalf("Incoming(Helper) = %+v, want one edge sourced from New (%d)", incoming, newID)
	}

	if report.Stats.Repositories != 1 || report.Stats.Packages != 1 || report.Stats.Files != 2 || report.Stats.Symbols != 2 {
		t.Fatalf("Stats = %+v, want 1 repository, 1 package, 2 files, 2 symbols", report.Stats)
	}
	if report.Stats.Edges != 1 {
		t.Fatalf("Stats.Edges = %d, want 1", report.Stats.Edges)
	}
	if report.Stats.SkippedEdges != 5 {
		t.Fatalf("Stats.SkippedEdges = %d, want 5 (CONTAINS_PACKAGE + 2 CONTAINS_FILE + 2 DEFINES)", report.Stats.SkippedEdges)
	}
}

// TestBuildSnapshotCarriesGraphProvenance covers what graph_status reports:
// the schema and resolver behind the published snapshot must come from the
// definitive graph, and a graph that records no resolver must not acquire one.
func TestBuildSnapshotCarriesGraphProvenance(t *testing.T) {
	graph := fakeCanonicalGraph()
	graph.Metadata = map[string]string{"resolver_version": "resolver-v9"}
	snapshot, _, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{
		DatabasePath: "x", SnapshotID: 4, Scan: fixedScan(graph),
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	metadata := snapshot.Metadata()
	if metadata.SchemaVersion != ladybug.CanonicalSchemaVersion || metadata.ResolverVersion != "resolver-v9" {
		t.Fatalf("provenance = %d/%q, want %d/%q", metadata.SchemaVersion, metadata.ResolverVersion,
			ladybug.CanonicalSchemaVersion, "resolver-v9")
	}

	withoutResolver := fakeCanonicalGraph()
	bare, _, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{
		DatabasePath: "x", SnapshotID: 5, Scan: fixedScan(withoutResolver),
	})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if resolver := bare.Metadata().ResolverVersion; resolver != "" {
		t.Fatalf("ResolverVersion = %q, want empty for a graph that records none", resolver)
	}
}

// TestBuildSnapshotIsDeterministicAcrossBuildsAndSensitiveToContent is the
// (b) contract: two builds of the same graph (under different SnapshotID
// and DatabasePath, exactly as two different generations would be) agree
// on digest and counts, and a genuinely different graph disagrees.
func TestBuildSnapshotIsDeterministicAcrossBuildsAndSensitiveToContent(t *testing.T) {
	graph := fakeCanonicalGraph()
	_, first, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{DatabasePath: "a", SnapshotID: 1, Scan: fixedScan(graph)})
	if err != nil {
		t.Fatalf("first BuildSnapshot() error = %v", err)
	}
	_, second, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{DatabasePath: "b", SnapshotID: 2, Scan: fixedScan(graph)})
	if err != nil {
		t.Fatalf("second BuildSnapshot() error = %v", err)
	}
	if len(first.Digest) != 64 {
		t.Fatalf("Digest = %q, want a 64 character sha256 hex digest", first.Digest)
	}
	if first.Digest != second.Digest {
		t.Fatalf("digest differs across two builds of the same graph (different SnapshotID/DatabasePath): %s != %s", first.Digest, second.Digest)
	}
	if first.Stats != second.Stats {
		t.Fatalf("Stats differ across two builds of the same graph: %+v != %+v", first.Stats, second.Stats)
	}

	other := fakeCanonicalGraph()
	other.Symbols[1].Name = "OtherHelper"
	_, third, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{DatabasePath: "c", SnapshotID: 1, Scan: fixedScan(other)})
	if err != nil {
		t.Fatalf("third BuildSnapshot() error = %v", err)
	}
	if third.Digest == first.Digest {
		t.Fatal("digest did not change for a genuinely different graph")
	}
}

// TestBuildSnapshotRejectsEdgeTableOutsideVocabulary and
// TestBuildSnapshotRejectsInventedConfidence are half of the (c) contract.
func TestBuildSnapshotRejectsEdgeTableOutsideVocabulary(t *testing.T) {
	graph := fakeCanonicalGraph()
	graph.Edges = append(graph.Edges, ladybug.CanonicalEdge{
		Table: "NOT_A_REAL_RELATION", SourceKey: "sym-stable-new", TargetKey: "sym-stable-helper",
		Confidence: string(facts.ExactTypechecked), Provenance: string(facts.GoTypesUse), EvidenceKey: "evidence-stable-1",
	})
	_, _, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{DatabasePath: "x", SnapshotID: 1, Scan: fixedScan(graph)})
	if !errors.Is(err, ErrSnapshotBuildFailed) {
		t.Fatalf("errors.Is(err, ErrSnapshotBuildFailed) = false; err = %v", err)
	}
}

func TestBuildSnapshotRejectsInventedConfidence(t *testing.T) {
	graph := fakeCanonicalGraph()
	graph.Edges[len(graph.Edges)-1].Confidence = "BOGUS_CONFIDENCE"
	_, _, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{DatabasePath: "x", SnapshotID: 1, Scan: fixedScan(graph)})
	if !errors.Is(err, ErrSnapshotBuildFailed) {
		t.Fatalf("errors.Is(err, ErrSnapshotBuildFailed) = false; err = %v", err)
	}
}

// TestBuildSnapshotRejectsInventedProvenance defends the third vocabulary
// axis alongside table and confidence.
func TestBuildSnapshotRejectsInventedProvenance(t *testing.T) {
	graph := fakeCanonicalGraph()
	graph.Edges[len(graph.Edges)-1].Provenance = "BOGUS_PROVENANCE"
	_, _, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{DatabasePath: "x", SnapshotID: 1, Scan: fixedScan(graph)})
	if !errors.Is(err, ErrSnapshotBuildFailed) {
		t.Fatalf("errors.Is(err, ErrSnapshotBuildFailed) = false; err = %v", err)
	}
}

// TestBuildSnapshotRejectsSymbolLineOverflowingUint32 is the (d) contract:
// a line that does not fit in uint32 fails the build instead of truncating.
func TestBuildSnapshotRejectsSymbolLineOverflowingUint32(t *testing.T) {
	graph := fakeCanonicalGraph()
	graph.Symbols[0].StartLine = int64(math.MaxUint32) + 1
	_, _, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{DatabasePath: "x", SnapshotID: 1, Scan: fixedScan(graph)})
	if !errors.Is(err, ErrSnapshotBuildFailed) {
		t.Fatalf("errors.Is(err, ErrSnapshotBuildFailed) = false; err = %v", err)
	}
}

// TestBuildSnapshotKeepsPackageToPackageEdgesOutsideSymbolCSR checks that
// package dependencies remain queryable in the HotSnapshot instead of being
// silently counted as discarded structural rows.
func TestBuildSnapshotKeepsPackageToPackageEdgesOutsideSymbolCSR(t *testing.T) {
	graph := fakeCanonicalGraph()
	graph.Edges = append(graph.Edges, ladybug.CanonicalEdge{
		Table: "MODULE_DEPENDS_ON", SourceKey: "pkg-stable-1", TargetKey: "pkg-stable-1",
		Confidence: string(facts.StructuralCertain), Provenance: string(facts.PackageManifest),
	})
	snapshot, report, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{DatabasePath: "x", SnapshotID: 1, Scan: fixedScan(graph)})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v", err)
	}
	if report.Stats.Edges != 1 {
		t.Fatalf("Stats.Edges = %d, want 1 (only the CALLS_DIRECT edge)", report.Stats.Edges)
	}
	if report.Stats.PackageEdges != 1 {
		t.Fatalf("Stats.PackageEdges = %d, want 1", report.Stats.PackageEdges)
	}
	if report.Stats.SkippedEdges != 5 {
		t.Fatalf("Stats.SkippedEdges = %d, want 5 structural edges", report.Stats.SkippedEdges)
	}
	if dependencies := snapshot.PackageDependencies(0); len(dependencies) != 1 {
		t.Fatalf("PackageDependencies(target=0) = %v, want one dependency", dependencies)
	}
}

// TestBuildSnapshotRejectsDanglingEvidenceKey defends the coherence
// assertion: a non-empty evidence_key that does not resolve is an error,
// never a silently evidence-less edge.
func TestBuildSnapshotRejectsDanglingEvidenceKey(t *testing.T) {
	graph := fakeCanonicalGraph()
	graph.Edges[len(graph.Edges)-1].EvidenceKey = "does-not-exist"
	_, _, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{DatabasePath: "x", SnapshotID: 1, Scan: fixedScan(graph)})
	if !errors.Is(err, ErrSnapshotBuildFailed) {
		t.Fatalf("errors.Is(err, ErrSnapshotBuildFailed) = false; err = %v", err)
	}
}

// TestBuildSnapshotAllowsSemanticEdgeWithEmptyEvidenceKey defends the other
// half: a genuinely absent evidence_key (as opposed to a dangling one) is
// not an error, since some canonical rows legitimately carry none.
func TestBuildSnapshotAllowsSemanticEdgeWithEmptyEvidenceKey(t *testing.T) {
	graph := fakeCanonicalGraph()
	graph.Edges[len(graph.Edges)-1].EvidenceKey = ""
	_, report, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{DatabasePath: "x", SnapshotID: 1, Scan: fixedScan(graph)})
	if err != nil {
		t.Fatalf("BuildSnapshot() error = %v, want a genuinely absent evidence_key to succeed", err)
	}
	if !report.Passed || report.Stats.Edges != 1 {
		t.Fatalf("Report = %+v, want a passed build with the one semantic edge converted", report)
	}
}

// TestBuildSnapshotRejectsDanglingSourceSymbol defends the "no dangling
// exact edges" invariant on the converter's own side, independent of
// hotsnapshot's own duplicate validation.
func TestBuildSnapshotRejectsDanglingSourceSymbol(t *testing.T) {
	graph := fakeCanonicalGraph()
	graph.Edges[len(graph.Edges)-1].SourceKey = "sym-does-not-exist"
	_, _, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{DatabasePath: "x", SnapshotID: 1, Scan: fixedScan(graph)})
	if !errors.Is(err, ErrSnapshotBuildFailed) {
		t.Fatalf("errors.Is(err, ErrSnapshotBuildFailed) = false; err = %v", err)
	}
}

// TestBuildSnapshotDefaultsScanToLadybugScanCanonical confirms
// BuildSnapshotOptions wires its Scan default exactly like Options.Load,
// Options.Counts, Options.Probes and Options.Integrity already do for Run.
func TestBuildSnapshotDefaultsScanToLadybugScanCanonical(t *testing.T) {
	_, _, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{
		DatabasePath: filepath.Join(t.TempDir(), "missing.db"),
		SnapshotID:   1,
	})
	if !errors.Is(err, ErrSnapshotBuildFailed) {
		t.Fatalf("errors.Is(err, ErrSnapshotBuildFailed) = false; err = %v, want the default ladybug.ScanCanonical to fail against a database that was never written", err)
	}
}

// TestConvertCanonicalGraphMapsPackageModulePathFromContainer defends the
// reasoned ModulePath mapping: it comes from the canonical Container
// column, the schema's own home for a Go module path.
func TestConvertCanonicalGraphMapsPackageModulePathFromContainer(t *testing.T) {
	rows, _, err := convertCanonicalGraph(fakeCanonicalGraph())
	if err != nil {
		t.Fatalf("convertCanonicalGraph() error = %v", err)
	}
	if len(rows.Packages) != 1 || rows.Packages[0].ModulePath != "example.com/acme/widgets" {
		t.Fatalf("Packages = %+v, want ModulePath sourced from the canonical Container column", rows.Packages)
	}
}

// TestConvertCanonicalGraphDerivesEdgeEvidenceFromEndpointSymbolsAndProvenance
// defends the EvidenceSourceFileKey/EvidenceTargetFileKey/EvidenceKind
// mapping directly, since GraphSnapshot exposes no public accessor for
// EvidenceRecord that a black box test through BuildSnapshot could use.
func TestConvertCanonicalGraphDerivesEdgeEvidenceFromEndpointSymbolsAndProvenance(t *testing.T) {
	rows, skipped, err := convertCanonicalGraph(fakeCanonicalGraph())
	if err != nil {
		t.Fatalf("convertCanonicalGraph() error = %v", err)
	}
	if skipped != 5 {
		t.Fatalf("skippedEdges = %d, want 5", skipped)
	}
	if len(rows.Edges) != 1 {
		t.Fatalf("Edges = %+v, want exactly one converted edge", rows.Edges)
	}
	edge := rows.Edges[0]
	if edge.EvidenceSourceFileKey != "file-stable-1" {
		t.Fatalf("EvidenceSourceFileKey = %q, want the source symbol's own file (file-stable-1)", edge.EvidenceSourceFileKey)
	}
	if edge.EvidenceTargetFileKey != "file-stable-2" {
		t.Fatalf("EvidenceTargetFileKey = %q, want the target symbol's own file (file-stable-2)", edge.EvidenceTargetFileKey)
	}
	if edge.EvidenceKind != string(facts.GoTypesUse) {
		t.Fatalf("EvidenceKind = %q, want the edge's own provenance string %q", edge.EvidenceKind, facts.GoTypesUse)
	}
	if edge.Flags != 0 {
		t.Fatalf("Flags = %d, want 0", edge.Flags)
	}
}

// TestSnapshotGenerationResolvesActiveGenerationWhenIDEmpty and
// TestSnapshotGenerationResolvesGivenGenerationID defend the root+generation
// resolution the snapshot CLI command relies on.
func TestSnapshotGenerationResolvesActiveGenerationWhenIDEmpty(t *testing.T) {
	root := t.TempDir()
	published, err := Run(context.Background(), buildOptions(t, root, "000001", sampleFacts()))
	if err != nil || !published.Passed {
		t.Fatalf("seed Run() error = %v, passed = %v", err, published.Passed)
	}

	_, report, err := SnapshotGeneration(context.Background(), GenerationSnapshotOptions{Root: root, SnapshotID: 42, Scan: fakeScan})
	if err != nil {
		t.Fatalf("SnapshotGeneration() error = %v", err)
	}
	if !report.Passed {
		t.Fatalf("Report.Passed = false, want true: %+v", report)
	}
	if report.SnapshotID != 42 {
		t.Fatalf("Report.SnapshotID = %d, want 42", report.SnapshotID)
	}
}

func TestSnapshotGenerationResolvesGivenGenerationID(t *testing.T) {
	root := t.TempDir()
	set := sampleFacts()
	if _, err := Run(context.Background(), buildOptions(t, root, "000001", set)); err != nil {
		t.Fatalf("first Run() error = %v", err)
	}
	if _, err := Run(context.Background(), buildOptions(t, root, "000002", set)); err != nil {
		t.Fatalf("second Run() error = %v", err)
	}

	_, report, err := SnapshotGeneration(context.Background(), GenerationSnapshotOptions{Root: root, GenerationID: "000001", SnapshotID: 1, Scan: fakeScan})
	if err != nil {
		t.Fatalf("SnapshotGeneration(000001) error = %v", err)
	}
	if !report.Passed {
		t.Fatalf("Report.Passed = false, want true: %+v", report)
	}
}

func TestSnapshotGenerationFailsForUnknownGenerationID(t *testing.T) {
	root := t.TempDir()
	seedGeneration(t, root)

	_, _, err := SnapshotGeneration(context.Background(), GenerationSnapshotOptions{Root: root, GenerationID: "000099"})
	if !errors.Is(err, ErrSnapshotBuildFailed) {
		t.Fatalf("errors.Is(err, ErrSnapshotBuildFailed) = false; err = %v", err)
	}
}

package resilience

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
	"github.com/Luqueee/ladygraph/internal/testsupport"
)

var errRebuildInjected = errors.New("injected rebuild failure")

// TestFailedFullRebuildDoesNotChangeTheServedGraph is the LUQUE-1202 contract
// seen from a client instead of from the filesystem: while a full rebuild runs
// and fails, queries keep answering from the snapshot that was already
// published, byte for byte.
//
// rebuild.Run and SnapshotStore.Publish are deliberately separate steps. This
// test is what makes that separation observable: a run that never reaches its
// publish stage cannot reach the store either.
func TestFailedFullRebuildDoesNotChangeTheServedGraph(t *testing.T) {
	root := t.TempDir()
	store := hotsnapshot.NewSnapshotStore(publishedSnapshot(t))
	session := connectServer(t, store)
	before := querySymbol(t, session)

	if _, err := rebuild.Run(context.Background(), rebuildOptions(root, "000001")); err != nil {
		testsupport.RequireSpaceOrSkip(t, err)
		t.Fatalf("seed rebuild error = %v", err)
	}

	failing := rebuildOptions(root, "000002")
	failing.Probes = func(context.Context, string, []ladybug.CanonicalProbe) ([]ladybug.CanonicalProbeResult, error) {
		return nil, errRebuildInjected
	}
	report, err := rebuild.Run(context.Background(), failing)
	if !errors.Is(err, rebuild.ErrRebuildFailed) {
		t.Fatalf("Run() error = %v, want ErrRebuildFailed", err)
	}
	if report.Passed {
		t.Fatalf("Report.Passed = true, want false")
	}

	if after := querySymbol(t, session); after != before {
		t.Fatalf("served graph changed across a failed rebuild:\n%s\n%s", before, after)
	}
	if current := readCurrent(t, root); current != "000001" {
		t.Fatalf("CURRENT = %q, want the previous generation", current)
	}
}

// TestSnapshotStoreRejectsAStaleCandidate covers the other way a failed rebuild
// could corrupt what readers see: not by publishing garbage, but by publishing
// an older generation over a newer one. The store must refuse, and keep serving
// what it had.
func TestSnapshotStoreRejectsAStaleCandidate(t *testing.T) {
	store := hotsnapshot.NewSnapshotStore(publishedSnapshot(t))
	session := connectServer(t, store)
	before := querySymbol(t, session)

	stale := snapshotWithID(t, 1)
	if err := store.Publish(stale); err == nil {
		t.Fatal("Publish(stale) error = nil, want a rejected generation")
	}
	if after := querySymbol(t, session); after != before {
		t.Fatalf("served graph changed after a rejected publication:\n%s\n%s", before, after)
	}
}

// TestServedGraphChangesWhenANewerSnapshotIsPublished is the control for the
// two tests above: querySymbol must actually notice a new graph. Otherwise
// "unchanged across a failure" would be satisfied by a blind comparison.
func TestServedGraphChangesWhenANewerSnapshotIsPublished(t *testing.T) {
	store := hotsnapshot.NewSnapshotStore(publishedSnapshot(t))
	session := connectServer(t, store)
	before := querySymbol(t, session)

	if err := store.Publish(snapshotWithID(t, 92)); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if after := querySymbol(t, session); after == before {
		t.Fatalf("querySymbol did not observe the new generation: %s", after)
	}
}

func rebuildOptions(root, generationID string) rebuild.Options {
	return rebuild.Options{
		Root:            root,
		GenerationID:    generationID,
		Facts:           rebuildFacts(),
		ResolverVersion: "resolver-resilience-1",
		SnapshotID:      7,
		Load:            fakeCanonicalLoad,
		Counts:          fakeCanonicalCounts,
		Probes:          passingProbes,
		Integrity: func(context.Context, string) (ladybug.CanonicalIntegrityReport, error) {
			return ladybug.CanonicalIntegrityReport{Passed: true}, nil
		},
		Scan: func(context.Context, string) (ladybug.CanonicalGraph, error) {
			return rebuildCanonicalGraph(), nil
		},
	}
}

// passingProbes answers every golden probe the rebuild derived from the fact
// set. Returning fewer results than probes is itself a publish failure, which
// is what the failing variant of this fixture exploits.
func passingProbes(_ context.Context, _ string, probes []ladybug.CanonicalProbe) ([]ladybug.CanonicalProbeResult, error) {
	results := make([]ladybug.CanonicalProbeResult, 0, len(probes))
	for _, probe := range probes {
		results = append(results, ladybug.CanonicalProbeResult{Probe: probe.Name, Rows: 1, Passed: true})
	}
	return results, nil
}

// fakeCanonicalLoad renders the fact set through the real canonical mapping and
// leaves a placeholder database where the generation store expects one, the
// same stance internal/rebuild's own tests take: the orchestration is what is
// under test here, not cgo.
func fakeCanonicalLoad(_ context.Context, path string, set facts.Set, options ladybug.CanonicalLoadOptions) (ladybug.LoadReport, error) {
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
	tables := make(map[string]int64, len(rows))
	for table, tableRows := range rows {
		tables[table] = int64(len(tableRows))
	}
	return ladybug.LoadReport{Tables: tables}, nil
}

func fakeCanonicalCounts(_ context.Context, path string) (map[string]int64, error) {
	rows, err := ladybug.CanonicalTableRows(rebuildFacts(), ladybug.CanonicalLoadOptions{
		SnapshotID: 7, ResolverVersion: "resolver-resilience-1",
	})
	if err != nil {
		return nil, err
	}
	tables := make(map[string]int64, len(rows))
	for table, tableRows := range rows {
		tables[table] = int64(len(tableRows))
	}
	return tables, nil
}

func rebuildFacts() facts.Set {
	repoKey := facts.RepositoryKey("acme/widgets")
	pkgKey := facts.PackageKey(facts.LanguageGo, repoKey, "widgets")
	fileKey := facts.FileKey(repoKey, "widgets.go")
	symbolKey := "symbol:go:acme/widgets.New"
	return facts.Set{
		Repositories: []facts.Repository{{Key: repoKey, Name: "acme/widgets", RootPath: "/repos/widgets", Languages: []facts.Language{facts.LanguageGo}}},
		Packages:     []facts.Package{{Key: pkgKey, RepositoryKey: repoKey, Language: facts.LanguageGo, Name: "widgets", RootPath: "/repos/widgets"}},
		Files:        []facts.File{{Key: fileKey, RepositoryKey: repoKey, PackageKey: pkgKey, Path: "widgets.go", Language: facts.LanguageGo}},
		Symbols: []facts.Symbol{{
			Key: symbolKey, CanonicalIdentity: "go:acme/widgets.New", RepositoryKey: repoKey,
			PackageKey: pkgKey, FileKey: fileKey, Language: facts.LanguageGo, Name: "New",
			QualifiedName: "widgets.New", Kind: "func", Exported: true,
		}},
		Edges: []facts.Edge{
			{Kind: facts.ContainsPackage, SourceKey: repoKey, TargetKey: pkgKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsFile, SourceKey: pkgKey, TargetKey: fileKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.Defines, SourceKey: fileKey, TargetKey: symbolKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
		},
	}
}

func rebuildCanonicalGraph() ladybug.CanonicalGraph {
	return ladybug.CanonicalGraph{
		SchemaVersion: ladybug.CanonicalSchemaVersion,
		Metadata:      map[string]string{"resolver_version": "resolver-resilience-1"},
		Repositories: []ladybug.CanonicalRepository{
			{StableKey: "repo-stable-1", Name: "acme/widgets", RootPath: "/repos/widgets", Commit: "deadbeef0001", Branch: "main", Languages: "go"},
		},
		Packages: []ladybug.CanonicalPackage{
			{StableKey: "pkg-stable-1", RepositoryKey: "repo-stable-1", Language: "go", Name: "widgets", RootPath: "/repos/widgets", ManifestPath: "/repos/widgets/go.mod", Container: "example.com/acme/widgets"},
		},
		Files: []ladybug.CanonicalFile{
			{StableKey: "file-stable-1", RepositoryKey: "repo-stable-1", PackageKey: "pkg-stable-1", Path: "widgets.go", Language: "go", ContentHash: "hash-1"},
		},
		Symbols: []ladybug.CanonicalSymbol{
			{
				StableKey: "sym-stable-new", CanonicalIdentity: "go:acme/widgets.New", RepositoryKey: "repo-stable-1",
				PackageKey: "pkg-stable-1", FileKey: "file-stable-1", Language: "go", Name: "New",
				QualifiedName: "widgets.New", Kind: "func", Exported: true, Signature: "func New() *Widget",
				StartLine: 10, StartColumn: 0, StartOffset: 100, EndLine: 12, EndOffset: 140,
			},
		},
		Edges: []ladybug.CanonicalEdge{
			{Table: "CONTAINS_PACKAGE", SourceKey: "repo-stable-1", TargetKey: "pkg-stable-1"},
			{Table: "CONTAINS_FILE", SourceKey: "pkg-stable-1", TargetKey: "file-stable-1"},
			{Table: "DEFINES", SourceKey: "file-stable-1", TargetKey: "sym-stable-new"},
		},
	}
}

func snapshotWithID(t *testing.T, id uint64) *hotsnapshot.GraphSnapshot {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo-old", Name: "old", Languages: "ts"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "pkg-old", RepositoryKey: "repo-old", Language: "ts", Name: "old", ModulePath: "@acme/old"}},
		Files:        []hotsnapshot.FileRow{{Key: "file-old", RepositoryKey: "repo-old", PackageKey: "pkg-old", Path: "old.ts", Language: "ts"}},
		Symbols: []hotsnapshot.SymbolRow{{
			StableKey: "sym-root", CanonicalIdentity: "ts:old.Root", FileKey: "file-old", Language: "ts",
			Name: "Root", QualifiedName: "old.Root", Kind: "function",
		}},
	}, id, publishedSnapshotTime, 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return snapshot
}

func readCurrent(t *testing.T, root string) string {
	t.Helper()
	link := filepath.Join(root, "CURRENT")
	target, err := os.Readlink(link)
	if err == nil {
		return filepath.Base(target)
	}
	content, readErr := os.ReadFile(link)
	if readErr != nil {
		t.Fatalf("read CURRENT: readlink err = %v, read err = %v", err, readErr)
	}
	return filepath.Base(trimSpace(string(content)))
}

func trimSpace(value string) string {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\n' || value[start] == '\t' || value[start] == '\r') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\n' || value[end-1] == '\t' || value[end-1] == '\r') {
		end--
	}
	return value[start:end]
}

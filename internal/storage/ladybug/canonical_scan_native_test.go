//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/facts"
)

const (
	scanFixtureSnapshotID       = int64(42)
	scanFixtureResolverVersion  = "canonical-scan-test-resolver-v1"
	scanFixtureSymbolMainKey    = "symbol:repoA:main.go:Main"
	scanFixtureSymbolSetupKey   = "symbol:repoA:main.go:setup"
	scanFixtureSymbolHelperKey  = "symbol:repoA:lib.go:Helper"
	scanFixtureSymbolProcessKey = "symbol:repoA:lib.go:process"
	scanFixtureSymbolAppKey     = "symbol:repoB:app.ts:App"
	scanFixtureSymbolBaseKey    = "symbol:repoB:app.ts:Base"
)

// canonicalScanFixtureSet builds a self contained fact set covering every
// shape ScanCanonical must round trip: two repositories, three packages,
// three files, six symbols, two evidence rows, two unresolved references,
// every containment edge kind, and five distinct semantic edge classes --
// PACKAGE_DEPENDS_ON (a Package -> Package edge), REFERENCES, CALLS_DIRECT,
// TYPE_USES and EXTENDS -- with a deliberate mix of populated and empty
// optional fields (Package.Version, Package.Container, File.ContentHash,
// Symbol.Signature, and an edge with no EvidenceKey) so the NULL round trip
// tupleOptionalString depends on is actually exercised, not just the happy
// path.
func canonicalScanFixtureSet(t *testing.T) facts.Set {
	t.Helper()

	repoAKey := facts.RepositoryKey("scan-repoA")
	repoBKey := facts.RepositoryKey("scan-repoB")
	pkgA1Key := facts.PackageKey(facts.LanguageGo, repoAKey, "main")
	pkgA2Key := facts.PackageKey(facts.LanguageGo, repoAKey, "lib")
	pkgBKey := facts.PackageKey(facts.LanguageTypeScript, repoBKey, "app")
	fileMainKey := facts.FileKey(repoAKey, "main.go")
	fileLibKey := facts.FileKey(repoAKey, "lib/helper.go")
	fileAppKey := facts.FileKey(repoBKey, "app.ts")
	evidenceRefKey := facts.EvidenceKey(fileMainKey, 20, 30)
	evidenceCallKey := facts.EvidenceKey(fileMainKey, 40, 55)

	set := facts.Set{
		Repositories: []facts.Repository{
			{
				Key: repoAKey, Name: "scan-repoA", RootPath: "/repos/scan-repoA", Commit: "commit-a", Branch: "main",
				Dirty: true, Languages: []facts.Language{facts.LanguageGo},
			},
			{
				// Languages listed reversed on purpose: joinLanguages sorts,
				// so this is the only way to exercise that the scan reflects
				// the stored, already-sorted string, not discovery order.
				Key: repoBKey, Name: "scan-repoB", RootPath: "/repos/scan-repoB", Commit: "commit-b", Branch: "develop",
				Dirty: false, Languages: []facts.Language{facts.LanguageTypeScript, facts.LanguageGo},
			},
		},
		Packages: []facts.Package{
			{
				Key: pkgA1Key, RepositoryKey: repoAKey, Language: facts.LanguageGo, Name: "main",
				RootPath: "/repos/scan-repoA", ManifestPath: "/repos/scan-repoA/go.mod", Container: "github.com/example/scan-repoA",
				// Version left empty deliberately.
			},
			{
				Key: pkgA2Key, RepositoryKey: repoAKey, Language: facts.LanguageGo, Name: "lib",
				RootPath: "/repos/scan-repoA/lib", ManifestPath: "/repos/scan-repoA/go.mod", Container: "github.com/example/scan-repoA",
			},
			{
				Key: pkgBKey, RepositoryKey: repoBKey, Language: facts.LanguageTypeScript, Name: "app", Version: "1.2.3",
				RootPath: "/repos/scan-repoB", ManifestPath: "/repos/scan-repoB/package.json",
				// Container left empty deliberately: only Go packages have one.
			},
		},
		Files: []facts.File{
			{Key: fileMainKey, RepositoryKey: repoAKey, PackageKey: pkgA1Key, Path: "main.go", Language: facts.LanguageGo, ContentHash: "hash-main"},
			{
				Key: fileLibKey, RepositoryKey: repoAKey, PackageKey: pkgA2Key, Path: "lib/helper.go", Language: facts.LanguageGo,
				Generated: true,
				// ContentHash left empty deliberately.
			},
			{Key: fileAppKey, RepositoryKey: repoBKey, PackageKey: pkgBKey, Path: "app.ts", Language: facts.LanguageTypeScript, ContentHash: "hash-app"},
		},
		Symbols: []facts.Symbol{
			{
				Key: scanFixtureSymbolMainKey, CanonicalIdentity: "go:scan-repoA:main.Main", RepositoryKey: repoAKey, PackageKey: pkgA1Key, FileKey: fileMainKey,
				Language: facts.LanguageGo, Name: "Main", QualifiedName: "main.Main", Kind: "function", Exported: true, Signature: "func Main()",
				Start: facts.Position{Line: 1, Column: 0, Offset: 0}, End: facts.Position{Line: 6, Column: 1, Offset: 90},
			},
			{
				Key: scanFixtureSymbolSetupKey, CanonicalIdentity: "go:scan-repoA:main.setup", RepositoryKey: repoAKey, PackageKey: pkgA1Key, FileKey: fileMainKey,
				Language: facts.LanguageGo, Name: "setup", QualifiedName: "main.setup", Kind: "function", Exported: false,
				// Signature left empty deliberately.
				Start: facts.Position{Line: 8, Column: 0, Offset: 95}, End: facts.Position{Line: 10, Column: 1, Offset: 130},
			},
			{
				Key: scanFixtureSymbolHelperKey, CanonicalIdentity: "go:scan-repoA:lib.Helper", RepositoryKey: repoAKey, PackageKey: pkgA2Key, FileKey: fileLibKey,
				Language: facts.LanguageGo, Name: "Helper", QualifiedName: "lib.Helper", Kind: "function", Exported: true, Signature: "func Helper()",
				Start: facts.Position{Line: 1, Column: 0, Offset: 0}, End: facts.Position{Line: 3, Column: 1, Offset: 40},
			},
			{
				Key: scanFixtureSymbolProcessKey, CanonicalIdentity: "go:scan-repoA:lib.process", RepositoryKey: repoAKey, PackageKey: pkgA2Key, FileKey: fileLibKey,
				Language: facts.LanguageGo, Name: "process", QualifiedName: "lib.process", Kind: "function", Exported: false, Signature: "func process()",
				Start: facts.Position{Line: 5, Column: 0, Offset: 45}, End: facts.Position{Line: 7, Column: 1, Offset: 90},
			},
			{
				Key: scanFixtureSymbolAppKey, CanonicalIdentity: "ts:scan-repoB:app.App", RepositoryKey: repoBKey, PackageKey: pkgBKey, FileKey: fileAppKey,
				Language: facts.LanguageTypeScript, Name: "App", QualifiedName: "app.App", Kind: "class", Exported: true, Signature: "class App",
				Start: facts.Position{Line: 1, Column: 0, Offset: 0}, End: facts.Position{Line: 12, Column: 1, Offset: 180},
			},
			{
				Key: scanFixtureSymbolBaseKey, CanonicalIdentity: "ts:scan-repoB:app.Base", RepositoryKey: repoBKey, PackageKey: pkgBKey, FileKey: fileAppKey,
				Language: facts.LanguageTypeScript, Name: "Base", QualifiedName: "app.Base", Kind: "class", Exported: true, Signature: "class Base",
				Start: facts.Position{Line: 14, Column: 0, Offset: 182}, End: facts.Position{Line: 18, Column: 1, Offset: 230},
			},
		},
		Evidence: []facts.Evidence{
			{
				Key: evidenceRefKey, RepositoryKey: repoAKey, FileKey: fileMainKey,
				Start: facts.Position{Line: 2, Column: 4, Offset: 20}, End: facts.Position{Line: 2, Column: 14, Offset: 30}, Text: "Helper()",
			},
			{
				Key: evidenceCallKey, RepositoryKey: repoAKey, FileKey: fileMainKey,
				Start: facts.Position{Line: 3, Column: 4, Offset: 40}, End: facts.Position{Line: 3, Column: 19, Offset: 55}, Text: "process()",
			},
		},
		Unresolved: []facts.UnresolvedReference{
			{
				RepositoryKey: repoBKey, FileKey: fileAppKey, Language: facts.LanguageTypeScript, SourceSymbolKey: scanFixtureSymbolAppKey,
				RequestedPackage: "lodash", RequestedSymbol: "debounce", Reason: "package_not_indexed", Detail: "lodash is not part of this workspace",
				Start: facts.Position{Line: 20, Column: 2, Offset: 300},
			},
			{
				RepositoryKey: repoAKey, FileKey: fileMainKey, Language: facts.LanguageGo, SourceSymbolKey: scanFixtureSymbolMainKey,
				Reason: "syntax_error", Start: facts.Position{Line: 25, Column: 0, Offset: 400},
			},
		},
		Edges: []facts.Edge{
			// Containment.
			{Kind: facts.ContainsPackage, SourceKey: repoAKey, TargetKey: pkgA1Key, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsPackage, SourceKey: repoAKey, TargetKey: pkgA2Key, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsPackage, SourceKey: repoBKey, TargetKey: pkgBKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsFile, SourceKey: pkgA1Key, TargetKey: fileMainKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsFile, SourceKey: pkgA2Key, TargetKey: fileLibKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsFile, SourceKey: pkgBKey, TargetKey: fileAppKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.Defines, SourceKey: fileMainKey, TargetKey: scanFixtureSymbolMainKey, Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition},
			{Kind: facts.Defines, SourceKey: fileMainKey, TargetKey: scanFixtureSymbolSetupKey, Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition},
			{Kind: facts.Defines, SourceKey: fileLibKey, TargetKey: scanFixtureSymbolHelperKey, Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition},
			{Kind: facts.Defines, SourceKey: fileLibKey, TargetKey: scanFixtureSymbolProcessKey, Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition},
			{Kind: facts.Defines, SourceKey: fileAppKey, TargetKey: scanFixtureSymbolAppKey, Confidence: facts.StructuralCertain, Provenance: facts.TypeScriptChecker},
			{Kind: facts.Defines, SourceKey: fileAppKey, TargetKey: scanFixtureSymbolBaseKey, Confidence: facts.StructuralCertain, Provenance: facts.TypeScriptChecker},
			// Semantic: five distinct classes, including one Package -> Package edge.
			{Kind: facts.PackageDependsOn, SourceKey: pkgA1Key, TargetKey: pkgA2Key, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{
				Kind: facts.References, SourceKey: scanFixtureSymbolMainKey, TargetKey: scanFixtureSymbolHelperKey,
				Confidence: facts.ExactTypechecked, Provenance: facts.GoTypesUse, EvidenceKey: evidenceRefKey,
			},
			{
				Kind: facts.CallsDirect, SourceKey: scanFixtureSymbolMainKey, TargetKey: scanFixtureSymbolProcessKey,
				Confidence: facts.ExactTypechecked, Provenance: facts.GoASTCall, EvidenceKey: evidenceCallKey,
			},
			{
				// No EvidenceKey: exercises the NULL round trip on a
				// semantic (five property) edge table too.
				Kind: facts.TypeUses, SourceKey: scanFixtureSymbolAppKey, TargetKey: scanFixtureSymbolMainKey,
				Confidence: facts.StructuralCertain, Provenance: facts.GoObjectPath,
			},
			{
				Kind: facts.Extends, SourceKey: scanFixtureSymbolAppKey, TargetKey: scanFixtureSymbolBaseKey,
				Confidence: facts.Candidate, Provenance: facts.TreeSitterSyntax,
			},
		},
	}
	set.Sort()
	if err := set.Validate(); err != nil {
		t.Fatalf("canonicalScanFixtureSet: invalid fixture: %v", err)
	}
	return set
}

// loadCanonicalScanFixture loads canonicalScanFixtureSet into a fresh
// database under t.TempDir() and returns everything a test needs to both
// call ScanCanonical and compute the graph it must return.
func loadCanonicalScanFixture(t *testing.T) (path string, set facts.Set, options CanonicalLoadOptions) {
	t.Helper()
	ctx := context.Background()
	set = canonicalScanFixtureSet(t)
	options = CanonicalLoadOptions{SnapshotID: scanFixtureSnapshotID, ResolverVersion: scanFixtureResolverVersion}
	path = filepath.Join(t.TempDir(), "graph.db")
	if _, err := LoadCanonical(ctx, path, set, options); err != nil {
		t.Fatalf("LoadCanonical() error = %v", err)
	}
	return path, set, options
}

// wantCanonicalScanGraph derives the CanonicalGraph a correct scan of set
// must return, independently of ScanCanonical itself: it applies the same
// rendering rules canonical_load.go applies when writing rows (language
// values cast to plain strings, Repository.Languages comma joined and
// sorted, positions widened to int64, an edge's EvidenceKey/SourceSnapshot/
// ResolverVersion populated only for a table that actually carries those
// columns) and then sorts every collection the way ScanCanonical documents
// it will.
func wantCanonicalScanGraph(set facts.Set, options CanonicalLoadOptions) CanonicalGraph {
	want := CanonicalGraph{
		SchemaVersion: CanonicalSchemaVersion,
		Repositories:  make([]CanonicalRepository, len(set.Repositories)),
		Packages:      make([]CanonicalPackage, len(set.Packages)),
		Files:         make([]CanonicalFile, len(set.Files)),
		Symbols:       make([]CanonicalSymbol, len(set.Symbols)),
		Evidence:      make([]CanonicalEvidence, len(set.Evidence)),
		Edges:         make([]CanonicalEdge, len(set.Edges)),
	}

	for index, repository := range set.Repositories {
		want.Repositories[index] = CanonicalRepository{
			StableKey: repository.Key, Name: repository.Name, RootPath: repository.RootPath,
			Commit: repository.Commit, Branch: repository.Branch, Dirty: repository.Dirty,
			Languages: joinScanTestLanguages(repository.Languages),
		}
	}
	sort.Slice(want.Repositories, func(i, j int) bool { return want.Repositories[i].StableKey < want.Repositories[j].StableKey })

	for index, pkg := range set.Packages {
		want.Packages[index] = CanonicalPackage{
			StableKey: pkg.Key, RepositoryKey: pkg.RepositoryKey, Language: string(pkg.Language), Name: pkg.Name,
			Version: pkg.Version, RootPath: pkg.RootPath, ManifestPath: pkg.ManifestPath, Container: pkg.Container,
		}
	}
	sort.Slice(want.Packages, func(i, j int) bool { return want.Packages[i].StableKey < want.Packages[j].StableKey })

	for index, file := range set.Files {
		want.Files[index] = CanonicalFile{
			StableKey: file.Key, RepositoryKey: file.RepositoryKey, PackageKey: file.PackageKey, Path: file.Path,
			Language: string(file.Language), ContentHash: file.ContentHash, Generated: file.Generated,
		}
	}
	sort.Slice(want.Files, func(i, j int) bool { return want.Files[i].StableKey < want.Files[j].StableKey })

	for index, symbol := range set.Symbols {
		want.Symbols[index] = CanonicalSymbol{
			StableKey: symbol.Key, CanonicalIdentity: symbol.CanonicalIdentity, RepositoryKey: symbol.RepositoryKey,
			PackageKey: symbol.PackageKey, FileKey: symbol.FileKey, Language: string(symbol.Language), Name: symbol.Name,
			QualifiedName: symbol.QualifiedName, Kind: symbol.Kind, Exported: symbol.Exported, Signature: symbol.Signature,
			StartLine: int64(symbol.Start.Line), StartColumn: int64(symbol.Start.Column), StartOffset: int64(symbol.Start.Offset),
			EndLine: int64(symbol.End.Line), EndOffset: int64(symbol.End.Offset),
		}
	}
	sort.Slice(want.Symbols, func(i, j int) bool { return want.Symbols[i].StableKey < want.Symbols[j].StableKey })

	for index, evidence := range set.Evidence {
		want.Evidence[index] = CanonicalEvidence{
			StableKey: evidence.Key, RepositoryKey: evidence.RepositoryKey, FileKey: evidence.FileKey,
			StartLine: int64(evidence.Start.Line), StartColumn: int64(evidence.Start.Column), StartOffset: int64(evidence.Start.Offset),
			EndOffset: int64(evidence.End.Offset), Text: evidence.Text,
		}
	}
	sort.Slice(want.Evidence, func(i, j int) bool { return want.Evidence[i].StableKey < want.Evidence[j].StableKey })

	for index, edge := range set.Edges {
		canonical := CanonicalEdge{
			Table: string(edge.Kind), SourceKey: edge.SourceKey, TargetKey: edge.TargetKey,
			Confidence: string(edge.Confidence), Provenance: string(edge.Provenance),
		}
		if canonicalScanTableHasEvidenceColumns(string(edge.Kind)) {
			canonical.EvidenceKey = edge.EvidenceKey
			canonical.SourceSnapshot = options.SnapshotID
			canonical.ResolverVersion = options.ResolverVersion
		}
		want.Edges[index] = canonical
	}
	sort.Slice(want.Edges, func(i, j int) bool { return canonicalEdgeLess(want.Edges[i], want.Edges[j]) })

	return want
}

// canonicalScanTableHasEvidenceColumns reports whether the named
// relationship table carries evidence_key/source_snapshot/resolver_version,
// derived from CanonicalRelationshipTables rather than a second hand written
// list of which edge kinds are "semantic".
func canonicalScanTableHasEvidenceColumns(table string) bool {
	for _, relationship := range CanonicalRelationshipTables() {
		if relationship.Name != table {
			continue
		}
		for _, property := range relationship.Properties {
			if property.Name == "evidence_key" {
				return true
			}
		}
	}
	return false
}

func joinScanTestLanguages(languages []facts.Language) string {
	names := make([]string, len(languages))
	for index, language := range languages {
		names[index] = string(language)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

// TestScanCanonicalRoundTripsFixtureFieldByField is the proof that makes
// everything else in this package credible: what ScanCanonical returns must
// be exactly what was loaded, field by field, not merely the right counts.
func TestScanCanonicalRoundTripsFixtureFieldByField(t *testing.T) {
	path, set, options := loadCanonicalScanFixture(t)

	graph, err := ScanCanonical(context.Background(), path)
	if err != nil {
		t.Fatalf("ScanCanonical() error = %v", err)
	}

	if graph.SchemaVersion != CanonicalSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", graph.SchemaVersion, CanonicalSchemaVersion)
	}
	wantMetadata := map[string]string{
		"schema_version":   strconv.Itoa(CanonicalSchemaVersion),
		"resolver_version": options.ResolverVersion,
		"snapshot_id":      strconv.FormatInt(options.SnapshotID, 10),
	}
	for key, want := range wantMetadata {
		if got := graph.Metadata[key]; got != want {
			t.Fatalf("Metadata[%q] = %q, want %q", key, got, want)
		}
	}
	if _, present := graph.Metadata["stable_key_format"]; !present {
		t.Fatalf("Metadata is missing stable_key_format: %#v", graph.Metadata)
	}

	want := wantCanonicalScanGraph(set, options)
	if !reflect.DeepEqual(graph.Repositories, want.Repositories) {
		t.Fatalf("Repositories =\n%#v\nwant\n%#v", graph.Repositories, want.Repositories)
	}
	if !reflect.DeepEqual(graph.Packages, want.Packages) {
		t.Fatalf("Packages =\n%#v\nwant\n%#v", graph.Packages, want.Packages)
	}
	if !reflect.DeepEqual(graph.Files, want.Files) {
		t.Fatalf("Files =\n%#v\nwant\n%#v", graph.Files, want.Files)
	}
	if !reflect.DeepEqual(graph.Symbols, want.Symbols) {
		t.Fatalf("Symbols =\n%#v\nwant\n%#v", graph.Symbols, want.Symbols)
	}
	if !reflect.DeepEqual(graph.Evidence, want.Evidence) {
		t.Fatalf("Evidence =\n%#v\nwant\n%#v", graph.Evidence, want.Evidence)
	}
	if !reflect.DeepEqual(graph.Edges, want.Edges) {
		t.Fatalf("Edges =\n%#v\nwant\n%#v", graph.Edges, want.Edges)
	}

	// The fixture declares five distinct semantic classes plus three
	// structural ones; make sure none silently collapsed during decode.
	tables := make(map[string]struct{})
	for _, edge := range graph.Edges {
		tables[edge.Table] = struct{}{}
	}
	wantTables := []string{
		"CONTAINS_PACKAGE", "CONTAINS_FILE", "DEFINES",
		"PACKAGE_DEPENDS_ON", "REFERENCES", "CALLS_DIRECT", "TYPE_USES", "EXTENDS",
	}
	for _, table := range wantTables {
		if _, present := tables[table]; !present {
			t.Fatalf("Edges has no row from table %s, want one from the fixture", table)
		}
	}
}

// TestScanCanonicalExcludesObservedInAndReportsUnresolvedFromEdges proves the
// exclusion is real: the fixture's Evidence and Unresolved rows do create
// OBSERVED_IN and REPORTS_UNRESOLVED rows in the database (canonical_load.go
// derives them independently of facts.Edges), so an assertion that they are
// absent from Edges only passes for the right reason once that is confirmed.
func TestScanCanonicalExcludesObservedInAndReportsUnresolvedFromEdges(t *testing.T) {
	path, _, _ := loadCanonicalScanFixture(t)
	ctx := context.Background()

	counts, err := CanonicalTableCounts(ctx, path)
	if err != nil {
		t.Fatalf("CanonicalTableCounts() error = %v", err)
	}
	if counts["OBSERVED_IN"] == 0 || counts["REPORTS_UNRESOLVED"] == 0 {
		t.Fatalf("fixture does not populate OBSERVED_IN/REPORTS_UNRESOLVED, exclusion check would be vacuous: counts = %#v", counts)
	}

	graph, err := ScanCanonical(ctx, path)
	if err != nil {
		t.Fatalf("ScanCanonical() error = %v", err)
	}
	for _, edge := range graph.Edges {
		if edge.Table == "OBSERVED_IN" || edge.Table == "REPORTS_UNRESOLVED" {
			t.Fatalf("Edges contains a %s row, want it excluded: %#v", edge.Table, edge)
		}
	}
}

// TestScanCanonicalIsDeterministicAcrossRepeatedScans defends the rebuild's
// digest promise: two scans of the same graph must be byte identical,
// including collection order.
func TestScanCanonicalIsDeterministicAcrossRepeatedScans(t *testing.T) {
	path, _, _ := loadCanonicalScanFixture(t)
	ctx := context.Background()

	first, err := ScanCanonical(ctx, path)
	if err != nil {
		t.Fatalf("first ScanCanonical() error = %v", err)
	}
	second, err := ScanCanonical(ctx, path)
	if err != nil {
		t.Fatalf("second ScanCanonical() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("two scans of the same graph differ:\nfirst  = %#v\nsecond = %#v", first, second)
	}
}

// TestScanCanonicalOnEmptySchemaOnlyGraph covers a freshly created database
// that only ever received the schema: every collection must come back
// empty, but never with an error, and GraphMetadata's four identity keys --
// which LoadCanonical always writes, even for an empty facts.Set -- must
// still be present.
func TestScanCanonicalOnEmptySchemaOnlyGraph(t *testing.T) {
	ctx := context.Background()
	options := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "empty-graph-resolver"}
	path := filepath.Join(t.TempDir(), "graph.db")
	if _, err := LoadCanonical(ctx, path, facts.Set{}, options); err != nil {
		t.Fatalf("LoadCanonical(empty set) error = %v", err)
	}

	graph, err := ScanCanonical(ctx, path)
	if err != nil {
		t.Fatalf("ScanCanonical(empty graph) error = %v", err)
	}
	if graph.SchemaVersion != CanonicalSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", graph.SchemaVersion, CanonicalSchemaVersion)
	}
	for _, key := range []string{"schema_version", "resolver_version", "snapshot_id", "stable_key_format"} {
		if _, present := graph.Metadata[key]; !present {
			t.Fatalf("Metadata is missing key %q: %#v", key, graph.Metadata)
		}
	}
	if got := graph.Metadata["schema_version"]; got != strconv.Itoa(CanonicalSchemaVersion) {
		t.Fatalf("Metadata[schema_version] = %q, want %q", got, strconv.Itoa(CanonicalSchemaVersion))
	}

	collections := map[string]int{
		"Repositories": len(graph.Repositories), "Packages": len(graph.Packages), "Files": len(graph.Files),
		"Symbols": len(graph.Symbols), "Evidence": len(graph.Evidence), "Edges": len(graph.Edges),
	}
	for name, count := range collections {
		if count != 0 {
			t.Fatalf("%s has %d rows, want 0 on a schema only graph", name, count)
		}
	}
}

// TestScanCanonicalRejectsIncompatibleSchemaVersion covers a stored
// schema_version that disagrees with CanonicalSchemaVersion: ScanCanonical
// must refuse to build a snapshot from a layout it cannot interpret,
// reported over ErrInvalidCanonicalScan.
func TestScanCanonicalRejectsIncompatibleSchemaVersion(t *testing.T) {
	path, _, _ := loadCanonicalScanFixture(t)
	mutateCanonicalScanFixtureDatabase(t, path, `MATCH (m:GraphMetadata) WHERE m.key = 'schema_version' SET m.value = '999'`)

	_, err := ScanCanonical(context.Background(), path)
	if !errors.Is(err, ErrInvalidCanonicalScan) {
		t.Fatalf("ScanCanonical() error = %v, want errors.Is(err, ErrInvalidCanonicalScan)", err)
	}
}

// mutateCanonicalScanFixtureDatabase writes directly to the database at
// path, the same technique canonical_integrity_native_test.go's
// injectRawCypher uses, kept local to this file so it never collides with a
// same-named helper a sibling _test.go file declares.
func mutateCanonicalScanFixtureDatabase(t *testing.T, path string, statement string) {
	t.Helper()
	ctx := context.Background()
	db, err := openCanonicalDatabase(ctx, path, false)
	if err != nil {
		t.Fatalf("open for mutation: %v", err)
	}
	defer db.Close()
	connection, err := openConnection(db)
	if err != nil {
		t.Fatalf("connect for mutation: %v", err)
	}
	defer connection.Close()
	if err := queryWithDeadline(ctx, connection.native, statement); err != nil {
		t.Fatalf("mutate %q: %v", statement, err)
	}
}

// BenchmarkScanCanonicalAtScale measures ScanCanonical against a large
// synthetic canonical graph, to ground the Arrow-vs-simple-query tradeoff
// documented on ScanCanonical in a real number instead of a guess. Skipped
// by default: set LADYGRAPH_LADYBUG_SCAN_BENCH=1 to run it, the same opt-in
// shape BenchmarkReaderScanAll uses in scan_native_benchmark_test.go.
func BenchmarkScanCanonicalAtScale(b *testing.B) {
	if os.Getenv("LADYGRAPH_LADYBUG_SCAN_BENCH") == "" {
		b.Skip("set LADYGRAPH_LADYBUG_SCAN_BENCH=1 to build and scan a large synthetic canonical graph")
	}
	ctx := context.Background()
	set := largeSyntheticCanonicalScanFixture(100_000, 5)
	options := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "bench-resolver"}
	path := filepath.Join(b.TempDir(), "graph.db")
	if _, err := LoadCanonical(ctx, path, set, options); err != nil {
		b.Fatalf("LoadCanonical() error = %v", err)
	}

	b.ReportAllocs()
	b.ResetTimer()
	var graph CanonicalGraph
	var err error
	for b.Loop() {
		graph, err = ScanCanonical(ctx, path)
		if err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	rows := len(graph.Repositories) + len(graph.Packages) + len(graph.Files) + len(graph.Symbols) + len(graph.Evidence) + len(graph.Edges)
	b.ReportMetric(float64(rows), "rows")
}

// largeSyntheticCanonicalScanFixture builds a fact set with symbolCount
// symbols spread across a fixed shape of repositories/packages/files, plus
// referencesPerSymbol deterministic REFERENCES edges per symbol -- large
// enough to be representative of scan cost at the scale a real, sizeable
// repository would reach, without hand writing thousands of literals.
func largeSyntheticCanonicalScanFixture(symbolCount, referencesPerSymbol int) facts.Set {
	const (
		packageCount    = 20
		filesPerPackage = 100
		spreadStep      = 7919 // large prime: spreads deterministic edge targets across the population
	)
	repositoryKey := facts.RepositoryKey("bench-repo")
	set := facts.Set{
		Repositories: []facts.Repository{
			{Key: repositoryKey, Name: "bench-repo", RootPath: "/bench-repo", Languages: []facts.Language{facts.LanguageGo}},
		},
	}
	symbolsPerFile := symbolCount / (packageCount * filesPerPackage)
	if symbolsPerFile < 1 {
		symbolsPerFile = 1
	}

	symbolKeys := make([]string, 0, symbolCount)
	for packageIndex := range packageCount {
		packageName := fmt.Sprintf("pkg%04d", packageIndex)
		packageKey := facts.PackageKey(facts.LanguageGo, repositoryKey, packageName)
		set.Packages = append(set.Packages, facts.Package{
			Key: packageKey, RepositoryKey: repositoryKey, Language: facts.LanguageGo, Name: packageName, RootPath: "/bench-repo/" + packageName,
		})
		set.Edges = append(set.Edges, facts.Edge{
			Kind: facts.ContainsPackage, SourceKey: repositoryKey, TargetKey: packageKey,
			Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest,
		})

		for fileIndex := range filesPerPackage {
			if len(symbolKeys) >= symbolCount {
				break
			}
			filePath := fmt.Sprintf("%s/file%04d.go", packageName, fileIndex)
			fileKey := facts.FileKey(repositoryKey, filePath)
			set.Files = append(set.Files, facts.File{
				Key: fileKey, RepositoryKey: repositoryKey, PackageKey: packageKey, Path: filePath, Language: facts.LanguageGo,
			})
			set.Edges = append(set.Edges, facts.Edge{
				Kind: facts.ContainsFile, SourceKey: packageKey, TargetKey: fileKey,
				Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest,
			})

			for symbolIndex := range symbolsPerFile {
				if len(symbolKeys) >= symbolCount {
					break
				}
				name := fmt.Sprintf("Symbol%07d", len(symbolKeys))
				symbolKey := fmt.Sprintf("symbol:%s:%s:%s", packageName, filePath, name)
				set.Symbols = append(set.Symbols, facts.Symbol{
					Key: symbolKey, CanonicalIdentity: "go:" + symbolKey, RepositoryKey: repositoryKey, PackageKey: packageKey, FileKey: fileKey,
					Language: facts.LanguageGo, Name: name, QualifiedName: packageName + "." + name, Kind: "function", Exported: true,
					Signature: "func " + name + "()",
					Start:     facts.Position{Line: 1, Offset: symbolIndex * 10},
					End:       facts.Position{Line: 3, Offset: symbolIndex*10 + 9},
				})
				set.Edges = append(set.Edges, facts.Edge{
					Kind: facts.Defines, SourceKey: fileKey, TargetKey: symbolKey,
					Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition,
				})
				symbolKeys = append(symbolKeys, symbolKey)
			}
		}
	}

	for index, sourceKey := range symbolKeys {
		for edgeIndex := range referencesPerSymbol {
			target := (index + spreadStep*(edgeIndex+1)) % len(symbolKeys)
			set.Edges = append(set.Edges, facts.Edge{
				Kind: facts.References, SourceKey: sourceKey, TargetKey: symbolKeys[target],
				Confidence: facts.Candidate, Provenance: facts.TreeSitterSyntax,
			})
		}
	}

	set.Sort()
	return set
}

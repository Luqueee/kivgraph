//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
)

// Fixture symbol keys are shared between the fact set builder and the probe
// assertions below, so a typo cannot silently desync them.
const (
	fixtureSymbolMainKey    = "symbol:repoA:main.go:Main"
	fixtureSymbolHelperKey  = "symbol:repoA:helper.go:Helper"
	fixtureSymbolProcessKey = "symbol:repoA:helper.go:process"
	fixtureSymbolAppKey     = "symbol:repoB:app.ts:App"
)

// canonicalFixtureSet builds a small but complete fact set: two repositories,
// two packages, three files, four symbols, one evidence, one unresolved
// reference, every containment edge kind and three distinct semantic edge
// classes (REFERENCES, CALLS_DIRECT, TYPE_USES).
func canonicalFixtureSet(t *testing.T) facts.Set {
	t.Helper()
	repoAKey := facts.RepositoryKey("repoA")
	repoBKey := facts.RepositoryKey("repoB")
	pkgAKey := facts.PackageKey(facts.LanguageGo, "repoA", "main")
	pkgBKey := facts.PackageKey(facts.LanguageTypeScript, "repoB", "app")
	fileMainKey := facts.FileKey("repoA", "main.go")
	fileHelperKey := facts.FileKey("repoA", "helper.go")
	fileAppKey := facts.FileKey("repoB", "app.ts")
	evidenceKey := facts.EvidenceKey(fileMainKey, 10, 20)

	set := facts.Set{
		Repositories: []facts.Repository{
			{Key: repoAKey, Name: "repoA", RootPath: "/repos/repoA", Commit: "abc123", Branch: "main", Languages: []facts.Language{facts.LanguageGo}},
			{Key: repoBKey, Name: "repoB", RootPath: "/repos/repoB", Commit: "def456", Branch: "main", Languages: []facts.Language{facts.LanguageTypeScript}},
		},
		Packages: []facts.Package{
			{Key: pkgAKey, RepositoryKey: repoAKey, Language: facts.LanguageGo, Name: "main", RootPath: "/repos/repoA", ManifestPath: "/repos/repoA/go.mod"},
			{Key: pkgBKey, RepositoryKey: repoBKey, Language: facts.LanguageTypeScript, Name: "app", RootPath: "/repos/repoB", ManifestPath: "/repos/repoB/package.json"},
		},
		Files: []facts.File{
			{Key: fileMainKey, RepositoryKey: repoAKey, PackageKey: pkgAKey, Path: "main.go", Language: facts.LanguageGo, ContentHash: "hash-main"},
			{Key: fileHelperKey, RepositoryKey: repoAKey, PackageKey: pkgAKey, Path: "helper.go", Language: facts.LanguageGo, ContentHash: "hash-helper"},
			{Key: fileAppKey, RepositoryKey: repoBKey, PackageKey: pkgBKey, Path: "app.ts", Language: facts.LanguageTypeScript, ContentHash: "hash-app"},
		},
		Symbols: []facts.Symbol{
			{
				Key: fixtureSymbolMainKey, CanonicalIdentity: "go:repoA:main.Main", RepositoryKey: repoAKey, PackageKey: pkgAKey, FileKey: fileMainKey,
				Language: facts.LanguageGo, Name: "Main", QualifiedName: "main.Main", Kind: "function", Exported: true, Signature: "func Main()",
				Start: facts.Position{Line: 1, Column: 0, Offset: 0}, End: facts.Position{Line: 5, Column: 1, Offset: 80},
			},
			{
				Key: fixtureSymbolHelperKey, CanonicalIdentity: "go:repoA:helper.Helper", RepositoryKey: repoAKey, PackageKey: pkgAKey, FileKey: fileHelperKey,
				Language: facts.LanguageGo, Name: "Helper", QualifiedName: "helper.Helper", Kind: "function", Exported: true, Signature: "func Helper()",
				Start: facts.Position{Line: 1, Column: 0, Offset: 0}, End: facts.Position{Line: 3, Column: 1, Offset: 40},
			},
			{
				Key: fixtureSymbolProcessKey, CanonicalIdentity: "go:repoA:helper.process", RepositoryKey: repoAKey, PackageKey: pkgAKey, FileKey: fileHelperKey,
				Language: facts.LanguageGo, Name: "process", QualifiedName: "helper.process", Kind: "function", Exported: false, Signature: "func process()",
				Start: facts.Position{Line: 5, Column: 0, Offset: 45}, End: facts.Position{Line: 7, Column: 1, Offset: 90},
			},
			{
				Key: fixtureSymbolAppKey, CanonicalIdentity: "ts:repoB:app.App", RepositoryKey: repoBKey, PackageKey: pkgBKey, FileKey: fileAppKey,
				Language: facts.LanguageTypeScript, Name: "App", QualifiedName: "app.App", Kind: "class", Exported: true, Signature: "class App",
				Start: facts.Position{Line: 1, Column: 0, Offset: 0}, End: facts.Position{Line: 10, Column: 1, Offset: 150},
			},
		},
		Evidence: []facts.Evidence{
			{
				Key: evidenceKey, RepositoryKey: repoAKey, FileKey: fileMainKey,
				Start: facts.Position{Line: 2, Column: 0, Offset: 10}, End: facts.Position{Line: 2, Column: 10, Offset: 20}, Text: "Helper()",
			},
		},
		Unresolved: []facts.UnresolvedReference{
			{
				RepositoryKey: repoBKey, FileKey: fileAppKey, Language: facts.LanguageTypeScript, SourceSymbolKey: fixtureSymbolAppKey,
				RequestedPackage: "lodash", RequestedSymbol: "debounce", Reason: "package_not_indexed", Detail: "lodash is not part of this workspace",
				Start: facts.Position{Line: 5, Column: 2, Offset: 50},
			},
		},
		Edges: []facts.Edge{
			{Kind: facts.ContainsPackage, SourceKey: repoAKey, TargetKey: pkgAKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsPackage, SourceKey: repoBKey, TargetKey: pkgBKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsFile, SourceKey: pkgAKey, TargetKey: fileMainKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsFile, SourceKey: pkgAKey, TargetKey: fileHelperKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.ContainsFile, SourceKey: pkgBKey, TargetKey: fileAppKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			{Kind: facts.Defines, SourceKey: fileMainKey, TargetKey: fixtureSymbolMainKey, Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition},
			{Kind: facts.Defines, SourceKey: fileHelperKey, TargetKey: fixtureSymbolHelperKey, Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition},
			{Kind: facts.Defines, SourceKey: fileHelperKey, TargetKey: fixtureSymbolProcessKey, Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition},
			{Kind: facts.Defines, SourceKey: fileAppKey, TargetKey: fixtureSymbolAppKey, Confidence: facts.StructuralCertain, Provenance: facts.TypeScriptChecker},
			{
				Kind: facts.References, SourceKey: fixtureSymbolMainKey, TargetKey: fixtureSymbolHelperKey,
				Confidence: facts.ExactTypechecked, Provenance: facts.GoTypesUse, EvidenceKey: evidenceKey,
			},
			{Kind: facts.CallsDirect, SourceKey: fixtureSymbolMainKey, TargetKey: fixtureSymbolProcessKey, Confidence: facts.ExactTypechecked, Provenance: facts.GoASTCall},
			{Kind: facts.TypeUses, SourceKey: fixtureSymbolAppKey, TargetKey: fixtureSymbolMainKey, Confidence: facts.StructuralCertain, Provenance: facts.GoObjectPath},
		},
	}
	set.Sort()
	if err := set.Validate(); err != nil {
		t.Fatalf("canonicalFixtureSet: invalid fixture: %v", err)
	}
	return set
}

func TestLoadCanonicalBuildsCompleteGraphWithMatchingCountsAndProbes(t *testing.T) {
	ctx := context.Background()
	set := canonicalFixtureSet(t)
	options := CanonicalLoadOptions{SnapshotID: 7, ResolverVersion: "test-resolver-v1"}

	expectedRows, err := CanonicalTableRows(set, options)
	if err != nil {
		t.Fatalf("CanonicalTableRows() error = %v", err)
	}

	path := filepath.Join(t.TempDir(), "graph.db")
	report, err := LoadCanonical(ctx, path, set, options)
	if err != nil {
		t.Fatalf("LoadCanonical() error = %v", err)
	}
	if report.StagingMS < 0 || report.CopyMS < 0 {
		t.Fatalf("report timings = %#v", report)
	}

	// LoadReport.Tables mirrors exactly what CanonicalTableRows staged: same
	// table set, same row counts.
	if len(report.Tables) != len(expectedRows) {
		t.Fatalf("report.Tables has %d entries, want %d: %#v", len(report.Tables), len(expectedRows), report.Tables)
	}
	for name, rows := range expectedRows {
		want := int64(len(rows))
		if got := report.Tables[name]; got != want {
			t.Fatalf("report.Tables[%s] = %d, want %d", name, got, want)
		}
	}

	var wantNodes, wantEdges int64
	for _, table := range CanonicalNodeTables() {
		wantNodes += int64(len(expectedRows[table.Name]))
	}
	for _, table := range CanonicalRelationshipTables() {
		wantEdges += int64(len(expectedRows[table.Name]))
	}
	if report.Nodes != wantNodes || report.Edges != wantEdges {
		t.Fatalf("report Nodes/Edges = %d/%d, want %d/%d", report.Nodes, report.Edges, wantNodes, wantEdges)
	}

	// (a) CanonicalTableCounts must agree with CanonicalTableRows for every
	// declared table, including the ones absent from the map: those read 0,
	// not "not found".
	counts, err := CanonicalTableCounts(ctx, path)
	if err != nil {
		t.Fatalf("CanonicalTableCounts() error = %v", err)
	}
	for _, name := range CanonicalTableNames() {
		want := int64(len(expectedRows[name]))
		if counts[name] != want {
			t.Fatalf("CanonicalTableCounts()[%s] = %d, want %d", name, counts[name], want)
		}
	}

	// (b) probes: four that reach MinRows and one that deliberately does not.
	probes := []CanonicalProbe{
		{Name: "main-references-outgoing", SymbolKey: fixtureSymbolMainKey, EdgeTable: "REFERENCES", MinRows: 1},
		{Name: "main-references-helper-target", SymbolKey: fixtureSymbolMainKey, TargetKey: fixtureSymbolHelperKey, EdgeTable: "REFERENCES", MinRows: 1},
		{Name: "main-calls-process-target", SymbolKey: fixtureSymbolMainKey, TargetKey: fixtureSymbolProcessKey, EdgeTable: "CALLS_DIRECT", MinRows: 1},
		{Name: "main-symbol-exists", SymbolKey: fixtureSymbolMainKey, MinRows: 1},
		{Name: "helper-has-no-outgoing-calls-direct", SymbolKey: fixtureSymbolHelperKey, EdgeTable: "CALLS_DIRECT", MinRows: 1},
	}
	wantProbeResults := map[string]struct {
		rows   int64
		passed bool
	}{
		"main-references-outgoing":            {1, true},
		"main-references-helper-target":       {1, true},
		"main-calls-process-target":           {1, true},
		"main-symbol-exists":                  {1, true},
		"helper-has-no-outgoing-calls-direct": {0, false},
	}

	results, err := RunCanonicalProbes(ctx, path, probes)
	if err != nil {
		t.Fatalf("RunCanonicalProbes() error = %v", err)
	}
	if len(results) != len(probes) {
		t.Fatalf("RunCanonicalProbes() = %d results, want %d", len(results), len(probes))
	}
	for _, result := range results {
		want, known := wantProbeResults[result.Probe]
		if !known {
			t.Fatalf("unexpected probe result %#v", result)
		}
		if result.Rows != want.rows || result.Passed != want.passed {
			t.Fatalf("probe %s = rows=%d passed=%t, want rows=%d passed=%t", result.Probe, result.Rows, result.Passed, want.rows, want.passed)
		}
		if result.Detail == "" {
			t.Fatalf("probe %s has no detail", result.Probe)
		}
	}
}

func TestLoadCanonicalRejectsPathThatAlreadyExists(t *testing.T) {
	ctx := context.Background()
	set := canonicalFixtureSet(t)
	options := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "v1"}
	path := filepath.Join(t.TempDir(), "graph.db")

	if _, err := LoadCanonical(ctx, path, set, options); err != nil {
		t.Fatalf("first LoadCanonical() error = %v", err)
	}
	before, err := CanonicalTableCounts(ctx, path)
	if err != nil {
		t.Fatalf("CanonicalTableCounts() before retry error = %v", err)
	}

	if _, err := LoadCanonical(ctx, path, set, options); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("second LoadCanonical() error = %v, want ErrAlreadyExists", err)
	}

	// The rejected retry must not have touched the existing database.
	after, err := CanonicalTableCounts(ctx, path)
	if err != nil {
		t.Fatalf("CanonicalTableCounts() after retry error = %v", err)
	}
	for name, want := range before {
		if after[name] != want {
			t.Fatalf("counts[%s] changed after rejected retry: %d -> %d", name, want, after[name])
		}
	}
}

func TestLoadCanonicalRejectsInvalidFactsWithoutLeavingADatabase(t *testing.T) {
	ctx := context.Background()
	invalid := facts.Set{
		Repositories: []facts.Repository{{Key: facts.RepositoryKey("solo"), Name: "solo", RootPath: "/repos/solo"}},
		Edges: []facts.Edge{
			{Kind: facts.References, SourceKey: "symbol:missing:one", TargetKey: "symbol:missing:two", Confidence: facts.Candidate, Provenance: facts.GoTypesUse},
		},
	}
	options := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "v1"}
	path := filepath.Join(t.TempDir(), "graph.db")

	if _, err := LoadCanonical(ctx, path, invalid, options); !errors.Is(err, ErrInvalidCanonicalLoad) {
		t.Fatalf("LoadCanonical(invalid) error = %v, want ErrInvalidCanonicalLoad", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("LoadCanonical(invalid) left something at %s: stat err = %v", path, statErr)
	}
}

func TestLoadCanonicalAcceptsEmptySetAndBuildsACompleteSchemaOnlyGraph(t *testing.T) {
	ctx := context.Background()
	options := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "v1"}
	path := filepath.Join(t.TempDir(), "graph.db")

	report, err := LoadCanonical(ctx, path, facts.Set{}, options)
	if err != nil {
		t.Fatalf("LoadCanonical(empty set) error = %v", err)
	}
	// GraphMetadata always carries its four identity rows; nothing else does.
	if report.Nodes != 4 {
		t.Fatalf("report.Nodes = %d, want 4 (GraphMetadata only)", report.Nodes)
	}
	if report.Edges != 0 {
		t.Fatalf("report.Edges = %d, want 0", report.Edges)
	}
	if len(report.Tables) != 1 || report.Tables["GraphMetadata"] != 4 {
		t.Fatalf("report.Tables = %#v, want only GraphMetadata=4", report.Tables)
	}

	counts, err := CanonicalTableCounts(ctx, path)
	if err != nil {
		t.Fatalf("CanonicalTableCounts(empty set) error = %v", err)
	}
	for _, name := range CanonicalTableNames() {
		want := int64(0)
		if name == "GraphMetadata" {
			want = 4
		}
		if counts[name] != want {
			t.Fatalf("CanonicalTableCounts(empty set)[%s] = %d, want %d", name, counts[name], want)
		}
	}

	if results, err := RunCanonicalProbes(ctx, path, nil); err != nil || results != nil {
		t.Fatalf("RunCanonicalProbes(no probes) = %#v, %v, want nil, nil", results, err)
	}
	results, err := RunCanonicalProbes(ctx, path, []CanonicalProbe{{Name: "nothing-yet", SymbolKey: "symbol:none", MinRows: 1}})
	if err != nil {
		t.Fatalf("RunCanonicalProbes(empty graph) error = %v", err)
	}
	if len(results) != 1 || results[0].Rows != 0 || results[0].Passed {
		t.Fatalf("RunCanonicalProbes(empty graph) = %#v, want a single failing probe with 0 rows", results)
	}
}

// Canonical text — Evidence.text, Symbol.signature — legitimately contains
// commas, double quotes and newlines. Those bytes must survive the CSV bulk
// load verbatim.
//
// The plain prefix is part of the contract, not padding: the engine sniffs the
// CSV dialect from the first rows, so a quoted field that only appears after a
// long unquoted run is exactly the case its defaults get wrong.
func TestLoadCanonicalRoundTripsTextWithCommasQuotesAndNewlines(t *testing.T) {
	ctx := context.Background()
	set := canonicalFixtureSet(t)
	const plainRows = 2048
	const quoted = `export * from "./topgg.js";`
	const snippet = "BitField<string, number>\nsecond \"line\", still one field"
	const signature = "func Main(values map[string]int,\n\tlabel string) (string, error)"
	repositoryKey := set.Evidence[0].RepositoryKey
	fileMainKey := set.Evidence[0].FileKey
	set.Evidence[0].Text = "plainAnchor"
	for index := 1; index <= plainRows; index++ {
		start := 1000 + index*10
		set.Evidence = append(set.Evidence, facts.Evidence{
			Key: facts.EvidenceKey(fileMainKey, start, start+4), RepositoryKey: repositoryKey, FileKey: fileMainKey,
			Start: facts.Position{Line: index, Column: 0, Offset: start}, End: facts.Position{Line: index, Column: 4, Offset: start + 4},
			Text: "plain",
		})
	}
	for offset, text := range map[int]string{40000: quoted, 50000: snippet} {
		set.Evidence = append(set.Evidence, facts.Evidence{
			Key: facts.EvidenceKey(fileMainKey, offset, offset+27), RepositoryKey: repositoryKey, FileKey: fileMainKey,
			Start: facts.Position{Line: 9000, Column: 0, Offset: offset}, End: facts.Position{Line: 9000, Column: 27, Offset: offset + 27},
			Text: text,
		})
	}
	for index := range set.Symbols {
		if set.Symbols[index].Key == fixtureSymbolMainKey {
			set.Symbols[index].Signature = signature
		}
	}
	set.Sort()
	if err := set.Validate(); err != nil {
		t.Fatalf("fixture with multiline text is invalid: %v", err)
	}

	path := filepath.Join(t.TempDir(), "graph.db")
	if _, err := LoadCanonical(ctx, path, set, CanonicalLoadOptions{SnapshotID: 3, ResolverVersion: "v1"}); err != nil {
		t.Fatalf("LoadCanonical() error = %v", err)
	}

	graph, err := ScanCanonical(ctx, path)
	if err != nil {
		t.Fatalf("ScanCanonical() error = %v", err)
	}
	if len(graph.Evidence) != len(set.Evidence) {
		t.Fatalf("scanned evidence = %d, want %d", len(graph.Evidence), len(set.Evidence))
	}
	scannedText := make(map[string]string, len(graph.Evidence))
	for _, evidence := range graph.Evidence {
		scannedText[evidence.StableKey] = evidence.Text
	}
	for _, want := range set.Evidence {
		if got := scannedText[string(want.Key)]; got != want.Text {
			t.Fatalf("evidence %s text = %q, want %q", want.Key, got, want.Text)
		}
	}
	if len(graph.Symbols) != len(set.Symbols) {
		t.Fatalf("scanned symbols = %d, want %d", len(graph.Symbols), len(set.Symbols))
	}
	var found bool
	for _, symbol := range graph.Symbols {
		if symbol.StableKey != fixtureSymbolMainKey {
			continue
		}
		found = true
		if symbol.Signature != signature {
			t.Fatalf("symbol signature = %q, want %q", symbol.Signature, signature)
		}
	}
	if !found {
		t.Fatalf("symbol %s missing from the scanned graph", fixtureSymbolMainKey)
	}
}

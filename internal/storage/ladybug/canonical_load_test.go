package ladybug

import (
	"errors"
	"reflect"
	"strconv"
	"testing"

	"github.com/Luqueee/luque/internal/facts"
	"github.com/Luqueee/luque/internal/hotsnapshot"
)

// TestCanonicalColumnsMatchesSchemaMetadata is the parity invariant with the
// schema: CanonicalColumns must not hand LoadCanonical an order that
// disagrees with what CanonicalNodeTables/CanonicalRelationshipTables define.
func TestCanonicalColumnsMatchesSchemaMetadata(t *testing.T) {
	for _, node := range CanonicalNodeTables() {
		want := append([]string{node.PrimaryKey.Name}, propertyNames(node.Properties)...)
		got, exists := CanonicalColumns(node.Name)
		if !exists {
			t.Fatalf("CanonicalColumns(%q) not found", node.Name)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("CanonicalColumns(%q) = %v, want %v", node.Name, got, want)
		}
	}

	for _, relationship := range CanonicalRelationshipTables() {
		want := append([]string{"from", "to"}, propertyNames(relationship.Properties)...)
		got, exists := CanonicalColumns(relationship.Name)
		if !exists {
			t.Fatalf("CanonicalColumns(%q) not found", relationship.Name)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("CanonicalColumns(%q) = %v, want %v", relationship.Name, got, want)
		}
	}

	if _, exists := CanonicalColumns("NotACanonicalTable"); exists {
		t.Fatalf("CanonicalColumns(unknown table) reported exists = true")
	}
}

// TestCanonicalTableRowsIsDeterministic defends the rebuild's core promise:
// the same facts always become the same bytes, regardless of discovery
// order, and rendering rows never mutates the caller's set.
func TestCanonicalTableRowsIsDeterministic(t *testing.T) {
	fixture := newRichFixture()
	options := CanonicalLoadOptions{SnapshotID: 9, ResolverVersion: "resolver-det"}

	sorted := fixture.set
	sorted.Sort()

	scrambled := fixture.set
	scrambled.Files = []facts.File{fixture.set.Files[1], fixture.set.Files[0]}
	scrambled.Symbols = []facts.Symbol{fixture.set.Symbols[1], fixture.set.Symbols[0]}
	scrambled.Evidence = []facts.Evidence{fixture.set.Evidence[1], fixture.set.Evidence[0]}
	scrambled.Edges = []facts.Edge{fixture.set.Edges[2], fixture.set.Edges[0], fixture.set.Edges[1]}
	scrambled.Unresolved = []facts.UnresolvedReference{fixture.set.Unresolved[1], fixture.set.Unresolved[0]}
	beforeCalls := cloneForComparison(scrambled)

	rowsFromSorted, err := CanonicalTableRows(sorted, options)
	if err != nil {
		t.Fatalf("CanonicalTableRows(sorted): %v", err)
	}
	rowsFromScrambledFirst, err := CanonicalTableRows(scrambled, options)
	if err != nil {
		t.Fatalf("CanonicalTableRows(scrambled) first call: %v", err)
	}
	rowsFromScrambledSecond, err := CanonicalTableRows(scrambled, options)
	if err != nil {
		t.Fatalf("CanonicalTableRows(scrambled) second call: %v", err)
	}

	if !reflect.DeepEqual(rowsFromSorted, rowsFromScrambledFirst) {
		t.Fatalf("an unsorted set produced different rows than the same set pre-sorted:\n%v\n%v",
			rowsFromSorted, rowsFromScrambledFirst)
	}
	if !reflect.DeepEqual(rowsFromScrambledFirst, rowsFromScrambledSecond) {
		t.Fatalf("two calls over the same set produced different rows:\n%v\n%v",
			rowsFromScrambledFirst, rowsFromScrambledSecond)
	}
	if !reflect.DeepEqual(beforeCalls, cloneForComparison(scrambled)) {
		t.Fatalf("CanonicalTableRows mutated the caller's set")
	}
}

// TestCanonicalTableRowsRendersFieldsInSchemaOrder is the round trip that
// matters: every field must land under the column CanonicalColumns says it
// should, with BOOL as true/false and INT64 in base 10.
func TestCanonicalTableRowsRendersFieldsInSchemaOrder(t *testing.T) {
	fixture := newRichFixture()
	options := CanonicalLoadOptions{SnapshotID: 501, ResolverVersion: "resolver-v9"}

	tables, err := CanonicalTableRows(fixture.set, options)
	if err != nil {
		t.Fatalf("CanonicalTableRows: %v", err)
	}

	repositoryRow := findRowByColumn(t, "Repository", tables["Repository"], "stable_key", fixture.repository.Key)
	assertColumns(t, "Repository", repositoryRow, map[string]string{
		"stable_key": fixture.repository.Key,
		"name":       fixture.repository.Name,
		"root_path":  fixture.repository.RootPath,
		"commit":     "abc123",
		"branch":     "main",
		"dirty":      "true",
		"languages":  "go,typescript", // rendered sorted, though the fixture lists them reversed
	})

	packageRow := findRowByColumn(t, "Package", tables["Package"], "stable_key", fixture.pkg.Key)
	assertColumns(t, "Package", packageRow, map[string]string{
		"stable_key":     fixture.pkg.Key,
		"repository_key": fixture.repository.Key,
		"language":       "go",
		"name":           "widgets",
		"version":        "v1.2.3",
		"root_path":      "/repo/widgets",
		"manifest_path":  "/repo/widgets/go.mod",
		"container":      "github.com/acme/widgets",
	})

	fileRow := findRowByColumn(t, "File", tables["File"], "stable_key", fixture.fileB.Key)
	assertColumns(t, "File", fileRow, map[string]string{
		"stable_key":     fixture.fileB.Key,
		"repository_key": fixture.repository.Key,
		"package_key":    fixture.pkg.Key,
		"path":           "widgets/b.go",
		"language":       "go",
		"content_hash":   "",
		"generated":      "true",
	})

	symbolRow := findRowByColumn(t, "Symbol", tables["Symbol"], "stable_key", fixture.symbolA.Key)
	assertColumns(t, "Symbol", symbolRow, map[string]string{
		"stable_key":         fixture.symbolA.Key,
		"canonical_identity": "widgets.A",
		"qualified_name":     "widgets.A",
		"kind":               "function",
		"exported":           "true",
		"signature":          "func A()",
		"start_line":         "10",
		"start_column":       "2",
		"start_offset":       "100",
		"end_line":           "12",
		"end_offset":         "140",
	})

	evidenceRow := findRowByColumn(t, "Evidence", tables["Evidence"], "stable_key", fixture.evidenceA.Key)
	assertColumns(t, "Evidence", evidenceRow, map[string]string{
		"stable_key":     fixture.evidenceA.Key,
		"repository_key": fixture.repository.Key,
		"file_key":       fixture.fileA.Key,
		"start_line":     "10",
		"start_column":   "2",
		"start_offset":   "100",
		"end_offset":     "105",
		"text":           "A()",
	})

	unresolvedKey := facts.UnresolvedKey(fixture.unresolvedA)
	unresolvedRow := findRowByColumn(t, "UnresolvedReference", tables["UnresolvedReference"], "stable_key", unresolvedKey)
	assertColumns(t, "UnresolvedReference", unresolvedRow, map[string]string{
		"stable_key":        unresolvedKey,
		"repository_key":    fixture.repository.Key,
		"file_key":          fixture.fileA.Key,
		"language":          "go",
		"source_symbol_key": "",
		"requested_package": "acme/missing-one",
		"requested_symbol":  "One",
		"reason":            "package_not_found",
		"detail":            "no go.mod entry",
		"start_line":        "20",
		"start_column":      "3",
		"start_offset":      "200",
	})

	containmentRow := findRowByColumn(t, "CONTAINS_FILE", tables["CONTAINS_FILE"], "from", fixture.pkg.Key)
	if len(containmentRow) != 4 {
		t.Fatalf("CONTAINS_FILE row = %v, a containment table must carry only from, to, confidence, provenance", containmentRow)
	}
	assertColumns(t, "CONTAINS_FILE", containmentRow, map[string]string{
		"from":       fixture.pkg.Key,
		"to":         fixture.fileA.Key,
		"confidence": string(facts.ExactPackageMapped),
		"provenance": string(facts.PackageManifest),
	})

	callRow := findRowByColumn(t, "CALLS_DIRECT", tables["CALLS_DIRECT"], "from", fixture.symbolA.Key)
	assertColumns(t, "CALLS_DIRECT", callRow, map[string]string{
		"from":             fixture.symbolA.Key,
		"to":               fixture.symbolB.Key,
		"confidence":       string(facts.StructuralCertain),
		"provenance":       string(facts.GoASTCall),
		"evidence_key":     fixture.evidenceA.Key,
		"source_snapshot":  "501",
		"resolver_version": "resolver-v9",
	})
}

// TestCanonicalTableRowsRendersGraphMetadata checks the fixed identity rows:
// present, sorted by key, and free of anything machine or time specific.
func TestCanonicalTableRowsRendersGraphMetadata(t *testing.T) {
	fixture := newRichFixture()
	options := CanonicalLoadOptions{SnapshotID: 77, ResolverVersion: "resolver-zz"}

	tables, err := CanonicalTableRows(fixture.set, options)
	if err != nil {
		t.Fatalf("CanonicalTableRows: %v", err)
	}

	rows := tables["GraphMetadata"]
	want := map[string]string{
		"schema_version":    strconv.Itoa(CanonicalSchemaVersion),
		"resolver_version":  "resolver-zz",
		"snapshot_id":       "77",
		"stable_key_format": strconv.FormatUint(uint64(hotsnapshot.StableKeyFormatVersion), 10),
	}
	if len(rows) != len(want) {
		t.Fatalf("GraphMetadata has %d rows, want %d: %v", len(rows), len(want), rows)
	}
	got := make(map[string]string, len(rows))
	for _, row := range rows {
		if len(row) != 2 {
			t.Fatalf("GraphMetadata row %v does not have exactly 2 fields", row)
		}
		got[row[0]] = row[1]
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("GraphMetadata = %v, want %v", got, want)
	}
	for index := 1; index < len(rows); index++ {
		if rows[index-1][0] >= rows[index][0] {
			t.Fatalf("GraphMetadata rows are not sorted by key: %v", rows)
		}
	}
}

// TestCanonicalTableRowsRejectsUnknownEdgeKind covers an edge kind the
// canonical schema has no table for: it must fail loudly, through
// ErrInvalidCanonicalLoad, never silently drop the edge.
func TestCanonicalTableRowsRejectsUnknownEdgeKind(t *testing.T) {
	fixture := newRichFixture()
	set := fixture.set
	set.Edges = append(append([]facts.Edge(nil), set.Edges...), facts.Edge{
		Kind:       facts.EdgeKind("NOT_A_REAL_RELATION"),
		SourceKey:  fixture.symbolA.Key,
		TargetKey:  fixture.symbolB.Key,
		Confidence: facts.Candidate,
		Provenance: facts.TreeSitterSyntax,
	})

	tables, err := CanonicalTableRows(set, CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "r1"})
	if err == nil {
		t.Fatalf("expected an error for an unrecognised edge kind")
	}
	if !errors.Is(err, ErrInvalidCanonicalLoad) {
		t.Fatalf("error = %v, want errors.Is(err, ErrInvalidCanonicalLoad)", err)
	}
	if tables != nil {
		t.Fatalf("tables = %v, want nil on failure", tables)
	}
}

// TestCanonicalTableRowsDerivesStructuralRelationsWithoutEdges covers
// OBSERVED_IN and REPORTS_UNRESOLVED: neither is a facts.EdgeKind, so they
// must come from Evidence and UnresolvedReference even when Edges is empty.
func TestCanonicalTableRowsDerivesStructuralRelationsWithoutEdges(t *testing.T) {
	repository := facts.Repository{Key: facts.RepositoryKey("acme/lonely"), Name: "acme/lonely", RootPath: "/repo"}
	pkg := facts.Package{
		Key: facts.PackageKey(facts.LanguageGo, repository.Key, "lonely"), RepositoryKey: repository.Key,
		Language: facts.LanguageGo, Name: "lonely", RootPath: "/repo/lonely",
	}
	file := facts.File{
		Key: facts.FileKey(repository.Key, "lonely/main.go"), RepositoryKey: repository.Key,
		PackageKey: pkg.Key, Path: "lonely/main.go", Language: facts.LanguageGo,
	}
	evidence := facts.Evidence{
		Key: facts.EvidenceKey(file.Key, 0, 4), RepositoryKey: repository.Key, FileKey: file.Key,
		Start: facts.Position{Line: 1, Offset: 0}, End: facts.Position{Line: 1, Offset: 4},
	}
	unresolved := facts.UnresolvedReference{
		RepositoryKey: repository.Key, FileKey: file.Key, Language: facts.LanguageGo,
		RequestedPackage: "missing/pkg", RequestedSymbol: "Thing", Reason: "package_not_found",
		Start: facts.Position{Line: 2, Offset: 8},
	}

	set := facts.Set{
		Repositories: []facts.Repository{repository},
		Packages:     []facts.Package{pkg},
		Files:        []facts.File{file},
		Evidence:     []facts.Evidence{evidence},
		Unresolved:   []facts.UnresolvedReference{unresolved},
		// Edges is intentionally empty: OBSERVED_IN and REPORTS_UNRESOLVED can
		// never appear here, so this is the only way to populate them.
	}

	tables, err := CanonicalTableRows(set, CanonicalLoadOptions{SnapshotID: 3, ResolverVersion: "resolver-derived"})
	if err != nil {
		t.Fatalf("CanonicalTableRows: %v", err)
	}

	if got, want := tables["OBSERVED_IN"], [][]string{{evidence.Key, file.Key}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("OBSERVED_IN = %v, want %v", got, want)
	}
	if got, want := tables["REPORTS_UNRESOLVED"], [][]string{{repository.Key, facts.UnresolvedKey(unresolved)}}; !reflect.DeepEqual(got, want) {
		t.Fatalf("REPORTS_UNRESOLVED = %v, want %v", got, want)
	}

	// No edge ever existed, and there are no symbols, so every table sourced
	// from Edges or Symbols must be entirely absent, not present-and-empty.
	for _, name := range []string{
		"Symbol", "CONTAINS_PACKAGE", "CONTAINS_FILE", "DEFINES",
		"PACKAGE_DEPENDS_ON", "MODULE_DEPENDS_ON", "IMPORTS_SYMBOL", "EXPORTS", "REEXPORTS",
		"REFERENCES", "CALLS_DIRECT", "PASSES_AS_CALLBACK", "ASSIGNS_FUNCTION", "RETURNS_FUNCTION",
		"TYPE_USES", "IMPLEMENTS", "EXTENDS", "EMBEDS", "OVERRIDES",
	} {
		if rows, exists := tables[name]; exists {
			t.Fatalf("table %q must be omitted when it has no rows, got %v", name, rows)
		}
	}
}

// TestCanonicalTableRowsRejectsInvalidSet keeps a set that fails Validate
// from ever producing partial rows: it either loads cleanly or not at all.
func TestCanonicalTableRowsRejectsInvalidSet(t *testing.T) {
	set := facts.Set{
		Files: []facts.File{{Key: "file:x", Path: "x.go", RepositoryKey: "repository:does-not-exist"}},
	}
	tables, err := CanonicalTableRows(set, CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "r1"})
	if err == nil {
		t.Fatalf("expected an error for a set that fails Validate")
	}
	if !errors.Is(err, ErrInvalidCanonicalLoad) {
		t.Fatalf("error = %v, want errors.Is(err, ErrInvalidCanonicalLoad)", err)
	}
	if tables != nil {
		t.Fatalf("tables = %v, want nil on failure", tables)
	}
}

// TestCanonicalTableRowsRejectsEmptyResolverVersion covers the option half of
// validation: a load with no resolver identity cannot be audited later.
func TestCanonicalTableRowsRejectsEmptyResolverVersion(t *testing.T) {
	fixture := newRichFixture()
	tables, err := CanonicalTableRows(fixture.set, CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "   "})
	if err == nil {
		t.Fatalf("expected an error for a blank resolver version")
	}
	if !errors.Is(err, ErrInvalidCanonicalLoad) {
		t.Fatalf("error = %v, want errors.Is(err, ErrInvalidCanonicalLoad)", err)
	}
	if tables != nil {
		t.Fatalf("tables = %v, want nil on failure", tables)
	}
}

// TestCanonicalTableRowsOmitsEveryEmptyTable pushes "omit tables with no
// rows" to its limit: a fact set with nothing in it must still render
// GraphMetadata alone, never a map of tables paired with empty slices.
func TestCanonicalTableRowsOmitsEveryEmptyTable(t *testing.T) {
	tables, err := CanonicalTableRows(facts.Set{}, CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "resolver-empty"})
	if err != nil {
		t.Fatalf("CanonicalTableRows: %v", err)
	}
	if len(tables) != 1 {
		t.Fatalf("tables = %v, want only GraphMetadata", tables)
	}
	if _, exists := tables["GraphMetadata"]; !exists {
		t.Fatalf("tables = %v, want GraphMetadata present", tables)
	}
}

func propertyNames(properties []SchemaProperty) []string {
	names := make([]string, len(properties))
	for index, property := range properties {
		names[index] = property.Name
	}
	return names
}

func cloneForComparison(set facts.Set) facts.Set {
	return facts.Set{
		Repositories: append([]facts.Repository(nil), set.Repositories...),
		Packages:     append([]facts.Package(nil), set.Packages...),
		Files:        append([]facts.File(nil), set.Files...),
		Symbols:      append([]facts.Symbol(nil), set.Symbols...),
		Evidence:     append([]facts.Evidence(nil), set.Evidence...),
		Edges:        append([]facts.Edge(nil), set.Edges...),
		Unresolved:   append([]facts.UnresolvedReference(nil), set.Unresolved...),
	}
}

func findRowByColumn(t *testing.T, table string, rows [][]string, column, value string) []string {
	t.Helper()
	columns, exists := CanonicalColumns(table)
	if !exists {
		t.Fatalf("CanonicalColumns(%q) missing", table)
	}
	index := -1
	for candidateIndex, name := range columns {
		if name == column {
			index = candidateIndex
			break
		}
	}
	if index == -1 {
		t.Fatalf("table %q has no column %q", table, column)
	}
	for _, row := range rows {
		if row[index] == value {
			return row
		}
	}
	t.Fatalf("table %q has no row with %s = %q; rows: %v", table, column, value, rows)
	return nil
}

func assertColumns(t *testing.T, table string, row []string, expected map[string]string) {
	t.Helper()
	columns, exists := CanonicalColumns(table)
	if !exists {
		t.Fatalf("CanonicalColumns(%q) missing", table)
	}
	if len(row) != len(columns) {
		t.Fatalf("%s row has %d fields %v, want %d matching columns %v", table, len(row), row, len(columns), columns)
	}
	for index, column := range columns {
		want, specified := expected[column]
		if !specified {
			continue
		}
		if row[index] != want {
			t.Fatalf("%s.%s = %q, want %q (row=%v columns=%v)", table, column, row[index], want, row, columns)
		}
	}
}

// richFixture is a fact set with two of everything that sorts or dedups, so
// tests can scramble collection order and exercise both a containment and a
// semantic relation derived from real data.
type richFixture struct {
	set                  facts.Set
	repository           facts.Repository
	pkg                  facts.Package
	fileA, fileB         facts.File
	symbolA, symbolB     facts.Symbol
	evidenceA, evidenceB facts.Evidence
	unresolvedA          facts.UnresolvedReference
}

func newRichFixture() richFixture {
	repository := facts.Repository{
		Key:       facts.RepositoryKey("github.com/acme/widgets"),
		Name:      "github.com/acme/widgets",
		RootPath:  "/repo",
		Commit:    "abc123",
		Branch:    "main",
		Dirty:     true,
		Languages: []facts.Language{facts.LanguageTypeScript, facts.LanguageGo}, // deliberately unsorted
	}
	pkg := facts.Package{
		Key:           facts.PackageKey(facts.LanguageGo, repository.Key, "widgets"),
		RepositoryKey: repository.Key,
		Language:      facts.LanguageGo,
		Name:          "widgets",
		Version:       "v1.2.3",
		RootPath:      "/repo/widgets",
		ManifestPath:  "/repo/widgets/go.mod",
		Container:     "github.com/acme/widgets",
	}
	fileA := facts.File{
		Key:           facts.FileKey(repository.Key, "widgets/a.go"),
		RepositoryKey: repository.Key,
		PackageKey:    pkg.Key,
		Path:          "widgets/a.go",
		Language:      facts.LanguageGo,
		ContentHash:   "deadbeef",
	}
	fileB := facts.File{
		Key:           facts.FileKey(repository.Key, "widgets/b.go"),
		RepositoryKey: repository.Key,
		PackageKey:    pkg.Key,
		Path:          "widgets/b.go",
		Language:      facts.LanguageGo,
		Generated:     true,
	}
	symbolA := facts.Symbol{
		Key:               "symbol:widgets.A",
		CanonicalIdentity: "widgets.A",
		RepositoryKey:     repository.Key,
		PackageKey:        pkg.Key,
		FileKey:           fileA.Key,
		Language:          facts.LanguageGo,
		Name:              "A",
		QualifiedName:     "widgets.A",
		Kind:              "function",
		Exported:          true,
		Signature:         "func A()",
		Start:             facts.Position{Line: 10, Column: 2, Offset: 100},
		End:               facts.Position{Line: 12, Column: 1, Offset: 140},
	}
	symbolB := facts.Symbol{
		Key:               "symbol:widgets.B",
		CanonicalIdentity: "widgets.B",
		RepositoryKey:     repository.Key,
		PackageKey:        pkg.Key,
		FileKey:           fileB.Key,
		Language:          facts.LanguageGo,
		Name:              "B",
		QualifiedName:     "widgets.B",
		Kind:              "function",
		Start:             facts.Position{Line: 5, Column: 0, Offset: 40},
		End:               facts.Position{Line: 5, Column: 9, Offset: 49},
	}
	evidenceA := facts.Evidence{
		Key:           facts.EvidenceKey(fileA.Key, 100, 105),
		RepositoryKey: repository.Key,
		FileKey:       fileA.Key,
		Start:         facts.Position{Line: 10, Column: 2, Offset: 100},
		End:           facts.Position{Line: 10, Column: 7, Offset: 105},
		Text:          "A()",
	}
	evidenceB := facts.Evidence{
		Key:           facts.EvidenceKey(fileB.Key, 40, 45),
		RepositoryKey: repository.Key,
		FileKey:       fileB.Key,
		Start:         facts.Position{Line: 5, Column: 0, Offset: 40},
		End:           facts.Position{Line: 5, Column: 5, Offset: 45},
	}
	containment := facts.Edge{
		Kind:       facts.ContainsFile,
		SourceKey:  pkg.Key,
		TargetKey:  fileA.Key,
		Confidence: facts.ExactPackageMapped,
		Provenance: facts.PackageManifest,
	}
	callEdge := facts.Edge{
		Kind:        facts.CallsDirect,
		SourceKey:   symbolA.Key,
		TargetKey:   symbolB.Key,
		Confidence:  facts.StructuralCertain,
		Provenance:  facts.GoASTCall,
		EvidenceKey: evidenceA.Key,
	}
	referenceEdge := facts.Edge{
		Kind:        facts.References,
		SourceKey:   symbolB.Key,
		TargetKey:   symbolA.Key,
		Confidence:  facts.Candidate,
		Provenance:  facts.TreeSitterSyntax,
		EvidenceKey: evidenceB.Key,
	}
	unresolvedA := facts.UnresolvedReference{
		RepositoryKey:    repository.Key,
		FileKey:          fileA.Key,
		Language:         facts.LanguageGo,
		RequestedPackage: "acme/missing-one",
		RequestedSymbol:  "One",
		Reason:           "package_not_found",
		Detail:           "no go.mod entry",
		Start:            facts.Position{Line: 20, Column: 3, Offset: 200},
	}
	unresolvedB := facts.UnresolvedReference{
		RepositoryKey:    repository.Key,
		FileKey:          fileB.Key,
		Language:         facts.LanguageGo,
		RequestedPackage: "acme/missing-two",
		RequestedSymbol:  "Two",
		Reason:           "package_not_found",
		Start:            facts.Position{Line: 30, Column: 1, Offset: 300},
	}

	set := facts.Set{
		Repositories: []facts.Repository{repository},
		Packages:     []facts.Package{pkg},
		Files:        []facts.File{fileA, fileB},
		Symbols:      []facts.Symbol{symbolA, symbolB},
		Evidence:     []facts.Evidence{evidenceA, evidenceB},
		Edges:        []facts.Edge{containment, callEdge, referenceEdge},
		Unresolved:   []facts.UnresolvedReference{unresolvedA, unresolvedB},
	}

	return richFixture{
		set:         set,
		repository:  repository,
		pkg:         pkg,
		fileA:       fileA,
		fileB:       fileB,
		symbolA:     symbolA,
		symbolB:     symbolB,
		evidenceA:   evidenceA,
		evidenceB:   evidenceB,
		unresolvedA: unresolvedA,
	}
}

package tools

import (
	"context"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

func TestImplementationsPageContainsTypedRelationsOnly(t *testing.T) {
	store := dispatchSnapshot(t, 200, 2)
	args := FindImplementationsInput{StableKey: "iface-shared", Limit: 1}
	_, first, err := findImplementations(context.Background(), nil, args, store)
	if err != nil {
		t.Fatalf("find implementations with %#v: %v", args, err)
	}
	if first.Total != 2 || first.Returned != 1 || first.NextCursor == nil {
		t.Fatalf("page for %#v: %#v", args, first)
	}
	if first.Results.Implementations[0].EdgeKind != "IMPLEMENTS" || first.Results.Implementations[0].Detection != "structural" {
		t.Fatalf("untyped result for %#v: %#v", args, first.Results)
	}
	args.Cursor = *first.NextCursor
	_, second, err := findImplementations(context.Background(), nil, args, store)
	if err != nil || second.Returned != 1 || second.NextCursor != nil {
		t.Fatalf("second page with %#v: %#v %v", args, second, err)
	}
	if second.Results.Implementations[0].EdgeKind != "IMPLEMENTS" || second.Results.Implementations[0].Detection != "structural" {
		t.Fatalf("untyped second result for %#v: %#v", args, second.Results)
	}
	if first.Results.Implementations[0].StableKey == second.Results.Implementations[0].StableKey {
		t.Fatalf("duplicate page for %#v: first=%q second=%q", args, first.Results.Implementations[0].StableKey, second.Results.Implementations[0].StableKey)
	}
	args.Detection = "declared"
	if _, _, err := findImplementations(context.Background(), nil, args, store); err == nil {
		t.Fatalf("cursor accepted changed filters: %#v", args)
	}
	args.Detection = ""
	if _, _, err := findImplementations(context.Background(), nil, args, dispatchSnapshot(t, 201, 2)); err == nil {
		t.Fatalf("cursor crossed generations: %#v", args)
	}
	filterStore := implementationFilterSnapshot(t)
	for _, detection := range []string{"declared", "structural"} {
		filteredArgs := FindImplementationsInput{StableKey: "contract", Detection: detection}
		_, filtered, err := findImplementations(context.Background(), nil, filteredArgs, filterStore)
		if err != nil || filtered.Total != 1 || filtered.Returned != 1 {
			t.Fatalf("%s filter for %#v: %#v %v", detection, filteredArgs, filtered, err)
		}
		if got := filtered.Results.Implementations[0].Detection; got != detection {
			t.Fatalf("%s filter for %#v returned detection %q", detection, filteredArgs, got)
		}
	}
	concreteArgs := FindImplementationsInput{StableKey: "impl-sole"}
	_, concrete, err := findImplementations(context.Background(), nil, concreteArgs, store)
	if err != nil || concrete.Total != 0 {
		t.Fatalf("dispatch calls leaked for %#v: %#v %v", concreteArgs, concrete, err)
	}
	if concrete.Completeness == nil || concrete.Completeness.Verdict != VerdictLowerBound {
		t.Fatalf("legacy generation falsely attested complete coverage for %#v", concreteArgs)
	}
	filteredArgs := FindImplementationsInput{StableKey: "iface-shared", Paths: []string{"disk.go"}}
	_, filtered, err := findImplementations(context.Background(), nil, filteredArgs, store)
	if err != nil || filtered.Total != 1 || filtered.Results.Implementations[0].FilePath != "disk.go" {
		t.Fatalf("paths filter for %#v: %#v %v", filteredArgs, filtered, err)
	}
	for _, args := range []FindImplementationsInput{{StableKey: "iface-sole", Detection: "guess"}, {StableKey: "iface-sole", Paths: []string{"../outside"}}, {StableKey: "iface-sole", Paths: []string{"."}}, {StableKey: "iface-sole", Paths: []string{"a\x00b"}}} {
		if _, _, err := findImplementations(context.Background(), nil, args, store); err == nil {
			t.Fatalf("invalid arguments accepted: %#v", args)
		}
	}
}

func implementationFilterSnapshot(t *testing.T) *hotsnapshot.SnapshotStore {
	t.Helper()
	code := func(value uint8) uint8 { return value }
	edge := func(source string, provenance facts.Provenance) hotsnapshot.EdgeRow {
		return hotsnapshot.EdgeRow{
			SourceKey: hotsnapshot.StableKey(source), TargetKey: "contract", Kind: code(mustFactsEdgeCode(t, facts.Implements)),
			Confidence: code(mustFactsConfidenceCode(t, facts.ExactTypechecked)), Provenance: code(mustFactsProvenanceCode(t, provenance)),
			EvidenceKind: "checker", EvidenceSourceFileKey: "file-" + source, EvidenceTargetFileKey: "file-contract",
		}
	}
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repository:repo-a", Name: "repo-a", Path: "/repo-a", Languages: "typescript"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "package-a", RepositoryKey: "repository:repo-a", Name: "pkg", ModulePath: "pkg"}},
		Files: []hotsnapshot.FileRow{
			{Key: "file-contract", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "contract.ts", Language: "typescript"},
			{Key: "file-declared", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "memory.ts", Language: "typescript"},
			{Key: "file-structural", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "disk.go", Language: "typescript"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "contract", CanonicalIdentity: "ts:Store", FileKey: "file-contract", Language: "typescript", Name: "Store", QualifiedName: "Store", Kind: "interface", StartLine: 1, EndLine: 3},
			{StableKey: "declared", CanonicalIdentity: "ts:Memory", FileKey: "file-declared", Language: "typescript", Name: "Memory", QualifiedName: "Memory", Kind: "class", StartLine: 1, EndLine: 3},
			{StableKey: "structural", CanonicalIdentity: "ts:Disk", FileKey: "file-structural", Language: "typescript", Name: "Disk", QualifiedName: "Disk", Kind: "class", StartLine: 1, EndLine: 3},
		},
		Edges: []hotsnapshot.EdgeRow{edge("declared", facts.TypeScriptImplementationDeclared), edge("structural", facts.TypeScriptImplementationStructural)},
	}
	// A class that extends one of the implementations, and names nothing else.
	rows.Files = append(rows.Files, hotsnapshot.FileRow{Key: "file-sub", RepositoryKey: "repository:repo-a", PackageKey: "package-a", Path: "cached.ts", Language: "typescript"})
	rows.Symbols = append(rows.Symbols, hotsnapshot.SymbolRow{StableKey: "sub", CanonicalIdentity: "ts:Cached", FileKey: "file-sub", Language: "typescript", Name: "Cached", QualifiedName: "Cached", Kind: "class", StartLine: 1, EndLine: 3})
	extends := edge("sub", facts.TypeScriptChecker)
	extends.TargetKey, extends.Kind, extends.EvidenceTargetFileKey = "declared", mustFactsEdgeCode(t, facts.Extends), "file-declared"
	rows.Edges = append(rows.Edges, extends)
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, 200, time.Unix(1_700_000_200, 0).UTC(), 5)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

// Asked who implements a class, the answer left out every class that extends
// it: only IMPLEMENTS and OVERRIDES were read.
func TestImplementationsOfAClassAreItsSubclasses(t *testing.T) {
	store := implementationFilterSnapshot(t)
	for detection, want := range map[string]int{"": 1, "declared": 1, "structural": 0} {
		_, page, err := findImplementations(context.Background(), nil, FindImplementationsInput{StableKey: "declared", Detection: detection}, store)
		if err != nil || page.Total != want {
			t.Fatalf("detection %q: total = %d, %v, want %d", detection, page.Total, err, want)
		}
		if want == 1 {
			row := page.Results.Implementations[0]
			if row.StableKey != "sub" || row.EdgeKind != "EXTENDS" || row.Detection != "declared" {
				t.Errorf("detection %q: row = %#v, want the subclass, declared", detection, row)
			}
		}
	}
	// A class nothing extends has none, and the interface keeps its two.
	if _, page, err := findImplementations(context.Background(), nil, FindImplementationsInput{StableKey: "sub"}, store); err != nil || page.Total != 0 {
		t.Errorf("a leaf class: %d, %v", page.Total, err)
	}
	if _, page, err := findImplementations(context.Background(), nil, FindImplementationsInput{StableKey: "contract"}, store); err != nil || page.Total != 2 {
		t.Errorf("the interface: %d, %v", page.Total, err)
	}
}

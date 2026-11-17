package tools

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/gitdiff"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// impactSnapshot is one repository with a type that holds two methods, and the
// things that depend on them:
//
//	app.go       Type 1-30 { Run 10-20, Stop 22-28 }   Helper 40-45   Main 50-56
//	app_test.go  TestRun 5-12, TestMain 20-26
//	Main calls Run; TestRun calls Run; TestMain calls Main; Holder (a field) uses Run.
//
// README.md and docs are not indexed.
func impactSnapshot(t *testing.T, root string) *hotsnapshot.GraphSnapshot {
	t.Helper()
	return impactSnapshotWith(t, root, nil, nil)
}

// impactSnapshotWith adds symbols and edges to the one above.
func impactSnapshotWith(t *testing.T, root string, extraSymbols []hotsnapshot.SymbolRow, extraEdges []hotsnapshot.EdgeRow) *hotsnapshot.GraphSnapshot {
	t.Helper()
	call := func(source, target, file, targetFile string) hotsnapshot.EdgeRow {
		return hotsnapshot.EdgeRow{
			SourceKey: hotsnapshot.StableKey(source), TargetKey: hotsnapshot.StableKey(target),
			Kind: facts.CodeCallsDirect, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse,
			EvidenceKind: "types", EvidenceSourceFileKey: file, EvidenceTargetFileKey: targetFile,
		}
	}
	symbol := func(key, qualified, kind, file string, start, end uint32) hotsnapshot.SymbolRow {
		return hotsnapshot.SymbolRow{
			StableKey: hotsnapshot.StableKey(key), CanonicalIdentity: "go:" + qualified, FileKey: file,
			Language: "go", Name: qualified, QualifiedName: qualified, Kind: kind, StartLine: start, EndLine: end,
		}
	}
	symbols := []hotsnapshot.SymbolRow{
		symbol("type", "app.Type", "type", "file-app", 1, 30),
		symbol("run", "app.Type.Run", "method", "file-app", 10, 20),
		symbol("stop", "app.Type.Stop", "method", "file-app", 22, 28),
		symbol("helper", "app.Helper", "func", "file-app", 40, 45),
		symbol("main", "app.Main", "func", "file-app", 50, 56),
		symbol("holder", "app.Holder", "field", "file-app", 31, 31),
		symbol("testrun", "app.TestRun", "func", "file-test", 5, 12),
		symbol("testmain", "app.TestMain", "func", "file-test", 20, 26),
	}
	symbols = append(symbols, extraSymbols...)
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "app", Name: "app", Path: root, Languages: "go"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "pkg", RepositoryKey: "app", Language: "go", Name: "app", ModulePath: "example.com/app"}},
		Files: []hotsnapshot.FileRow{
			{Key: "file-app", RepositoryKey: "app", PackageKey: "pkg", Path: "app.go", Language: "go"},
			{Key: "file-test", RepositoryKey: "app", PackageKey: "pkg", Path: "app_test.go", Language: "go"},
		},
		Symbols: symbols,
		Edges: append([]hotsnapshot.EdgeRow{
			call("main", "run", "file-app", "file-app"),
			call("testrun", "run", "file-test", "file-app"),
			call("testmain", "main", "file-test", "file-app"),
			call("holder", "run", "file-app", "file-app"),
		}, extraEdges...),
	}, 7, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return snapshot
}

func lines(path string, ranges ...gitdiff.Range) gitdiff.Change {
	return gitdiff.Change{Path: path, Ranges: ranges}
}

func impact(t *testing.T, snapshot *hotsnapshot.GraphSnapshot, root string, changes ...gitdiff.Change) ImpactReport {
	t.Helper()
	report, err := Impact(context.Background(), snapshot, ImpactOptions{RepositoryRoot: root, Changes: changes})
	if err != nil {
		t.Fatalf("Impact() error = %v", err)
	}
	return report
}

func names[T any](rows []T, name func(T) string) []string {
	out := make([]string, 0, len(rows))
	for _, row := range rows {
		out = append(out, name(row))
	}
	return out
}

func touchedNames(report ImpactReport) []string {
	return names(report.Touched, func(row ImpactSymbol) string { return row.QualifiedName })
}

func TestImpactNamesTheInnermostSymbolsAChangeTouches(t *testing.T) {
	root := t.TempDir()
	snapshot := impactSnapshot(t, root)
	for _, testCase := range []struct {
		name   string
		change gitdiff.Range
		want   []string
	}{
		{"a hunk inside a method touches the method, not the type around it", gitdiff.Range{Start: 12, End: 13}, []string{"app.Type.Run"}},
		{"a hunk across two methods touches both", gitdiff.Range{Start: 18, End: 23}, []string{"app.Type.Run", "app.Type.Stop"}},
		{"a hunk in the type outside its methods touches the type", gitdiff.Range{Start: 3, End: 3}, []string{"app.Type"}},
		{"the first line of a symbol", gitdiff.Range{Start: 40, End: 40}, []string{"app.Helper"}},
		{"the last line of a symbol", gitdiff.Range{Start: 45, End: 45}, []string{"app.Helper"}},
		{"a removal between two methods belongs to the type that holds both", gitdiff.Range{Start: 21, End: 22, Between: true}, []string{"app.Type"}},
		{"a removal between two top-level symbols belongs to neither", gitdiff.Range{Start: 45, End: 46, Between: true}, nil},
		{"a removal inside a method", gitdiff.Range{Start: 14, End: 15, Between: true}, []string{"app.Type.Run"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := touchedNames(impact(t, snapshot, root, lines("app.go", testCase.change)))
			if len(got) != len(testCase.want) || (len(got) > 0 && !reflect.DeepEqual(got, testCase.want)) {
				t.Fatalf("touched = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestImpactSplitsWhatDependsOnAChangeIntoProductionAndTests(t *testing.T) {
	root := t.TempDir()
	report := impact(t, impactSnapshot(t, root), root, lines("app.go", gitdiff.Range{Start: 12, End: 13}))

	// Main and TestRun call Run (depth 1); TestMain calls Main (depth 2). Holder is
	// a field, a kind `get_blast_radius` withholds, so it is not counted either.
	if report.Affected != 3 || report.TraversalTruncated {
		t.Fatalf("affected = %d truncated = %v, want the three callers", report.Affected, report.TraversalTruncated)
	}
	production := names(report.Production, func(row ImpactAffected) string { return row.QualifiedName })
	if !reflect.DeepEqual(production, []string{"app.Main"}) || report.ProductionTotal != 1 {
		t.Fatalf("production = %v (%d), want only app.Main", production, report.ProductionTotal)
	}
	if len(report.ByTestFile) != 1 || report.ByTestFile[0].FilePath != "app_test.go" {
		t.Fatalf("by_test_file = %#v, want the one test file", report.ByTestFile)
	}
	tests := names(report.ByTestFile[0].Symbols, func(row ImpactAffected) string { return row.QualifiedName })
	if !reflect.DeepEqual(tests, []string{"app.TestRun", "app.TestMain"}) {
		t.Fatalf("test symbols = %v, want TestRun then TestMain by depth", tests)
	}
	if depths := []int{report.ByTestFile[0].Symbols[0].Depth, report.ByTestFile[0].Symbols[1].Depth}; !reflect.DeepEqual(depths, []int{1, 2}) {
		t.Fatalf("test depths = %v, want 1 and 2", depths)
	}
	if !reflect.DeepEqual(report.ByRepository, []ImpactRepositoryCount{{Repository: "app", Affected: 3}}) {
		t.Fatalf("by_repository = %#v", report.ByRepository)
	}
	// The touched symbol is not affected by its own change.
	for _, row := range append(append([]ImpactAffected{}, report.Production...), report.ByTestFile[0].Symbols...) {
		if row.QualifiedName == "app.Type.Run" {
			t.Fatalf("the touched symbol is reported as affected: %#v", row)
		}
	}
}

// A change to two symbols is one traversal from both, and a dependent of both is
// counted once.
func TestImpactCountsASymbolThatDependsOnSeveralTouchedOnesOnce(t *testing.T) {
	root := t.TempDir()
	report := impact(t, impactSnapshot(t, root), root,
		lines("app.go", gitdiff.Range{Start: 12, End: 13}, gitdiff.Range{Start: 50, End: 51}))
	if got := touchedNames(report); !reflect.DeepEqual(got, []string{"app.Type.Run", "app.Main"}) {
		t.Fatalf("touched = %v", got)
	}
	// Main is touched, so it is not also affected; TestMain depends on it and
	// TestRun on Run.
	if report.Affected != 2 {
		t.Fatalf("affected = %d, want TestRun and TestMain once each", report.Affected)
	}
}

func TestImpactSaysWhatItCouldNotPlace(t *testing.T) {
	root := t.TempDir()
	report := impact(
		t, impactSnapshot(t, root), root,
		lines("README.md", gitdiff.Range{Start: 1, End: 2}),
		gitdiff.Change{Path: "gone.go", Deleted: true},
		// Never added to git and not in the graph: a build directory, a tool's own
		// state. Nobody changed it on purpose, so it is not worth a line.
		gitdiff.Change{Path: ".cache/state.json", Untracked: true, Ranges: []gitdiff.Range{{Start: 1, End: gitdiff.WholeFile}}},
		lines("app.go", gitdiff.Range{Start: 32, End: 35}), // between the holder and Helper: no symbol
		lines("app.go", gitdiff.Range{Start: 1, End: gitdiff.WholeFile}),
	)
	wantFiles := []ImpactFile{
		{Path: "README.md", Reason: "not in the index"},
		{Path: "gone.go", Reason: "deleted: the symbols it declared are no longer in the graph"},
	}
	if !reflect.DeepEqual(report.NotIndexed, wantFiles) {
		t.Fatalf("not_indexed = %#v, want %#v", report.NotIndexed, wantFiles)
	}
	if !reflect.DeepEqual(report.NoSymbol, []ImpactGap{{Path: "app.go", StartLine: 32, EndLine: 35}}) {
		t.Fatalf("no_symbol = %#v, want the comment-only run", report.NoSymbol)
	}
	if len(report.Touched) == 0 {
		t.Fatal("a whole-file change touched no symbol")
	}
}

func TestImpactWithNothingTouchedAnswersEmptyListsAndTraversesNothing(t *testing.T) {
	root := t.TempDir()
	for name, changes := range map[string][]gitdiff.Change{
		"no change at all":           nil,
		"only a file nobody indexes": {lines("README.md", gitdiff.Range{Start: 1, End: 1})},
		"only lines outside symbols": {lines("app.go", gitdiff.Range{Start: 33, End: 34})},
	} {
		report := impact(t, impactSnapshot(t, root), root, changes...)
		if report.Touched == nil || report.Production == nil || report.ByTestFile == nil || report.ByRepository == nil ||
			report.NotIndexed == nil || report.NoSymbol == nil {
			t.Errorf("%s: a list is nil and encodes as null: %#v", name, report)
		}
		if len(report.Touched) != 0 || report.Affected != 0 || report.TraversalTruncated {
			t.Errorf("%s: report = %#v, want nothing touched and nothing affected", name, report)
		}
	}
}

func TestImpactCapsProductionRowsButNotTheCount(t *testing.T) {
	root := t.TempDir()
	var symbols []hotsnapshot.SymbolRow
	var edges []hotsnapshot.EdgeRow
	for index := range 6 {
		key := "caller" + strconv.Itoa(index)
		symbols = append(symbols, hotsnapshot.SymbolRow{StableKey: hotsnapshot.StableKey(key), CanonicalIdentity: "go:" + key, FileKey: "file-app",
			Language: "go", Name: key, QualifiedName: "app." + key, Kind: "func", StartLine: uint32(100 + 10*index), EndLine: uint32(105 + 10*index)})
		edges = append(edges, hotsnapshot.EdgeRow{SourceKey: hotsnapshot.StableKey(key), TargetKey: "run", Kind: facts.CodeCallsDirect,
			Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types",
			EvidenceSourceFileKey: "file-app", EvidenceTargetFileKey: "file-app"})
	}
	snapshot := impactSnapshotWith(t, root, symbols, edges)
	report, err := Impact(context.Background(), snapshot, ImpactOptions{
		RepositoryRoot: root, Limit: 2,
		Changes: []gitdiff.Change{lines("app.go", gitdiff.Range{Start: 12, End: 13})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.ProductionTotal != 7 || len(report.Production) != 2 {
		t.Fatalf("production total = %d rows = %d, want every one counted and two shown", report.ProductionTotal, len(report.Production))
	}
}

func TestImpactBoundsTheTraversalAndSaysSo(t *testing.T) {
	root := t.TempDir()
	report, err := Impact(context.Background(), impactSnapshot(t, root), ImpactOptions{
		RepositoryRoot: root, Depth: 1,
		Changes: []gitdiff.Change{lines("app.go", gitdiff.Range{Start: 12, End: 13})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Affected != 2 || report.Depth != 1 {
		t.Fatalf("affected = %d at depth %d, want the two direct callers only", report.Affected, report.Depth)
	}
}

func TestImpactRefusesWhatItCannotAnswer(t *testing.T) {
	root := t.TempDir()
	if _, err := Impact(context.Background(), nil, ImpactOptions{RepositoryRoot: root}); err == nil {
		t.Error("a missing snapshot was answered")
	}
	other := filepath.Join(t.TempDir(), "elsewhere")
	if _, err := Impact(context.Background(), impactSnapshot(t, root), ImpactOptions{RepositoryRoot: other}); !errors.Is(err, ErrRepositoryNotIndexed) {
		t.Errorf("a checkout no repository lives at: error = %v, want ErrRepositoryNotIndexed", err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Impact(cancelled, impactSnapshot(t, root), ImpactOptions{
		RepositoryRoot: root,
		Changes:        []gitdiff.Change{lines("app.go", gitdiff.Range{Start: 1, End: 1})},
	}); !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled context: error = %v, want context.Canceled", err)
	}
}

func TestImpactFindsTheRepositoryThroughASymbolicLink(t *testing.T) {
	root := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("symbolic links are unavailable: %v", err)
	}
	if _, err := Impact(context.Background(), impactSnapshot(t, root), ImpactOptions{RepositoryRoot: link}); err != nil {
		t.Fatalf("a checkout reached through a link is the same repository: %v", err)
	}
}

func TestIsTestPathReadsTheConventionsOfEveryIndexedLanguage(t *testing.T) {
	for path, want := range map[string]bool{
		"internal/a_test.go": true, "a.go": false,
		"src/widget.test.ts": true, "src/widget.spec.tsx": true, "src/widget.ts": false,
		"pkg/test_model.py": true, "pkg/model_test.py": true, "pkg/conftest.py": true, "pkg/model.py": false,
		"src/test/java/app/AppTest.java": true, "src/main/java/app/App.java": false, "app/ServiceTests.cs": true,
		"lib/models_test.dart": true, "test/models.dart": true,
		"tests/integration.rs": true, "src/__tests__/x.ts": true, "src/contest.ts": false,
		"testdata/fixture.go": false, "src/latest/value.go": false,
		`src\widget.test.ts`: true,
	} {
		if got := IsTestPath(path); got != want {
			t.Errorf("IsTestPath(%q) = %v, want %v", path, got, want)
		}
	}
}

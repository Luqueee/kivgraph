package tools

import (
	"context"
	"slices"
	"sort"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// neighboursSnapshot has one subject and three neighbours -- a source file, a
// test by file name and a test by directory -- joined to it by one edge each.
// Incoming edges run neighbour to subject, which is what a blast radius and an
// implementation list read; outgoing run the other way, which is what a
// dependency trace reads.
func neighboursSnapshot(t *testing.T, id uint64, kind facts.EdgeKind, provenance facts.Provenance, incoming bool) *hotsnapshot.SnapshotStore {
	t.Helper()
	paths := map[string]string{"subject": "src/subject.go", "source": "src/a.go", "named": "src/a_test.go", "directory": "tests/b.go"}
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repository:repo", Name: "repo", Path: "/repo", Languages: "go"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "package", RepositoryKey: "repository:repo", Name: "pkg", ModulePath: "example.com/pkg"}},
	}
	for _, name := range []string{"subject", "source", "named", "directory"} {
		rows.Files = append(rows.Files, hotsnapshot.FileRow{Key: "file-" + name, RepositoryKey: "repository:repo", PackageKey: "package", Path: paths[name], Language: "go"})
		rows.Symbols = append(rows.Symbols, hotsnapshot.SymbolRow{
			StableKey: hotsnapshot.StableKey(name), CanonicalIdentity: "go:" + name, FileKey: "file-" + name,
			Language: "go", Name: name, QualifiedName: "pkg." + name, Kind: "function", StartLine: 1, EndLine: 5,
		})
	}
	for _, name := range []string{"source", "named", "directory"} {
		from, to := name, "subject"
		if !incoming {
			from, to = to, from
		}
		rows.Edges = append(rows.Edges, hotsnapshot.EdgeRow{
			SourceKey: hotsnapshot.StableKey(from), TargetKey: hotsnapshot.StableKey(to),
			Kind:       mustFactsEdgeCode(t, kind),
			Confidence: mustFactsConfidenceCode(t, facts.ExactTypechecked),
			Provenance: mustFactsProvenanceCode(t, provenance), EvidenceKind: "checker",
			EvidenceSourceFileKey: "file-" + from, EvidenceTargetFileKey: "file-" + to,
		})
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, id, time.Unix(1_700_000_000+int64(id), 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

func reachedPaths(rows []ReachedSymbol) []string {
	var paths []string
	for _, row := range rows {
		paths = append(paths, row.FilePath)
	}
	sort.Strings(paths)
	return paths
}

// get_blast_radius counts its four axes over the symbols that pass the row
// filters, so a test that is left out has to leave the totals too, not only the
// page: a page of one row under a count of three would be an answer that
// disagrees with itself.
func TestBlastRadiusExcludesTestFilesFromRowsAndAxes(t *testing.T) {
	store := neighboursSnapshot(t, 301, facts.CallsDirect, facts.GoTypesUse, true)
	for _, testCase := range []struct {
		name         string
		excludeTests bool
		want         []string
	}{
		{name: "tests are affected by default", want: []string{"src/a.go", "src/a_test.go", "tests/b.go"}},
		{name: "excluded by file name and by directory", excludeTests: true, want: []string{"src/a.go"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, response, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
				StableKey: "subject", ExcludeTests: testCase.excludeTests,
			}, store)
			if err != nil {
				t.Fatalf("getBlastRadius() error = %v", err)
			}
			radius := response.Results
			if got := reachedPaths(radius.Symbols); !slices.Equal(got, testCase.want) {
				t.Fatalf("paths = %v, want %v", got, testCase.want)
			}
			if radius.Affected != len(testCase.want) {
				t.Fatalf("affected = %d, want %d: the axes still count what the page dropped", radius.Affected, len(testCase.want))
			}
			total := 0
			for _, group := range radius.ByRepository {
				total += group.Count
			}
			if total != len(testCase.want) {
				t.Fatalf("by_repository counts %d symbols, want %d", total, len(testCase.want))
			}
		})
	}

	first := mustBlastRadius(t, store, GetBlastRadiusInput{StableKey: "subject", Limit: 1})
	if first.NextCursor == nil {
		t.Fatal("first page has no cursor to resume")
	}
	if _, _, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "subject", Limit: 1, ExcludeTests: true, Cursor: *first.NextCursor,
	}, store); err == nil {
		t.Fatal("a cursor minted without exclude_tests was accepted with it")
	}
}

func mustBlastRadius(t *testing.T, store *hotsnapshot.SnapshotStore, input GetBlastRadiusInput) Response[BlastRadius] {
	t.Helper()
	_, response, err := getBlastRadius(context.Background(), nil, input, store)
	if err != nil {
		t.Fatalf("getBlastRadius(%#v) error = %v", input, err)
	}
	return response
}

func TestTraceDependenciesExcludesTestFiles(t *testing.T) {
	store := neighboursSnapshot(t, 302, facts.CallsDirect, facts.GoTypesUse, false)
	for _, testCase := range []struct {
		name         string
		excludeTests bool
		want         []string
	}{
		{name: "tests are reached by default", want: []string{"src/a.go", "src/a_test.go", "tests/b.go"}},
		{name: "excluded by file name and by directory", excludeTests: true, want: []string{"src/a.go"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, response, err := traceDependencies(context.Background(), nil, TraceDependenciesInput{
				StableKey: "subject", ExcludeTests: testCase.excludeTests, View: ViewFull,
			}, store)
			if err != nil {
				t.Fatalf("traceDependencies() error = %v", err)
			}
			if got := reachedPaths(response.Results.Nodes); !slices.Equal(got, testCase.want) {
				t.Fatalf("paths = %v, want %v", got, testCase.want)
			}
		})
	}
}

func TestFindImplementationsExcludesTestFiles(t *testing.T) {
	store := neighboursSnapshot(t, 303, facts.Implements, facts.GoTypesUse, true)
	for _, testCase := range []struct {
		name         string
		excludeTests bool
		want         []string
	}{
		{name: "tests implement by default", want: []string{"src/a.go", "src/a_test.go", "tests/b.go"}},
		{name: "excluded by file name and by directory", excludeTests: true, want: []string{"src/a.go"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, response, err := findImplementations(context.Background(), nil, FindImplementationsInput{
				StableKey: "subject", ExcludeTests: testCase.excludeTests,
			}, store)
			if err != nil {
				t.Fatalf("findImplementations() error = %v", err)
			}
			var paths []string
			for _, row := range response.Results.Implementations {
				paths = append(paths, row.FilePath)
			}
			sort.Strings(paths)
			if !slices.Equal(paths, testCase.want) || response.Total != len(testCase.want) {
				t.Fatalf("paths = %v total = %d, want %v", paths, response.Total, testCase.want)
			}
		})
	}
}

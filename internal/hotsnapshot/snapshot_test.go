package hotsnapshot

import (
	"errors"
	"testing"
	"time"
	"unsafe"
)

func TestGraphSnapshotCopiesDataAndIndexes(t *testing.T) {
	input := graphSnapshotTestInput()
	snapshot, err := NewGraphSnapshot(input)
	if err != nil {
		t.Fatalf("NewGraphSnapshot() error = %v", err)
	}

	metadata := snapshot.Metadata()
	if metadata.ID != 42 || metadata.Version != 1 || !metadata.CreatedAt.Equal(input.CreatedAt) {
		t.Fatalf("Metadata() = %#v", metadata)
	}
	if metadata.Counts != (IDCounts{Repositories: 1, Packages: 1, Files: 1, Symbols: 2, Evidence: 1, Edges: 1}) {
		t.Fatalf("Metadata().Counts = %#v", metadata.Counts)
	}
	if symbol, found := snapshot.Symbol(1); !found || symbol.StableKey != "symbol-b" {
		t.Fatalf("Symbol(1) = %#v, %t", symbol, found)
	}
	if _, found := snapshot.Symbol(2); found {
		t.Fatal("Symbol(2) found")
	}
	if id, found := snapshot.SymbolByStableKey("symbol-a"); !found || id != 0 {
		t.Fatalf("SymbolByStableKey() = %d, %t", id, found)
	}
	if id, found := snapshot.FileByRepoPath(RepoPathKey{Repository: 0, Path: 3}); !found || id != 0 {
		t.Fatalf("FileByRepoPath() = %d, %t", id, found)
	}
	if got := snapshot.SymbolsByName(4); len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Fatalf("SymbolsByName() = %v", got)
	}

	input.Symbols[0].StableKey = "mutated"
	input.ForwardEdges[0].Target = 1
	input.SymbolByStableKey["symbol-a"] = 1
	input.SymbolsByName[4][0] = 1
	input.FileByRepoPath[RepoPathKey{Repository: 0, Path: 3}] = 1
	if id, found := snapshot.SymbolByStableKey("symbol-a"); !found || id != 0 {
		t.Fatalf("snapshot changed after input mutation: %d, %t", id, found)
	}
	matches := snapshot.SymbolsByName(4)
	matches[0] = 1
	if got := snapshot.SymbolsByName(4); got[0] != 0 {
		t.Fatalf("snapshot index escaped through getter: %v", got)
	}
}

func TestGraphSnapshotRejectsInvalidEnvelopeAndIndexes(t *testing.T) {
	input := graphSnapshotTestInput()
	input.Version = 0
	if _, err := NewGraphSnapshot(input); !errors.Is(err, ErrInvalidGraphSnapshot) {
		t.Fatalf("invalid version error = %v", err)
	}

	input = graphSnapshotTestInput()
	input.ReverseEdges = nil
	if _, err := NewGraphSnapshot(input); !errors.Is(err, ErrInvalidGraphSnapshot) {
		t.Fatalf("mismatched edge count error = %v", err)
	}

	input = graphSnapshotTestInput()
	delete(input.SymbolByStableKey, "symbol-a")
	if _, err := NewGraphSnapshot(input); !errors.Is(err, ErrInvalidGraphSnapshot) {
		t.Fatalf("missing stable key index error = %v", err)
	}

	input = graphSnapshotTestInput()
	input.SymbolsByQName[5] = []SymbolID{0, 0}
	if _, err := NewGraphSnapshot(input); !errors.Is(err, ErrInvalidGraphSnapshot) {
		t.Fatalf("duplicate qualified-name result error = %v", err)
	}

	input = graphSnapshotTestInput()
	input.FileByRepoPath[RepoPathKey{Repository: 0, Path: 3}] = 1
	if _, err := NewGraphSnapshot(input); !errors.Is(err, ErrInvalidGraphSnapshot) {
		t.Fatalf("invalid file index error = %v", err)
	}

	input = graphSnapshotTestInput()
	input.ForwardEdges[0].Evidence = 1
	if _, err := NewGraphSnapshot(input); !errors.Is(err, ErrInvalidGraphSnapshot) {
		t.Fatalf("invalid evidence ID error = %v", err)
	}

	input = graphSnapshotTestInput()
	input.ReverseEdges[0].Target = 1
	if _, err := NewGraphSnapshot(input); !errors.Is(err, ErrInvalidGraphSnapshot) {
		t.Fatalf("invalid reverse counterpart error = %v", err)
	}
}

func TestGraphSnapshotAllowsEmptyTables(t *testing.T) {
	snapshot, err := NewGraphSnapshot(GraphSnapshotInput{
		CreatedAt: time.Unix(1, 0).UTC(),
		Version:   1,
	})
	if err != nil {
		t.Fatalf("NewGraphSnapshot() error = %v", err)
	}
	if counts := snapshot.Metadata().Counts; counts != (IDCounts{}) {
		t.Fatalf("Counts = %#v", counts)
	}
}

// TestGraphSnapshotPackageDependenciesReturnsIncomingOnly pins the direction
// of the package index: a package sees who depends on it, not what it depends
// on, and the returned rows never alias snapshot storage.
func TestGraphSnapshotPackageDependenciesReturnsIncomingOnly(t *testing.T) {
	rows := builderRows()
	rows.PackageDependencies = []PackageDependencyRow{
		{SourceKey: "pkg-a", TargetKey: "pkg-b", Kind: 20, Confidence: 5, Provenance: 3, EvidenceKey: "manifest-a"},
	}
	snapshot, err := BuildGraphSnapshot(rows, 7, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	source, target := packageIDByKey(t, snapshot, "pkg-a"), packageIDByKey(t, snapshot, "pkg-b")

	if outgoing := snapshot.PackageDependencies(source); len(outgoing) != 0 {
		t.Fatalf("PackageDependencies(pkg-a) = %#v, want no incoming relation", outgoing)
	}
	incoming := snapshot.PackageDependencies(target)
	if len(incoming) != 1 || incoming[0].Source != source || incoming[0].Kind != 20 {
		t.Fatalf("PackageDependencies(pkg-b) = %#v, want one relation from pkg-a", incoming)
	}
	incoming[0].Source = target
	if again := snapshot.PackageDependencies(target); again[0].Source != source {
		t.Fatalf("snapshot rows escaped through the getter: %#v", again[0])
	}
}

func packageIDByKey(t *testing.T, snapshot *GraphSnapshot, key string) PackageID {
	t.Helper()
	for id := PackageID(0); ; id++ {
		pkg, found := snapshot.Package(id)
		if !found {
			t.Fatalf("package %q is not in the snapshot", key)
		}
		if value, ok := snapshot.Strings().String(pkg.Key); ok && value == key {
			return id
		}
	}
}

func TestPackedEdgeIsCompact(t *testing.T) {
	if size := unsafe.Sizeof(PackedEdge{}); size < 12 || size > 16 {
		t.Fatalf("PackedEdge size = %d, want 12–16", size)
	}
}

func graphSnapshotTestInput() GraphSnapshotInput {
	interner := NewStringInterner()
	for _, value := range []string{"repository", "commit", "module", "src/parser.ts", "parse", "Parser.parse", "method", "signature", "evidence", "checker"} {
		if _, err := interner.Intern(value); err != nil {
			panic(err)
		}
	}
	return GraphSnapshotInput{
		ID:        42,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
		Version:   1,
		Strings:   interner.Freeze(),
		Repositories: []RepositoryRecord{
			{Name: 0, Commit: 1},
		},
		Packages: []PackageRecord{
			{Repository: 0, Name: 2, ModulePath: 2},
		},
		Files: []FileRecord{
			{Repository: 0, Package: 0, Path: 3},
		},
		Symbols: []SymbolRecord{
			{StableKey: "symbol-a", CanonicalIdentity: 4, File: 0, Name: 4, QualifiedName: 5, Kind: 6, Signature: 7},
			{StableKey: "symbol-b", CanonicalIdentity: 4, File: 0, Name: 4, QualifiedName: 5, Kind: 6, Signature: 7},
		},
		Evidence: []EvidenceRecord{
			{SourceFile: 0, TargetFile: 0, Kind: 8, Provenance: 9},
		},
		ForwardOffsets: []uint32{0, 1, 1},
		ForwardEdges:   []PackedEdge{{Target: 1, Evidence: 0}},
		ReverseOffsets: []uint32{0, 0, 1},
		ReverseEdges:   []PackedEdge{{Target: 0, Evidence: 0}},
		SymbolByStableKey: map[StableKey]SymbolID{
			"symbol-a": 0,
			"symbol-b": 1,
		},
		SymbolsByName: map[InternedString][]SymbolID{
			4: {0, 1},
		},
		SymbolsByQName: map[InternedString][]SymbolID{
			5: {0, 1},
		},
		FileByRepoPath: map[RepoPathKey]FileID{
			{Repository: 0, Path: 3}: 0,
		},
	}
}

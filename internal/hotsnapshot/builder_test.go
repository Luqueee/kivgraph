package hotsnapshot

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestBuildGraphSnapshotSortsRowsAndBuildsCompletePipeline(t *testing.T) {
	rows := builderRows()
	snapshot, err := BuildGraphSnapshot(rows, 9, time.Unix(1_700_000_001, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	if counts := snapshot.Metadata().Counts; counts != (IDCounts{Repositories: 2, Packages: 2, Files: 2, Symbols: 3, Evidence: 2, Edges: 2}) {
		t.Fatalf("Counts = %#v", counts)
	}
	if id, found := snapshot.SymbolByStableKey("s-a"); !found || id != 0 {
		t.Fatalf("s-a = %d, %t", id, found)
	}
	if id, found := snapshot.SymbolByStableKey("s-b"); !found || id != 1 {
		t.Fatalf("s-b = %d, %t", id, found)
	}
	if id, found := snapshot.SymbolByStableKey("s-c"); !found || id != 2 {
		t.Fatalf("s-c = %d, %t", id, found)
	}
	if outgoing := snapshot.Outgoing(0); len(outgoing) != 1 || outgoing[0].Target != 1 {
		t.Fatalf("Outgoing(0) = %v", outgoing)
	}
	if incoming := snapshot.Incoming(1); len(incoming) != 1 || incoming[0].Target != 0 {
		t.Fatalf("Incoming(1) = %v", incoming)
	}
	if _, found := snapshot.Strings().Lookup("LADYBUGDB"); !found {
		t.Fatal("provenance was not interned")
	}
	if len(snapshot.SymbolsByName(4)) != 2 {
		t.Fatalf("SymbolsByName(shared) = %v", snapshot.SymbolsByName(4))
	}
}

func TestBuildGraphSnapshotIsDeterministicForUnorderedRows(t *testing.T) {
	first, err := BuildGraphSnapshot(builderRows(), 9, time.Unix(1_700_000_001, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	rows := builderRows()
	rows.Repositories[0], rows.Repositories[1] = rows.Repositories[1], rows.Repositories[0]
	rows.Packages[0], rows.Packages[1] = rows.Packages[1], rows.Packages[0]
	rows.Files[0], rows.Files[1] = rows.Files[1], rows.Files[0]
	rows.Symbols[0], rows.Symbols[2] = rows.Symbols[2], rows.Symbols[0]
	rows.Edges[0], rows.Edges[1] = rows.Edges[1], rows.Edges[0]
	second, err := BuildGraphSnapshot(rows, 9, time.Unix(1_700_000_001, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first.metadata, second.metadata) || !reflect.DeepEqual(first.repositories, second.repositories) || !reflect.DeepEqual(first.packages, second.packages) || !reflect.DeepEqual(first.files, second.files) || !reflect.DeepEqual(first.symbols, second.symbols) || !reflect.DeepEqual(first.evidence, second.evidence) || !reflect.DeepEqual(first.forwardOffsets, second.forwardOffsets) || !reflect.DeepEqual(first.forwardEdges, second.forwardEdges) || !reflect.DeepEqual(first.reverseOffsets, second.reverseOffsets) || !reflect.DeepEqual(first.reverseEdges, second.reverseEdges) {
		t.Fatal("shuffled rows produced a different snapshot")
	}
	firstStrings, err := first.Strings().MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	secondStrings, err := second.Strings().MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(firstStrings, secondStrings) {
		t.Fatal("shuffled rows produced a different string table")
	}
}

func TestBuildGraphSnapshotRejectsDanglingAndDuplicateRows(t *testing.T) {
	rows := builderRows()
	rows.Symbols[0].FileKey = "missing"
	if _, err := BuildGraphSnapshot(rows, 1, time.Unix(1, 0).UTC(), 1); !errors.Is(err, ErrInvalidSnapshotRows) {
		t.Fatalf("dangling file error = %v", err)
	}

	rows = builderRows()
	rows.Files = append(rows.Files, rows.Files[0])
	if _, err := BuildGraphSnapshot(rows, 1, time.Unix(1, 0).UTC(), 1); !errors.Is(err, ErrInvalidSnapshotRows) {
		t.Fatalf("duplicate file error = %v", err)
	}

	rows = builderRows()
	rows.Edges[0].TargetKey = "missing"
	if _, err := BuildGraphSnapshot(rows, 1, time.Unix(1, 0).UTC(), 1); !errors.Is(err, ErrInvalidSnapshotRows) {
		t.Fatalf("dangling edge error = %v", err)
	}
}

// TestBuildGraphSnapshotAcceptsAbsentDescriptiveFields fixes the line between
// identity and description. A checkout without git metadata has no commit, an
// npm package has no Go module path, and a constant has no signature: real
// graphs carry those gaps, and rejecting them would make the snapshot
// unbuildable from the definitive graph.
func TestBuildGraphSnapshotAcceptsAbsentDescriptiveFields(t *testing.T) {
	rows := builderRows()
	rows.Repositories[0].Commit = ""
	rows.Repositories[1].Commit = ""
	rows.Packages[0].ModulePath = ""
	rows.Symbols[0].Signature = ""

	snapshot, err := BuildGraphSnapshot(rows, 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v, want a snapshot: commit, module path and signature are descriptive, not identity", err)
	}

	// The gaps must survive as gaps, never as invented placeholder text.
	id, ok := snapshot.SymbolByStableKey("s-a")
	if !ok {
		t.Fatal("SymbolByStableKey(s-a) missing")
	}
	symbol, ok := snapshot.Symbol(id)
	if !ok {
		t.Fatal("Symbol() missing")
	}
	signature, ok := snapshot.Strings().String(symbol.Signature)
	if !ok || signature != "(): void" {
		t.Fatalf("signature = %q, %v; want the row value preserved", signature, ok)
	}
}

// TestBuildGraphSnapshotRejectsMissingIdentity keeps the other half of that
// line: a row without the fields the snapshot indexes by is not a gap, it is
// an unusable row.
func TestBuildGraphSnapshotRejectsMissingIdentity(t *testing.T) {
	for name, mutate := range map[string]func(*LadybugSnapshotRows){
		"repository without key":  func(rows *LadybugSnapshotRows) { rows.Repositories[0].Key = "" },
		"repository without name": func(rows *LadybugSnapshotRows) { rows.Repositories[0].Name = "" },
		"package without name":    func(rows *LadybugSnapshotRows) { rows.Packages[0].Name = "" },
		"file without path":       func(rows *LadybugSnapshotRows) { rows.Files[0].Path = "" },
		"symbol without key":      func(rows *LadybugSnapshotRows) { rows.Symbols[0].StableKey = "" },
		"symbol without name":     func(rows *LadybugSnapshotRows) { rows.Symbols[0].Name = "" },
		"symbol without kind":     func(rows *LadybugSnapshotRows) { rows.Symbols[0].Kind = "" },
		"symbol without identity": func(rows *LadybugSnapshotRows) { rows.Symbols[0].CanonicalIdentity = "" },
		"edge without evidence":   func(rows *LadybugSnapshotRows) { rows.Edges[0].EvidenceKind = "" },
	} {
		rows := builderRows()
		mutate(&rows)
		if _, err := BuildGraphSnapshot(rows, 1, time.Unix(1, 0).UTC(), 1); !errors.Is(err, ErrInvalidSnapshotRows) {
			t.Errorf("%s: error = %v, want ErrInvalidSnapshotRows", name, err)
		}
	}
}

func builderRows() LadybugSnapshotRows {
	return LadybugSnapshotRows{
		Repositories: []RepositoryRow{
			{Key: "repo-b", Name: "repo-b", Commit: "commit-b"},
			{Key: "repo-a", Name: "repo-a", Commit: "commit-a"},
		},
		Packages: []PackageRow{
			{Key: "pkg-b", RepositoryKey: "repo-b", Name: "shared", ModulePath: "example.com/b"},
			{Key: "pkg-a", RepositoryKey: "repo-a", Name: "shared", ModulePath: "example.com/a"},
		},
		Files: []FileRow{
			{Key: "file-b", RepositoryKey: "repo-b", PackageKey: "pkg-b", Path: "src/b.ts"},
			{Key: "file-a", RepositoryKey: "repo-a", PackageKey: "pkg-a", Path: "src/a.ts"},
		},
		Symbols: []SymbolRow{
			{StableKey: "s-c", CanonicalIdentity: "identity-c", FileKey: "file-b", Name: "c", QualifiedName: "C", Kind: "function", Signature: "(): void"},
			{StableKey: "s-a", CanonicalIdentity: "identity-a", FileKey: "file-a", Name: "shared", QualifiedName: "A.shared", Kind: "function", Signature: "(): void"},
			{StableKey: "s-b", CanonicalIdentity: "identity-b", FileKey: "file-a", Name: "shared", QualifiedName: "B.shared", Kind: "function", Signature: "(): void"},
		},
		Edges: []EdgeRow{
			{SourceKey: "s-b", TargetKey: "s-c", Kind: 2, Confidence: 8, Provenance: 3, EvidenceKind: "checker", EvidenceSourceFileKey: "file-a", EvidenceTargetFileKey: "file-b"},
			{SourceKey: "s-a", TargetKey: "s-b", Kind: 1, Confidence: 9, Provenance: 2, EvidenceKind: "checker", EvidenceSourceFileKey: "file-a", EvidenceTargetFileKey: "file-a"},
		},
	}
}

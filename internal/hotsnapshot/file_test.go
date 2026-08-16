package hotsnapshot

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"reflect"
	"testing"
	"time"
)

// TestSnapshotFileRoundTripPreservesEveryField is the oracle for this format. A
// snapshot written and read back has to be indistinguishable from the one that
// was built, table by table and field by field, and the fixture below exists to
// make that claim mean something: every field of every record carries a
// distinct non-zero value, so a field this format forgets to write, or writes
// into the wrong offset, cannot round trip as a zero that compares equal.
func TestSnapshotFileRoundTripPreservesEveryField(t *testing.T) {
	built, err := BuildGraphSnapshot(fileFixtureRows(), 7, time.Unix(1_700_000_123, 0).UTC(), 3)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	contentDigest := sha256.Sum256([]byte("content"))

	// Comparing two empty tables proves nothing, so the fixture's own shape is
	// asserted before anything is compared against it. A table that stops being
	// populated has to fail here rather than pass everything below.
	counts := built.metadata.Counts
	for _, populated := range []struct {
		name  string
		count uint64
	}{
		{"repositories", uint64(counts.Repositories)}, {"packages", uint64(counts.Packages)},
		{"files", uint64(counts.Files)}, {"symbols", uint64(counts.Symbols)},
		{"evidence", counts.Evidence}, {"edges", counts.Edges},
		{"package edges", counts.PackageEdges}, {"unresolved", counts.Unresolved},
	} {
		if populated.count == 0 {
			t.Fatalf("the fixture produced no %s, so this test cannot detect a format that drops them", populated.name)
		}
	}
	if len(built.forwardEdges) == 0 || len(built.reverseEdges) == 0 || len(built.packageIncoming) == 0 {
		t.Fatal("the fixture produced an empty CSR or package index")
	}

	var buffer bytes.Buffer
	payloadDigest, err := WriteSnapshot(&buffer, built, contentDigest)
	if err != nil {
		t.Fatalf("write: %v", err)
	}
	loaded, err := ReadSnapshot(buffer.Bytes(), contentDigest)
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	if loaded.metadata != built.metadata {
		t.Errorf("metadata\n got %+v\nwant %+v", loaded.metadata, built.metadata)
	}
	for _, table := range []struct {
		name        string
		got, expect any
	}{
		{"repositories", loaded.repositories, built.repositories},
		{"packages", loaded.packages, built.packages},
		{"files", loaded.files, built.files},
		{"symbols", loaded.symbols, built.symbols},
		{"evidence", loaded.evidence, built.evidence},
		{"packageDependencies", loaded.packageDependencies, built.packageDependencies},
		{"packageIncoming", loaded.packageIncoming, built.packageIncoming},
		{"unresolved", loaded.unresolved, built.unresolved},
		{"forwardOffsets", loaded.forwardOffsets, built.forwardOffsets},
		{"forwardEdges", loaded.forwardEdges, built.forwardEdges},
		{"reverseOffsets", loaded.reverseOffsets, built.reverseOffsets},
		{"reverseEdges", loaded.reverseEdges, built.reverseEdges},
		{"symbolByStableKey", loaded.symbolByStableKey, built.symbolByStableKey},
		{"symbolsByName", loaded.symbolsByName, built.symbolsByName},
		{"symbolsByQName", loaded.symbolsByQName, built.symbolsByQName},
		{"fileByRepoPath", loaded.fileByRepoPath, built.fileByRepoPath},
	} {
		if !reflect.DeepEqual(table.got, table.expect) {
			t.Errorf("%s differs\n got %+v\nwant %+v", table.name, table.got, table.expect)
		}
	}

	// The string table is compared through its own surface rather than by
	// deep equality: it owns private storage whose shape is not the contract,
	// while every id resolving to the same value is.
	if loaded.strings.Stats() != built.strings.Stats() {
		t.Errorf("string table stats\n got %+v\nwant %+v", loaded.strings.Stats(), built.strings.Stats())
	}
	for id := range InternedString(built.strings.Stats().Entries) {
		want, _ := built.strings.String(id)
		got, ok := loaded.strings.String(id)
		if !ok || got != want {
			t.Fatalf("interned string %d is %q (%v), expected %q", id, got, ok, want)
		}
	}

	// Rewriting the same snapshot has to produce the same payload digest:
	// nothing in this format may depend on map iteration order.
	var second bytes.Buffer
	secondDigest, err := WriteSnapshot(&second, built, contentDigest)
	if err != nil {
		t.Fatalf("second write: %v", err)
	}
	if secondDigest != payloadDigest || !bytes.Equal(second.Bytes(), buffer.Bytes()) {
		t.Error("writing the same snapshot twice produced different bytes")
	}
}

// TestSnapshotFileFailsClosed keeps every rejection a rejection. A server that
// cannot trust the file derives the snapshot again, so the only thing this
// format must never do is accept a file it cannot vouch for.
func TestSnapshotFileFailsClosed(t *testing.T) {
	built, err := BuildGraphSnapshot(fileFixtureRows(), 7, time.Unix(1_700_000_123, 0).UTC(), 3)
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	contentDigest := sha256.Sum256([]byte("content"))
	var buffer bytes.Buffer
	if _, err := WriteSnapshot(&buffer, built, contentDigest); err != nil {
		t.Fatalf("write: %v", err)
	}
	original := buffer.Bytes()

	for _, testCase := range []struct {
		name    string
		corrupt func([]byte) []byte
	}{
		{"empty", func([]byte) []byte { return nil }},
		{"header only", func(data []byte) []byte { return data[:snapshotFileHeaderSize] }},
		{"truncated payload", func(data []byte) []byte { return data[:len(data)-1] }},
		{"foreign magic", func(data []byte) []byte {
			copy(data[0:8], []byte("LGVB\x00\x00\x00\x00"))
			return data
		}},
		{"unknown format version", func(data []byte) []byte {
			data[8] = byte(SnapshotFileFormatVersion + 1)
			return data
		}},
		{"flipped payload byte", func(data []byte) []byte {
			data[len(data)-1] ^= 0xFF
			return data
		}},
		{"section length lies", func(data []byte) []byte {
			// The first section entry's count, which no longer divides its
			// declared length.
			data[snapshotFileHeaderSize+8]++
			return data
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			data := testCase.corrupt(append([]byte(nil), original...))
			if _, err := ReadSnapshot(data, contentDigest); !errors.Is(err, ErrInvalidSnapshotFile) {
				t.Fatalf("expected ErrInvalidSnapshotFile, got %v", err)
			}
		})
	}

	t.Run("content digest of another generation", func(t *testing.T) {
		other := sha256.Sum256([]byte("another generation"))
		if _, err := ReadSnapshot(original, other); !errors.Is(err, ErrInvalidSnapshotFile) {
			t.Fatalf("expected ErrInvalidSnapshotFile, got %v", err)
		}
	})

	t.Run("no digest asserted", func(t *testing.T) {
		if _, err := ReadSnapshot(original, [sha256.Size]byte{}); err != nil {
			t.Fatalf("a zero digest asserts nothing and must be accepted: %v", err)
		}
	})
}

// TestIndexSnapshotInputRejectsDuplicateIdentities defends the one class of
// corruption that validation downstream cannot catch: an index built from
// duplicated identities agrees with the tables it was built from, so the
// snapshot would validate and answer one symbol's question with another's id.
func TestIndexSnapshotInputRejectsDuplicateIdentities(t *testing.T) {
	duplicateKeys := GraphSnapshotInput{Symbols: []SymbolRecord{
		{StableKey: "s-a", Name: 1, QualifiedName: 2},
		{StableKey: "s-a", Name: 3, QualifiedName: 4},
	}}
	if err := indexSnapshotInput(&duplicateKeys); !errors.Is(err, ErrInvalidSnapshotFile) {
		t.Fatalf("duplicate stable key: expected ErrInvalidSnapshotFile, got %v", err)
	}
	duplicatePaths := GraphSnapshotInput{Files: []FileRecord{
		{Repository: 0, Path: 7},
		{Repository: 0, Path: 7},
	}}
	if err := indexSnapshotInput(&duplicatePaths); !errors.Is(err, ErrInvalidSnapshotFile) {
		t.Fatalf("duplicate repository path: expected ErrInvalidSnapshotFile, got %v", err)
	}
}

// TestRestoreSymbolKeysRejectsImpossibleRanges guards the one section a length
// check cannot cover: the key blob is bytes, so only its offsets say where a
// symbol's identity starts and ends.
func TestRestoreSymbolKeysRejectsImpossibleRanges(t *testing.T) {
	records := make([]SymbolRecord, 2)
	for _, testCase := range []struct {
		name    string
		offsets []uint32
		blob    string
	}{
		{"too few offsets", []uint32{0, 3}, "abcdef"},
		{"descending range", []uint32{3, 0, 6}, "abcdef"},
		{"range past the blob", []uint32{0, 3, 99}, "abcdef"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if err := restoreSymbolKeys(records, testCase.offsets, []byte(testCase.blob)); !errors.Is(err, ErrInvalidSnapshotFile) {
				t.Fatalf("expected ErrInvalidSnapshotFile, got %v", err)
			}
		})
	}
}

// fileFixtureRows populates every field of every row type with a distinct
// non-zero value. A fixture that leaves a field at its zero value cannot tell a
// format that drops it from one that keeps it.
func fileFixtureRows() LadybugSnapshotRows {
	return LadybugSnapshotRows{
		SchemaVersion:   2,
		ResolverVersion: "resolver-9",
		Repositories: []RepositoryRow{
			{Key: "repo-a", Name: "name-a", Path: "/src/a", Languages: "go,typescript", Commit: "commit-a", Branch: "main", Dirty: true},
			{Key: "repo-b", Name: "name-b", Path: "/src/b", Languages: "rust", Commit: "commit-b", Branch: "release", Dirty: false},
		},
		Packages: []PackageRow{
			{Key: "pkg-a", RepositoryKey: "repo-a", Language: "go", Name: "alpha", ModulePath: "example.com/a"},
			{Key: "pkg-b", RepositoryKey: "repo-b", Language: "rust", Name: "beta", ModulePath: "example.com/b"},
		},
		Files: []FileRow{
			{Key: "file-a", RepositoryKey: "repo-a", PackageKey: "pkg-a", Path: "src/a.go", Language: "go",
				ContentHash: "1111111111111111111111111111111111111111111111111111111111111111"},
			{Key: "file-b", RepositoryKey: "repo-b", PackageKey: "pkg-b", Path: "src/b.rs", Language: "rust",
				ContentHash: "2222222222222222222222222222222222222222222222222222222222222222"},
		},
		Symbols: []SymbolRow{
			{StableKey: "s-a", CanonicalIdentity: "identity-a", FileKey: "file-a", Language: "go",
				Name: "Alpha", QualifiedName: "alpha.Alpha", Kind: "function", Signature: "func()", Exported: true, StartLine: 11, EndLine: 21},
			{StableKey: "s-b", CanonicalIdentity: "identity-b", FileKey: "file-b", Language: "rust",
				Name: "beta", QualifiedName: "beta::beta", Kind: "struct", Signature: "struct beta", Exported: false, StartLine: 31, EndLine: 41},
			{StableKey: "s-c", CanonicalIdentity: "identity-c", FileKey: "file-a", Language: "go",
				Name: "Alpha", QualifiedName: "alpha.Other", Kind: "method", Signature: "func() error", Exported: true, StartLine: 51, EndLine: 61},
		},
		Edges: []EdgeRow{
			{SourceKey: "s-a", TargetKey: "s-b", Kind: 1, Confidence: 9, Provenance: 2, Flags: 3,
				EvidenceKey: "ev-1", EvidenceKind: "checker", EvidenceSourceFileKey: "file-a", EvidenceTargetFileKey: "file-b"},
			{SourceKey: "s-b", TargetKey: "s-c", Kind: 4, Confidence: 7, Provenance: 5, Flags: 6,
				EvidenceKey: "ev-2", EvidenceKind: "ast", EvidenceSourceFileKey: "file-b", EvidenceTargetFileKey: "file-a"},
		},
		PackageDependencies: []PackageDependencyRow{
			{SourceKey: "pkg-a", TargetKey: "pkg-b", Kind: 2, Confidence: 8, Provenance: 4, EvidenceKey: "dep-1"},
		},
		Unresolved: []UnresolvedReferenceRow{
			{Key: "unres-1", RepositoryKey: "repo-a", FileKey: "file-a", SourceKey: "s-a", Language: "go",
				RequestedPackage: "example.com/missing", RequestedSymbol: "Missing", Reason: "PACKAGE_NOT_FOUND",
				Detail: "no provider", StartLine: 71, StartColumn: 8, StartOffset: 900},
			{Key: "unres-2", RepositoryKey: "repo-b", Language: "rust", RequestedPackage: "missing-crate",
				Reason: "CRATE_PROVIDER_NOT_FOUND", Detail: "nobody declares it"},
		},
	}
}

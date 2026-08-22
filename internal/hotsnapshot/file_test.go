package hotsnapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"reflect"
	"testing"
	"time"
	"unsafe"
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
	if len(built.forwardEdges) == 0 || len(built.reverseEdges) == 0 || len(built.packageIncoming.values) == 0 {
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
	// Every value has to resolve back to the same id. A lookup order that is
	// valid but wrong -- sorted, in range, and simply not this table's -- would
	// pass every check above and answer every query with another value's id.
	if !reflect.DeepEqual(loaded.strings.order, built.strings.order) {
		t.Errorf("lookup order differs")
	}
	for id := range InternedString(built.strings.Stats().Entries) {
		value, _ := built.strings.String(id)
		got, found := loaded.strings.Lookup(value)
		if !found || got != id {
			t.Fatalf("Lookup(%q) = %d (%v), want %d", value, got, found, id)
		}
	}

	// The key table is compared through its own surface for the same reason,
	// and in both directions: an id resolving to the same key proves the arena
	// survived, and that key resolving back to the same id proves the order
	// did -- a reordered table keeps answering, with another symbol's id.
	if loaded.StableKeys().Stats() != built.StableKeys().Stats() {
		t.Errorf("stable key table stats\n got %+v\nwant %+v", loaded.StableKeys().Stats(), built.StableKeys().Stats())
	}
	for id := range StableKeyID(built.StableKeys().Entries()) {
		want, _ := built.StableKey(id)
		got, ok := loaded.StableKey(id)
		if !ok || got != want {
			t.Fatalf("stable key %d is %q (%v), expected %q", id, got, ok, want)
		}
		if resolved, found := loaded.SymbolByStableKey(want); !found || resolved != SymbolID(id) {
			t.Fatalf("SymbolByStableKey(%q) = %d (%v), want %d", want, resolved, found, id)
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

// TestSnapshotLoadRejectsDuplicateIdentities defends the one class of
// corruption that validation downstream cannot catch: an index built from
// duplicated identities agrees with the tables it was built from, so the
// snapshot would validate and answer one symbol's question with another's id.
//
// A duplicated stable key is refused by the key table rather than by
// indexSnapshotInput, because two equal keys are not in strict byte order.
func TestSnapshotLoadRejectsDuplicateIdentities(t *testing.T) {
	if _, err := restoreSymbolKeys(2, []uint32{0, 3, 6}, []byte("s-as-a"), false); !errors.Is(err, ErrInvalidSnapshotFile) {
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
	const symbols = 2
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
			if _, err := restoreSymbolKeys(symbols, testCase.offsets, []byte(testCase.blob), false); !errors.Is(err, ErrInvalidSnapshotFile) {
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

// TestStringTableOrderIsValidatedOnLoad guards the one section whose corruption
// is silent. Values and records are checked by their widths and by the snapshot
// validation that follows; a lookup order is just numbers, and a wrong one keeps
// answering -- with another value's id.
func TestStringTableOrderIsValidatedOnLoad(t *testing.T) {
	interner := NewStringInterner()
	for _, value := range []string{"delta", "alpha", "charlie", "bravo"} {
		if _, err := interner.Intern(value); err != nil {
			t.Fatalf("intern %q: %v", value, err)
		}
	}
	table := interner.Freeze()
	values, err := table.MarshalBinary()
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	if restored, err := unmarshalStringTableWithOrder(values, table.order); err != nil {
		t.Fatalf("the table's own order was refused: %v", err)
	} else if id, found := restored.Lookup("charlie"); !found || id != 2 {
		t.Fatalf(`Lookup("charlie") = %d (%v), want 2`, id, found)
	}

	for _, testCase := range []struct {
		name  string
		order []InternedString
	}{
		{"too short", []InternedString{0, 1, 2}},
		{"too long", []InternedString{1, 3, 2, 0, 0}},
		{"id out of range", []InternedString{1, 3, 2, 9}},
		{"not sorted by value", []InternedString{0, 1, 2, 3}},
		{"a repeated id", []InternedString{1, 1, 2, 0}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := unmarshalStringTableWithOrder(values, testCase.order); !errors.Is(err, ErrMalformedStringTable) {
				t.Fatalf("expected ErrMalformedStringTable, got %v", err)
			}
		})
	}
}

// TestReadStringTableSortsWithoutAnOrderSection keeps the optional section
// optional: a file that carries the values but not their order has to keep
// loading, and sorting is exactly what that costs.
func TestReadStringTableSortsWithoutAnOrderSection(t *testing.T) {
	interner := NewStringInterner()
	for _, value := range []string{"zulu", "kilo", "alpha"} {
		if _, err := interner.Intern(value); err != nil {
			t.Fatalf("intern %q: %v", value, err)
		}
	}
	frozen := interner.Freeze()
	table, err := readStringTable(frozen.arena, frozen.offsets, nil, nil, false)
	if err != nil {
		t.Fatalf("readStringTable without an order: %v", err)
	}
	for id, value := range map[InternedString]string{0: "zulu", 1: "kilo", 2: "alpha"} {
		if got, found := table.Lookup(value); !found || got != id {
			t.Fatalf("Lookup(%q) = %d (%v), want %d", value, got, found, id)
		}
	}
	if _, err := readStringTable(nil, nil, nil, nil, false); !errors.Is(err, ErrInvalidSnapshotFile) {
		t.Fatalf("a file with no string table must be refused, got %v", err)
	}
}

// TestAMappedTableCopiesWhatItHandsOut is the whole safety argument of
// MapSnapshot, and it is checked by address rather than by value: a borrowed
// arena is memory the collector cannot see, so a returned string that pointed
// into it would keep answering after the mapping went away. Comparing the bytes
// would pass either way; comparing where they live is what distinguishes a copy
// from a view.
func TestAMappedTableCopiesWhatItHandsOut(t *testing.T) {
	interner := NewStringInterner()
	if _, err := interner.Intern("borrowed-value"); err != nil {
		t.Fatalf("intern: %v", err)
	}
	frozen := interner.Freeze()

	owned, err := StringTableFromArena(frozen.arena, frozen.offsets, frozen.order, false)
	if err != nil {
		t.Fatalf("owned table: %v", err)
	}
	borrowed, err := StringTableFromArena(frozen.arena, frozen.offsets, frozen.order, true)
	if err != nil {
		t.Fatalf("borrowed table: %v", err)
	}

	arenaStart := uintptr(unsafe.Pointer(unsafe.SliceData(frozen.arena)))
	arenaEnd := arenaStart + uintptr(len(frozen.arena))
	inside := func(value string) bool {
		at := uintptr(unsafe.Pointer(unsafe.StringData(value)))
		return at >= arenaStart && at < arenaEnd
	}

	ownedValue, ok := owned.String(0)
	if !ok || ownedValue != "borrowed-value" {
		t.Fatalf("owned String(0) = %q (%v)", ownedValue, ok)
	}
	if !inside(ownedValue) {
		t.Error("a table that owns its arena copied a value it could have handed out in place")
	}
	borrowedValue, ok := borrowed.String(0)
	if !ok || borrowedValue != "borrowed-value" {
		t.Fatalf("borrowed String(0) = %q (%v)", borrowedValue, ok)
	}
	if inside(borrowedValue) {
		t.Fatal("a borrowed arena handed out a view into memory the collector cannot see")
	}
}

// TestSnapshotFileRefusesAMalformedSectionTable covers the entries in the table
// rather than the bytes they point at.
//
// Every case here passed every check the reader had before this test existed, and
// two of them did not produce a wrong answer -- they produced a panic, which is
// worse than a wrong answer because it is not a refusal at all. A file that can
// panic a reader is a file that gets served by nobody and crashes everybody.
func TestSnapshotFileRefusesAMalformedSectionTable(t *testing.T) {
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

	// entryOf finds the table entry for a kind so a case can corrupt one field
	// of one section without hand-computing an offset.
	entryOf := func(data []byte, kind uint32) int {
		count := int(binary.LittleEndian.Uint32(data[12:16]))
		for index := range count {
			at := snapshotFileHeaderSize + index*sectionEntrySize
			if binary.LittleEndian.Uint32(data[at:at+4]) == kind {
				return at
			}
		}
		t.Fatalf("section %d not in the table", kind)
		return 0
	}

	for name, corrupt := range map[string]func([]byte){
		// Claiming one byte per symbol keeps count*elemSize == length, so the
		// length check agrees. decodeSymbols then reads 37 bytes per record.
		"symbol records one byte wide": func(data []byte) {
			at := entryOf(data, sectionSymbols)
			count := binary.LittleEndian.Uint64(data[at+8 : at+16])
			length := binary.LittleEndian.Uint64(data[at+24 : at+32])
			binary.LittleEndian.PutUint32(data[at+4:at+8], 1)
			binary.LittleEndian.PutUint64(data[at+8:at+16], length)
			_ = count
		},
		// 2^61 records of 8 bytes multiplies to exactly zero, which matches a
		// zero length. The count survives into the decoder.
		"record count that overflows its own product": func(data []byte) {
			at := entryOf(data, sectionForwardOffsets)
			binary.LittleEndian.PutUint32(data[at+4:at+8], 8)
			binary.LittleEndian.PutUint64(data[at+8:at+16], 1<<61)
			binary.LittleEndian.PutUint64(data[at+24:at+32], 0)
		},
		"a kind that appears twice": func(data []byte) {
			source := entryOf(data, sectionEvidence)
			target := entryOf(data, sectionUnresolved)
			copy(data[target:target+sectionEntrySize], data[source:source+sectionEntrySize])
		},
		"two sections sharing bytes": func(data []byte) {
			at := entryOf(data, sectionReverseEdges)
			forward := entryOf(data, sectionForwardEdges)
			copy(data[at+16:at+24], data[forward+16:forward+24])
		},
		"a section that does not start on its alignment": func(data []byte) {
			at := entryOf(data, sectionSymbols)
			offset := binary.LittleEndian.Uint64(data[at+16 : at+24])
			binary.LittleEndian.PutUint64(data[at+16:at+24], offset+1)
		},
	} {
		t.Run(name, func(t *testing.T) {
			data := append([]byte(nil), original...)
			corrupt(data)
			snapshot, err := ReadSnapshot(data, contentDigest)
			if !errors.Is(err, ErrInvalidSnapshotFile) {
				t.Fatalf("ReadSnapshot() error = %v, want %v", err, ErrInvalidSnapshotFile)
			}
			if snapshot != nil {
				t.Fatal("a malformed file produced a snapshot")
			}
		})
	}
}

// TestSnapshotFileIsDeterministicApartFromItsProvenance is the cheapest
// regression this format has, and it fixes the exact claim that is true.
//
// "Two publications of the same graph produce the same file" is false, and
// measuring it said by how much: on a 98 MB file over kena, six bytes. They are
// the snapshot id and the build timestamp, which identify *which* publication
// this is -- a second publication is a different generation at a different time,
// and recording that is provenance, not nondeterminism.
//
// So the assertion is the precise one: the payload is identical byte for byte,
// and the header differs in nothing except those two fields. That is stronger
// than comparing whole files, because it enumerates what is allowed to vary. A
// new field fed by a map iteration, a pointer address or a clock fails here.
func TestSnapshotFileIsDeterministicApartFromItsProvenance(t *testing.T) {
	rows := fileFixtureRows()
	contentDigest := sha256.Sum256([]byte("content"))

	write := func(id uint64, at time.Time) []byte {
		built, err := BuildGraphSnapshot(rows, id, at, 3)
		if err != nil {
			t.Fatalf("build: %v", err)
		}
		var buffer bytes.Buffer
		if _, err := WriteSnapshot(&buffer, built, contentDigest); err != nil {
			t.Fatalf("write: %v", err)
		}
		return buffer.Bytes()
	}

	first := write(7, time.Unix(1_700_000_123, 0).UTC())
	again := write(7, time.Unix(1_700_000_123, 0).UTC())
	if !bytes.Equal(first, again) {
		t.Fatal("the same graph published under the same id and time produced different bytes")
	}

	// Same graph, different publication.
	other := write(8, time.Unix(1_700_009_999, 0).UTC())
	if len(other) != len(first) {
		t.Fatalf("length changed with provenance: %d then %d", len(first), len(other))
	}
	const idAt, createdAt = 16, 24
	for index := range first {
		provenance := (index >= idAt && index < idAt+8) || (index >= createdAt && index < createdAt+8)
		if provenance {
			continue
		}
		if first[index] != other[index] {
			t.Fatalf("byte %d differs between two publications of the same graph", index)
		}
	}
	if bytes.Equal(first[idAt:idAt+8], other[idAt:idAt+8]) ||
		bytes.Equal(first[createdAt:createdAt+8], other[createdAt:createdAt+8]) {
		t.Fatal("the fixture did not actually change the provenance, so this proves nothing")
	}
}

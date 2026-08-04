package hotsnapshot

import (
	"errors"
	"math"
	"time"
)

var ErrInvalidGraphSnapshot = errors.New("invalid graph snapshot")

// RepositoryRecord stores the interned identity of one indexed repository.
type RepositoryRecord struct {
	Name   InternedString
	Commit InternedString
}

// PackageRecord belongs to one repository.
type PackageRecord struct {
	Repository RepositoryID
	Name       InternedString
	ModulePath InternedString
}

// FileRecord belongs to one repository and package.
type FileRecord struct {
	Repository RepositoryID
	Package    PackageID
	Path       InternedString
}

// SymbolRecord stores source-independent and display identities for a symbol.
type SymbolRecord struct {
	StableKey          StableKey
	CanonicalIdentity  InternedString
	File               FileID
	Name               InternedString
	QualifiedName      InternedString
	Kind               InternedString
	Signature          InternedString
	StartLine, EndLine uint32
}

// EvidenceRecord describes the source that supports an edge.
type EvidenceRecord struct {
	SourceFile FileID
	TargetFile FileID
	Kind       InternedString
	Provenance InternedString
}

// PackedEdge keeps adjacency compact while preserving its exact target and
// evidence. Its 16-byte layout is suitable for CSR edge arrays.
type PackedEdge struct {
	Target     SymbolID
	Evidence   EvidenceID
	Kind       uint8
	Confidence uint8
	Provenance uint8
	Flags      uint8
}

// RepoPathKey indexes a file by its exact repository/path pair.
type RepoPathKey struct {
	Repository RepositoryID
	Path       InternedString
}

// GraphSnapshotInput transfers one fully constructed graph to NewGraphSnapshot.
// It is mutable by design; NewGraphSnapshot copies every slice and map before
// publishing the immutable GraphSnapshot.
type GraphSnapshotInput struct {
	ID        uint64
	CreatedAt time.Time
	Version   uint32

	Strings      StringTable
	Repositories []RepositoryRecord
	Packages     []PackageRecord
	Files        []FileRecord
	Symbols      []SymbolRecord
	Evidence     []EvidenceRecord

	ForwardOffsets []uint32
	ForwardEdges   []PackedEdge
	ReverseOffsets []uint32
	ReverseEdges   []PackedEdge

	SymbolByStableKey map[StableKey]SymbolID
	SymbolsByName     map[InternedString][]SymbolID
	SymbolsByQName    map[InternedString][]SymbolID
	FileByRepoPath    map[RepoPathKey]FileID
}

// SnapshotMetadata contains versioned, immutable snapshot metadata.
type SnapshotMetadata struct {
	ID        uint64
	CreatedAt time.Time
	Version   uint32
	Counts    IDCounts
}

// GraphSnapshot is an immutable in-memory graph. Its fields remain private so
// readers cannot mutate tables or indexes after publication.
type GraphSnapshot struct {
	metadata SnapshotMetadata
	strings  StringTable

	repositories []RepositoryRecord
	packages     []PackageRecord
	files        []FileRecord
	symbols      []SymbolRecord
	evidence     []EvidenceRecord

	forwardOffsets []uint32
	forwardEdges   []PackedEdge
	reverseOffsets []uint32
	reverseEdges   []PackedEdge

	symbolByStableKey map[StableKey]SymbolID
	symbolsByName     map[InternedString][]SymbolID
	symbolsByQName    map[InternedString][]SymbolID
	fileByRepoPath    map[RepoPathKey]FileID
}

// NewGraphSnapshot validates the supplied graph envelope and copies all mutable
// inputs. The caller can therefore reuse or mutate GraphSnapshotInput after the
// call without changing the returned snapshot.
func NewGraphSnapshot(input GraphSnapshotInput) (*GraphSnapshot, error) {
	if input.Version == 0 || input.CreatedAt.IsZero() ||
		!fitsDenseTable(input.Repositories) || !fitsDenseTable(input.Packages) ||
		!fitsDenseTable(input.Files) || !fitsDenseTable(input.Symbols) ||
		!fitsDenseTable(input.Evidence) ||
		len(input.ForwardEdges) != len(input.ReverseEdges) {
		return nil, ErrInvalidGraphSnapshot
	}

	counts := IDCounts{
		Repositories: uint64(len(input.Repositories)),
		Packages:     uint64(len(input.Packages)),
		Files:        uint64(len(input.Files)),
		Symbols:      uint64(len(input.Symbols)),
		Evidence:     uint64(len(input.Evidence)),
		Edges:        uint64(len(input.ForwardEdges)),
	}
	snapshot := &GraphSnapshot{
		metadata: SnapshotMetadata{ID: input.ID, CreatedAt: input.CreatedAt, Version: input.Version, Counts: counts},
		strings:  input.Strings,

		repositories: append([]RepositoryRecord(nil), input.Repositories...),
		packages:     append([]PackageRecord(nil), input.Packages...),
		files:        append([]FileRecord(nil), input.Files...),
		symbols:      append([]SymbolRecord(nil), input.Symbols...),
		evidence:     append([]EvidenceRecord(nil), input.Evidence...),

		forwardOffsets: append([]uint32(nil), input.ForwardOffsets...),
		forwardEdges:   append([]PackedEdge(nil), input.ForwardEdges...),
		reverseOffsets: append([]uint32(nil), input.ReverseOffsets...),
		reverseEdges:   append([]PackedEdge(nil), input.ReverseEdges...),

		symbolByStableKey: cloneStableKeyIndex(input.SymbolByStableKey),
		symbolsByName:     cloneSymbolLists(input.SymbolsByName),
		symbolsByQName:    cloneSymbolLists(input.SymbolsByQName),
		fileByRepoPath:    cloneRepoPathIndex(input.FileByRepoPath),
	}
	if !snapshot.validExactIndexes() {
		return nil, ErrInvalidGraphSnapshot
	}
	return snapshot, nil
}

// Metadata returns the snapshot's identifier, timestamp, version, and counts.
func (snapshot *GraphSnapshot) Metadata() SnapshotMetadata { return snapshot.metadata }

// Strings returns the immutable interning table owned by the snapshot.
func (snapshot *GraphSnapshot) Strings() StringTable { return snapshot.strings }

// Symbol returns the record at one dense ID.
func (snapshot *GraphSnapshot) Symbol(id SymbolID) (SymbolRecord, bool) {
	if uint64(id) >= uint64(len(snapshot.symbols)) {
		return SymbolRecord{}, false
	}
	return snapshot.symbols[id], true
}

// SymbolByStableKey returns the symbol matching key through the exact index.
func (snapshot *GraphSnapshot) SymbolByStableKey(key StableKey) (SymbolID, bool) {
	id, found := snapshot.symbolByStableKey[key]
	return id, found
}

// SymbolsByName returns a copy of the exact-name result IDs.
func (snapshot *GraphSnapshot) SymbolsByName(name InternedString) []SymbolID {
	return append([]SymbolID(nil), snapshot.symbolsByName[name]...)
}

// SymbolsByQName returns a copy of the exact-qualified-name result IDs.
func (snapshot *GraphSnapshot) SymbolsByQName(name InternedString) []SymbolID {
	return append([]SymbolID(nil), snapshot.symbolsByQName[name]...)
}

// FileByRepoPath returns the file matching an exact repository/path pair.
func (snapshot *GraphSnapshot) FileByRepoPath(key RepoPathKey) (FileID, bool) {
	id, found := snapshot.fileByRepoPath[key]
	return id, found
}

func (snapshot *GraphSnapshot) validExactIndexes() bool {
	if len(snapshot.symbolByStableKey) != len(snapshot.symbols) || len(snapshot.fileByRepoPath) != len(snapshot.files) {
		return false
	}
	for id, symbol := range snapshot.symbols {
		if found, exists := snapshot.symbolByStableKey[symbol.StableKey]; !exists || found != SymbolID(id) {
			return false
		}
	}
	for id, file := range snapshot.files {
		key := RepoPathKey{Repository: file.Repository, Path: file.Path}
		if found, exists := snapshot.fileByRepoPath[key]; !exists || found != FileID(id) {
			return false
		}
	}
	return validSymbolLists(snapshot.symbolsByName, snapshot.symbols, func(symbol SymbolRecord) InternedString { return symbol.Name }) &&
		validSymbolLists(snapshot.symbolsByQName, snapshot.symbols, func(symbol SymbolRecord) InternedString { return symbol.QualifiedName })
}

func validSymbolLists(index map[InternedString][]SymbolID, symbols []SymbolRecord, keyFor func(SymbolRecord) InternedString) bool {
	seen := make([]bool, len(symbols))
	for key, ids := range index {
		for _, id := range ids {
			if uint64(id) >= uint64(len(symbols)) || seen[id] || keyFor(symbols[id]) != key {
				return false
			}
			seen[id] = true
		}
	}
	for _, found := range seen {
		if !found {
			return false
		}
	}
	return true
}

func fitsDenseTable[T any](records []T) bool { return uint64(len(records)) < math.MaxUint32 }

func cloneStableKeyIndex(source map[StableKey]SymbolID) map[StableKey]SymbolID {
	cloned := make(map[StableKey]SymbolID, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

func cloneSymbolLists(source map[InternedString][]SymbolID) map[InternedString][]SymbolID {
	cloned := make(map[InternedString][]SymbolID, len(source))
	for key, values := range source {
		cloned[key] = append([]SymbolID(nil), values...)
	}
	return cloned
}

func cloneRepoPathIndex(source map[RepoPathKey]FileID) map[RepoPathKey]FileID {
	cloned := make(map[RepoPathKey]FileID, len(source))
	for key, value := range source {
		cloned[key] = value
	}
	return cloned
}

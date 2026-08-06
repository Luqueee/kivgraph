package hotsnapshot

import (
	"errors"
	"math"
	"time"
)

var ErrInvalidGraphSnapshot = errors.New("invalid graph snapshot")

// RepositoryRecord stores the durable identity and display metadata for one
// indexed repository.
type RepositoryRecord struct {
	Key       InternedString
	Name      InternedString
	Path      InternedString
	Languages InternedString
	Commit    InternedString
}

// PackageRecord belongs to one repository.
type PackageRecord struct {
	Key        InternedString
	Repository RepositoryID
	Name       InternedString
	ModulePath InternedString
}

// FileRecord belongs to one package.
type FileRecord struct {
	Key        InternedString
	Repository RepositoryID
	Package    PackageID
	Path       InternedString
	Language   InternedString
}

// SymbolRecord stores source-independent and display identities for a symbol.
type SymbolRecord struct {
	StableKey          StableKey
	CanonicalIdentity  InternedString
	File               FileID
	Language           InternedString
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
	forwardOffsets := input.ForwardOffsets
	if forwardOffsets == nil {
		forwardOffsets = []uint32{0}
	}
	reverseOffsets := input.ReverseOffsets
	if reverseOffsets == nil {
		reverseOffsets = []uint32{0}
	}
	if input.Version == 0 || input.CreatedAt.IsZero() ||
		!fitsDenseTable(input.Repositories) || !fitsDenseTable(input.Packages) ||
		!fitsDenseTable(input.Files) || !fitsDenseTable(input.Symbols) ||
		!fitsDenseTable(input.Evidence) ||
		len(input.ForwardEdges) != len(input.ReverseEdges) ||
		!validCSR(len(input.Symbols), forwardOffsets, input.ForwardEdges) ||
		!validCSR(len(input.Symbols), reverseOffsets, input.ReverseEdges) ||
		!validReverseCounterpart(len(input.Symbols), forwardOffsets, input.ForwardEdges, reverseOffsets, input.ReverseEdges) ||
		!validEvidenceIDs(input.ForwardEdges, len(input.Evidence)) ||
		!validEvidenceIDs(input.ReverseEdges, len(input.Evidence)) {
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

		forwardOffsets:    append([]uint32(nil), forwardOffsets...),
		forwardEdges:      append([]PackedEdge(nil), input.ForwardEdges...),
		reverseOffsets:    append([]uint32(nil), reverseOffsets...),
		reverseEdges:      append([]PackedEdge(nil), input.ReverseEdges...),
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

// Repository returns one repository by its dense immutable ID.
func (snapshot *GraphSnapshot) Repository(id RepositoryID) (RepositoryRecord, bool) {
	if uint64(id) >= uint64(len(snapshot.repositories)) {
		return RepositoryRecord{}, false
	}
	return snapshot.repositories[id], true
}

// Package returns one package by its dense immutable ID.
func (snapshot *GraphSnapshot) Package(id PackageID) (PackageRecord, bool) {
	if uint64(id) >= uint64(len(snapshot.packages)) {
		return PackageRecord{}, false
	}
	return snapshot.packages[id], true
}

// File returns one file by its dense immutable ID.
func (snapshot *GraphSnapshot) File(id FileID) (FileRecord, bool) {
	if uint64(id) >= uint64(len(snapshot.files)) {
		return FileRecord{}, false
	}
	return snapshot.files[id], true
}

// Evidence returns one evidence record by its dense immutable ID.
func (snapshot *GraphSnapshot) Evidence(id EvidenceID) (EvidenceRecord, bool) {
	if uint64(id) >= uint64(len(snapshot.evidence)) {
		return EvidenceRecord{}, false
	}
	return snapshot.evidence[id], true
}

// Symbol returns the record at one dense ID.
func (snapshot *GraphSnapshot) Symbol(id SymbolID) (SymbolRecord, bool) {
	if uint64(id) >= uint64(len(snapshot.symbols)) {
		return SymbolRecord{}, false
	}
	return snapshot.symbols[id], true
}

// Outgoing returns a copy of the source's contiguous forward-CSR edge range.
func (snapshot *GraphSnapshot) Outgoing(source SymbolID) []PackedEdge {
	if uint64(source) >= uint64(len(snapshot.symbols)) {
		return nil
	}
	start := snapshot.forwardOffsets[source]
	end := snapshot.forwardOffsets[source+1]
	return append([]PackedEdge(nil), snapshot.forwardEdges[start:end]...)
}

// Incoming returns a copy of the target's contiguous reverse-CSR edge range.
func (snapshot *GraphSnapshot) Incoming(target SymbolID) []PackedEdge {
	if uint64(target) >= uint64(len(snapshot.symbols)) {
		return nil
	}
	start := snapshot.reverseOffsets[target]
	end := snapshot.reverseOffsets[target+1]
	return append([]PackedEdge(nil), snapshot.reverseEdges[start:end]...)
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

func validEvidenceIDs(edges []PackedEdge, evidence int) bool {
	for _, edge := range edges {
		if uint64(edge.Evidence) >= uint64(evidence) {
			return false
		}
	}
	return true
}

type csrEdgeKey struct {
	source, target                      SymbolID
	evidence                            EvidenceID
	kind, confidence, provenance, flags uint8
}

func validReverseCounterpart(symbols int, forwardOffsets []uint32, forwardEdges []PackedEdge, reverseOffsets []uint32, reverseEdges []PackedEdge) bool {
	counts := make(map[csrEdgeKey]int, len(forwardEdges))
	for source := 0; source < symbols; source++ {
		for _, edge := range forwardEdges[forwardOffsets[source]:forwardOffsets[source+1]] {
			counts[csrEdgeKey{
				source: SymbolID(source), target: edge.Target, evidence: edge.Evidence,
				kind: edge.Kind, confidence: edge.Confidence, provenance: edge.Provenance, flags: edge.Flags,
			}]++
		}
	}
	for target := 0; target < symbols; target++ {
		for _, edge := range reverseEdges[reverseOffsets[target]:reverseOffsets[target+1]] {
			key := csrEdgeKey{
				source: edge.Target, target: SymbolID(target), evidence: edge.Evidence,
				kind: edge.Kind, confidence: edge.Confidence, provenance: edge.Provenance, flags: edge.Flags,
			}
			if counts[key] == 0 {
				return false
			}
			counts[key]--
		}
	}
	for _, count := range counts {
		if count != 0 {
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

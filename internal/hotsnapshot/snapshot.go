package hotsnapshot

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"
)

var ErrInvalidGraphSnapshot = errors.New("invalid graph snapshot")

// RepositoryRecord stores the durable identity and display metadata for one
// indexed repository. Commit, Branch and Dirty describe the working tree the
// graph was built from: a query cannot tell whether its answers still hold
// without them, and a checkout moves all three.
type RepositoryRecord struct {
	Key       InternedString
	Name      InternedString
	Path      InternedString
	Languages InternedString
	Commit    InternedString
	Branch    InternedString
	Dirty     bool
}

// PackageRecord belongs to one repository.
type PackageRecord struct {
	Key        InternedString
	Repository RepositoryID
	Language   InternedString
	Name       InternedString
	ModulePath InternedString
}

// FileRecord belongs to one package.
//
// ContentDigest is the SHA-256 of the bytes that were analysed, as raw bytes
// rather than the hex the canonical row stores: thirty-two bytes per file
// instead of sixty-four characters in the string arena. A zero digest means the
// generation recorded none.
//
// It is here so a reader can tell whether a row still describes the file on
// disk. Serving a line range from a file that changed is the one failure an
// agent cannot notice on its own; see ADR 0040.
type FileRecord struct {
	Key           InternedString
	Repository    RepositoryID
	Package       PackageID
	Path          InternedString
	Language      InternedString
	ContentDigest [sha256.Size]byte
}

// SymbolRecord stores source-independent and display identities for a symbol.
// Exported separates public API from a private helper: breaking the first can
// reach another repository, breaking the second cannot leave its package.
//
// Every field is fixed size and none is a pointer, which is what makes this
// table mappable: a reader can address record i without following anything. The
// stable key is a dense ID into the snapshot's key table -- resolve it with
// GraphSnapshot.StableKey -- and it is numerically the record's own SymbolID,
// because symbols are sorted by key before any ID is allocated. It is stored
// rather than derived so a helper holding only a record can still name it.
type SymbolRecord struct {
	StableKey          StableKeyID
	CanonicalIdentity  InternedString
	File               FileID
	Language           InternedString
	Name               InternedString
	QualifiedName      InternedString
	Kind               InternedString
	Signature          InternedString
	Exported           bool
	StartLine, EndLine uint32
}

// EvidenceRecord describes the source that supports an edge.
type EvidenceRecord struct {
	Key        InternedString
	SourceFile FileID
	TargetFile FileID
	Kind       InternedString
	Provenance InternedString
}

// PackageDependencyRecord preserves a Package to Package relation that is
// outside the symbol CSR.
type PackageDependencyRecord struct {
	Source     PackageID
	Target     PackageID
	Kind       uint8
	Confidence uint8
	Provenance uint8
	Evidence   InternedString
}

// UnresolvedReferenceRecord preserves a reference that has no exact symbol
// target. File and Source are optional for module-level failures.
type UnresolvedReferenceRecord struct {
	Key              InternedString
	Repository       RepositoryID
	File             FileID
	Source           SymbolID
	Language         InternedString
	RequestedPackage InternedString
	RequestedSymbol  InternedString
	Reason           InternedString
	Detail           InternedString
	StartLine        uint32
	StartColumn      uint32
	StartOffset      uint32
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

	// SchemaVersion and ResolverVersion are provenance of the definitive
	// graph this snapshot was derived from, not of the snapshot format:
	// Version above covers that. They travel with the snapshot so a status
	// query can state them without reopening LadybugDB.
	SchemaVersion   int
	ResolverVersion string

	Strings      StringTable
	Repositories []RepositoryRecord
	Packages     []PackageRecord
	Files        []FileRecord
	Symbols      []SymbolRecord
	Evidence     []EvidenceRecord

	PackageDependencies []PackageDependencyRecord
	Unresolved          []UnresolvedReferenceRecord

	ForwardOffsets []uint32
	ForwardEdges   []PackedEdge
	ReverseOffsets []uint32
	ReverseEdges   []PackedEdge

	// StableKeys is the key table, and it replaces the map this input used to
	// carry. Entry i is the key of SymbolID i, so the index that used to cost
	// one hash entry per symbol is now the ordering of the table itself.
	//
	// The three lookup maps that used to sit beside it are gone for a stronger
	// reason than cost: every one of them was derivable from the tables above,
	// so a caller could only ever supply the right one or a wrong one. The
	// constructor derives them now, and a fixture can no longer disagree with
	// its own records.
	StableKeys StableKeyTable
}

// SnapshotMetadata contains versioned, immutable snapshot metadata.
type SnapshotMetadata struct {
	ID              uint64
	CreatedAt       time.Time
	Version         uint32
	SchemaVersion   int
	ResolverVersion string
	Counts          IDCounts
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

	packageDependencies []PackageDependencyRecord
	packageIncoming     packageIncomingIndex
	unresolved          []UnresolvedReferenceRecord

	forwardOffsets []uint32
	forwardEdges   []PackedEdge
	reverseOffsets []uint32
	reverseEdges   []PackedEdge

	stableKeys     StableKeyTable
	symbolsByName  symbolIndex
	symbolsByQName symbolIndex
	fileByRepoPath fileIndex

	// traversalWorkspacePool owns reusable per-call scratch buffers. It does not
	// participate in the immutable graph state returned to readers.
	traversalWorkspacePool sync.Pool
}

// NewGraphSnapshot validates the supplied graph envelope and copies all mutable
// inputs. The caller can therefore reuse or mutate GraphSnapshotInput after the
// call without changing the returned snapshot.
func NewGraphSnapshot(input GraphSnapshotInput) (*GraphSnapshot, error) {
	return newGraphSnapshot(input, false)
}

// newGraphSnapshot builds the snapshot, copying every mutable input unless the
// caller hands ownership over.
//
// The copy is the public contract and the builder needs it: it accumulates into
// slices it keeps using. A reader of a snapshot file does not. Its decoders
// allocate one slice per section, fill it from the mapped bytes and pass it
// here, and nobody else can name those slices -- so copying them produced a
// verbatim twin and left the original as garbage. Measured over `kena` in
// `benchmarks/snapshot-heap`, the pairs were exact rather than close:
// `decodeSymbols` and the symbols line allocated the same bytes, and so did the
// evidence table and both edge arrays. Nineteen point eight megabytes of a
// sixty-two megabyte transient half, dirtied once per process.
func newGraphSnapshot(input GraphSnapshotInput, owned bool) (*GraphSnapshot, error) {
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
		!validPackageDependencies(len(input.Packages), input.PackageDependencies) ||
		!validUnresolvedReferences(len(input.Repositories), len(input.Files), len(input.Symbols), input.Unresolved) ||
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
		PackageEdges: uint64(len(input.PackageDependencies)),
		Unresolved:   uint64(len(input.Unresolved)),
	}
	// Built before the snapshot because it is the one derivation that can fail:
	// two files at one path inside a repository.
	fileByRepoPath, err := newFileIndex(input.Files)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidGraphSnapshot, err)
	}
	snapshot := &GraphSnapshot{
		metadata: SnapshotMetadata{
			ID: input.ID, CreatedAt: input.CreatedAt, Version: input.Version,
			SchemaVersion: input.SchemaVersion, ResolverVersion: input.ResolverVersion, Counts: counts,
		},
		strings: input.Strings,

		repositories: keep(input.Repositories, owned),
		packages:     keep(input.Packages, owned),
		files:        keep(input.Files, owned),
		symbols:      keep(input.Symbols, owned),
		evidence:     keep(input.Evidence, owned),

		packageDependencies: keep(input.PackageDependencies, owned),
		packageIncoming:     newPackageIncomingIndex(len(input.Packages), input.PackageDependencies),
		unresolved:          keep(input.Unresolved, owned),

		forwardOffsets: keep(forwardOffsets, owned),
		forwardEdges:   keep(input.ForwardEdges, owned),
		reverseOffsets: keep(reverseOffsets, owned),
		reverseEdges:   keep(input.ReverseEdges, owned),
		// The table already owns its arena, so there is nothing to clone: it
		// copied its bytes when it was built, which is what stops a snapshot
		// from pinning the buffer its keys were read through.
		stableKeys: input.StableKeys,
		// Derived from the tables above rather than handed in: see the note on
		// GraphSnapshotInput.StableKeys for why there is nothing else they
		// could be.
		symbolsByName:  newSymbolIndex(input.Symbols, symbolName),
		symbolsByQName: newSymbolIndex(input.Symbols, symbolQualifiedName),
		fileByRepoPath: fileByRepoPath,
	}
	if !snapshot.validExactIndexes() {
		return nil, ErrInvalidGraphSnapshot
	}
	return snapshot, nil
}

// keep returns the slice itself when the caller handed ownership over, and a
// copy when it did not. A nil slice stays nil either way, which is what the
// empty tables of a graph with no rows rely on.
func keep[T any](records []T, owned bool) []T {
	if owned {
		return records
	}
	return append([]T(nil), records...)
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

// PackageDependencies returns the package relations entering target. A target
// with no incoming relation, including one that is not a package ID at all,
// has no entry in the index and yields no rows.
func (snapshot *GraphSnapshot) PackageDependencies(target PackageID) []PackageDependencyRecord {
	return snapshot.packageIncoming.rows(target, snapshot.packageDependencies)
}

// AllPackageDependencies returns all package relations in deterministic input
// order.
func (snapshot *GraphSnapshot) AllPackageDependencies() []PackageDependencyRecord {
	return append([]PackageDependencyRecord(nil), snapshot.packageDependencies...)
}

// UnresolvedReferences returns unresolved references in deterministic input
// order.
func (snapshot *GraphSnapshot) UnresolvedReferences() []UnresolvedReferenceRecord {
	return append([]UnresolvedReferenceRecord(nil), snapshot.unresolved...)
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

// SymbolByStableKey returns the symbol matching key.
//
// The signature is the one callers already had; what changed underneath is that
// the answer comes from a binary search over the key table instead of a hash
// table holding one string per symbol.
func (snapshot *GraphSnapshot) SymbolByStableKey(key StableKey) (SymbolID, bool) {
	id, found := snapshot.stableKeys.Lookup(key)
	if !found {
		return 0, false
	}
	return SymbolID(id), true
}

// StableKey resolves the dense key a SymbolRecord carries.
func (snapshot *GraphSnapshot) StableKey(id StableKeyID) (StableKey, bool) {
	return snapshot.stableKeys.Key(id)
}

// StableKeys returns the immutable key table owned by the snapshot.
func (snapshot *GraphSnapshot) StableKeys() StableKeyTable { return snapshot.stableKeys }

// SymbolsByName returns a copy of the exact-name result IDs.
func (snapshot *GraphSnapshot) SymbolsByName(name InternedString) []SymbolID {
	return append([]SymbolID(nil), snapshot.symbolsByName.lookup(name)...)
}

// SymbolsByQName returns a copy of the exact-qualified-name result IDs.
func (snapshot *GraphSnapshot) SymbolsByQName(name InternedString) []SymbolID {
	return append([]SymbolID(nil), snapshot.symbolsByQName.lookup(name)...)
}

// FileByRepoPath returns the file matching an exact repository/path pair.
func (snapshot *GraphSnapshot) FileByRepoPath(key RepoPathKey) (FileID, bool) {
	return snapshot.fileByRepoPath.lookup(key)
}

func (snapshot *GraphSnapshot) validExactIndexes() bool {
	if uint64(snapshot.stableKeys.Entries()) != uint64(len(snapshot.symbols)) {
		return false
	}
	// Entry i must be the key of symbol i, and that is what the record's dense
	// key claims -- so the claim is what gets checked.
	//
	// What used to sit here as well was the round trip: read entry i and look it
	// up, expecting i back. It cannot fail. Both constructors of a
	// StableKeyTable refuse entries that are not in strict byte order --
	// NewStableKeyTable on the builder's path and StableKeyTableFromArena on the
	// reader's -- and a binary search over strictly ascending, therefore
	// distinct, entries returns the position of the one it was handed. So the
	// ordering is trustworthy because those two say so, not because this loop
	// asked; asking cost 117 thousand binary searches over mapped pages, `7 ms`
	// a load, and `7,2 MB` of copies before the read stopped going through Key.
	for id, symbol := range snapshot.symbols {
		if symbol.StableKey != StableKeyID(id) {
			return false
		}
	}
	if len(snapshot.packageIncoming.offsets) != len(snapshot.packages)+1 ||
		len(snapshot.packageIncoming.values) != len(snapshot.packageDependencies) {
		return false
	}
	return snapshot.fileByRepoPath.validShape(len(snapshot.files)) &&
		snapshot.symbolsByName.validShape(len(snapshot.symbols)) &&
		snapshot.symbolsByQName.validShape(len(snapshot.symbols))
}

// symbolName and symbolQualifiedName name the two keys a symbol is indexed
// under. They are functions rather than a flag because the index does not care
// which field it reads, and a flag would have to be interpreted somewhere.
func symbolName(symbol SymbolRecord) InternedString { return symbol.Name }

func symbolQualifiedName(symbol SymbolRecord) InternedString { return symbol.QualifiedName }

func validPackageDependencies(packages int, dependencies []PackageDependencyRecord) bool {
	for _, dependency := range dependencies {
		if uint64(dependency.Source) >= uint64(packages) || uint64(dependency.Target) >= uint64(packages) ||
			dependency.Kind == 0 || dependency.Confidence == 0 || dependency.Provenance == 0 {
			return false
		}
	}
	return true
}

func validUnresolvedReferences(repositories, files, symbols int, references []UnresolvedReferenceRecord) bool {
	for _, reference := range references {
		if uint64(reference.Repository) >= uint64(repositories) {
			return false
		}
		if reference.File != InvalidFileID && uint64(reference.File) >= uint64(files) {
			return false
		}
		if reference.Source != InvalidSymbolID && uint64(reference.Source) >= uint64(symbols) {
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

// validReverseCounterpart proves the reverse CSR is a permutation of the
// forward one: every reverse row names a forward edge, and no forward edge is
// named twice.
//
// The bitmap is what makes it cheap. Keying a map by every forward edge
// allocated `13,3 MB` on `kena` for a structure discarded in the same call,
// which `benchmarks/snapshot-heap` measured as a third of the load's garbage;
// one bit per forward edge is `42 kB` there. What it costs instead is a walk of
// the source's forward group, so the work is the sum of the squared
// out-degrees -- `54x` the edge count on that corpus, over a comparison of
// seven fields rather than a hash, and bounded by the widest group (`889`
// against a median of `1`).
//
// It relies on validCSR having run: both offsets are sane and every Target is
// a symbol, so no index here needs a bound of its own.
func validReverseCounterpart(symbols int, forwardOffsets []uint32, forwardEdges []PackedEdge, reverseOffsets []uint32, reverseEdges []PackedEdge) bool {
	claimed := make([]uint64, (len(forwardEdges)+63)/64)
	matched := 0
	for target := range symbols {
		for _, edge := range reverseEdges[reverseOffsets[target]:reverseOffsets[target+1]] {
			// A reverse row carries its source in Target: it is the edge read
			// from the other end.
			source := edge.Target
			found := false
			for index := forwardOffsets[source]; index < forwardOffsets[source+1]; index++ {
				if claimed[index/64]&(1<<(index%64)) != 0 {
					continue
				}
				candidate := forwardEdges[index]
				if candidate.Target != SymbolID(target) ||
					candidate.Evidence != edge.Evidence ||
					candidate.Kind != edge.Kind ||
					candidate.Confidence != edge.Confidence ||
					candidate.Provenance != edge.Provenance ||
					candidate.Flags != edge.Flags {
					continue
				}
				claimed[index/64] |= 1 << (index % 64)
				matched++
				found = true
				break
			}
			if !found {
				return false
			}
		}
	}
	// Every forward edge has to have been claimed. The caller already refuses
	// two CSRs of different lengths, so this cannot fail today; it is here
	// because it is the only thing that would catch a short reverse CSR if that
	// check moved, and it is the direction the loop above cannot see.
	return matched == len(forwardEdges)
}

func fitsDenseTable[T any](records []T) bool { return uint64(len(records)) < math.MaxUint32 }

// Package hotsnapshot contains immutable in-memory graph representations.
package hotsnapshot

import (
	"errors"
	"math"
)

// Dense IDs index tables that belong to one immutable GraphSnapshot. They are
// never serialized as external identity: callers persist and exchange stable
// keys instead. Each snapshot allocates its own zero-based IDs, so an ID may
// refer to a different record after a snapshot rebuild.
type (
	RepositoryID uint32
	PackageID    uint32
	FileID       uint32
	SymbolID     uint32
	EvidenceID   uint32
	EdgeID       uint64
)

const (
	// Invalid*ID reserve the largest representation value as a sentinel. Zero
	// is a valid dense ID and indexes the first record in its table.
	InvalidRepositoryID RepositoryID = math.MaxUint32
	InvalidPackageID    PackageID    = math.MaxUint32
	InvalidFileID       FileID       = math.MaxUint32
	InvalidSymbolID     SymbolID     = math.MaxUint32
	InvalidEvidenceID   EvidenceID   = math.MaxUint32
	InvalidEdgeID       EdgeID       = math.MaxUint64
)

var ErrIDOverflow = errors.New("dense identifier capacity exceeded")

// IDCounts describes the number of IDs allocated while building one snapshot.
type IDCounts struct {
	Repositories uint64
	Packages     uint64
	Files        uint64
	Symbols      uint64
	Evidence     uint64
	Edges        uint64
	PackageEdges uint64
	Unresolved   uint64
}

// IDAllocator assigns dense IDs while one snapshot is built. It is deliberately
// not concurrency-safe: builders own it privately and publish only the finished
// immutable snapshot. Discard it after build completion; a rebuild starts from
// zero and must not reuse its IDs as external identity.
type IDAllocator struct {
	counts IDCounts
}

func (allocator *IDAllocator) Repository() (RepositoryID, error) {
	value, err := nextUint32(allocator.counts.Repositories)
	if err != nil {
		return InvalidRepositoryID, err
	}
	allocator.counts.Repositories++
	return RepositoryID(value), nil
}

func (allocator *IDAllocator) Package() (PackageID, error) {
	value, err := nextUint32(allocator.counts.Packages)
	if err != nil {
		return InvalidPackageID, err
	}
	allocator.counts.Packages++
	return PackageID(value), nil
}

func (allocator *IDAllocator) File() (FileID, error) {
	value, err := nextUint32(allocator.counts.Files)
	if err != nil {
		return InvalidFileID, err
	}
	allocator.counts.Files++
	return FileID(value), nil
}

func (allocator *IDAllocator) Symbol() (SymbolID, error) {
	value, err := nextUint32(allocator.counts.Symbols)
	if err != nil {
		return InvalidSymbolID, err
	}
	allocator.counts.Symbols++
	return SymbolID(value), nil
}

func (allocator *IDAllocator) Evidence() (EvidenceID, error) {
	value, err := nextUint32(allocator.counts.Evidence)
	if err != nil {
		return InvalidEvidenceID, err
	}
	allocator.counts.Evidence++
	return EvidenceID(value), nil
}

func (allocator *IDAllocator) Edge() (EdgeID, error) {
	if allocator.counts.Edges >= math.MaxUint64 {
		return InvalidEdgeID, ErrIDOverflow
	}
	value := EdgeID(allocator.counts.Edges)
	allocator.counts.Edges++
	return value, nil
}

// Counts returns a value copy suitable for snapshot metadata.
func (allocator *IDAllocator) Counts() IDCounts { return allocator.counts }

func nextUint32(next uint64) (uint32, error) {
	if next >= math.MaxUint32 {
		return 0, ErrIDOverflow
	}
	return uint32(next), nil
}

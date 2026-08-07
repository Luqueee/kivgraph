package hotsnapshot

import (
	"context"
	"errors"
)

var (
	// ErrInvalidSnapshotRange means that a dense or CSR half-open range is not
	// contained by the immutable snapshot.
	ErrInvalidSnapshotRange = errors.New("snapshot iteration range is invalid")
	// ErrNilSnapshotVisitor means that an iteration callback was not supplied.
	ErrNilSnapshotVisitor = errors.New("snapshot iteration visitor is nil")
	// ErrInvalidCSRDirection means that a direction other than outgoing or
	// incoming was supplied to a CSR accessor.
	ErrInvalidCSRDirection = errors.New("snapshot CSR direction is invalid")
)

// RepositoryVisitor receives one repository ID and a value copy of its record.
type RepositoryVisitor func(RepositoryID, RepositoryRecord) error

// PackageVisitor receives one package ID and a value copy of its record.
type PackageVisitor func(PackageID, PackageRecord) error

// FileVisitor receives one file ID and a value copy of its record.
type FileVisitor func(FileID, FileRecord) error

// SymbolVisitor receives one symbol ID and a value copy of its record.
type SymbolVisitor func(SymbolID, SymbolRecord) error

// EvidenceVisitor receives one evidence ID and a value copy of its record.
type EvidenceVisitor func(EvidenceID, EvidenceRecord) error

// EdgeVisitor receives one dense edge ID and a value copy of its packed edge.
// The edge value is safe to retain; it does not alias snapshot storage.
type EdgeVisitor func(EdgeID, PackedEdge) error

// VisitRepositories visits the half-open dense repository range [start, end).
func (snapshot *GraphSnapshot) VisitRepositories(ctx context.Context, start, end RepositoryID, visitor RepositoryVisitor) error {
	if snapshot == nil {
		return ErrInvalidSnapshotRange
	}
	return visitDense(ctx, start, end, snapshot.repositories, visitor)
}

// VisitPackages visits the half-open dense package range [start, end).
func (snapshot *GraphSnapshot) VisitPackages(ctx context.Context, start, end PackageID, visitor PackageVisitor) error {
	if snapshot == nil {
		return ErrInvalidSnapshotRange
	}
	return visitDense(ctx, start, end, snapshot.packages, visitor)
}

// VisitFiles visits the half-open dense file range [start, end).
func (snapshot *GraphSnapshot) VisitFiles(ctx context.Context, start, end FileID, visitor FileVisitor) error {
	if snapshot == nil {
		return ErrInvalidSnapshotRange
	}
	return visitDense(ctx, start, end, snapshot.files, visitor)
}

// VisitSymbols visits the half-open dense symbol range [start, end) in stable
// snapshot order. It performs no per-symbol allocation and checks ctx before
// each callback. A nil context is treated as context.Background().
func (snapshot *GraphSnapshot) VisitSymbols(ctx context.Context, start, end SymbolID, visitor SymbolVisitor) error {
	if snapshot == nil {
		return ErrInvalidSnapshotRange
	}
	return visitDense(ctx, start, end, snapshot.symbols, visitor)
}

// VisitEvidence visits the half-open dense evidence range [start, end).
func (snapshot *GraphSnapshot) VisitEvidence(ctx context.Context, start, end EvidenceID, visitor EvidenceVisitor) error {
	if snapshot == nil {
		return ErrInvalidSnapshotRange
	}
	return visitDense(ctx, start, end, snapshot.evidence, visitor)
}

// visitDense is shared by typed dense-table accessors. It copies each record
// value into the callback and never exposes the backing slice.
func visitDense[ID denseSnapshotID, Record any](ctx context.Context, start, end ID, records []Record, visitor func(ID, Record) error) error {
	if visitor == nil {
		return ErrNilSnapshotVisitor
	}
	if !validSnapshotRange(len(records), uint64(start), uint64(end)) {
		return ErrInvalidSnapshotRange
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for id := start; id < end; id++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := visitor(id, records[id]); err != nil {
			return err
		}
	}
	return nil
}

type denseSnapshotID interface {
	RepositoryID | PackageID | FileID | SymbolID | EvidenceID
}

// CSRRange returns the half-open packed-edge range for one symbol in the
// selected CSR direction. The returned range aliases no memory; pass it to
// VisitEdges to consume values without copying the underlying edge slice.
func (snapshot *GraphSnapshot) CSRRange(direction TraversalDirection, symbol SymbolID) (start, end EdgeID, ok bool) {
	if snapshot == nil || uint64(symbol) >= uint64(len(snapshot.symbols)) {
		return 0, 0, false
	}
	var offsets []uint32
	switch direction {
	case TraversalOutgoing:
		offsets = snapshot.forwardOffsets
	case TraversalIncoming:
		offsets = snapshot.reverseOffsets
	default:
		return 0, 0, false
	}
	return EdgeID(offsets[symbol]), EdgeID(offsets[symbol+1]), true
}

// VisitEdges visits the half-open packed-edge range [start, end) in one CSR
// direction. It performs no per-edge allocation and checks ctx before each
// callback. The range may span multiple source symbols; CSRRange is provided
// for callers that need one symbol's adjacency range.
func (snapshot *GraphSnapshot) VisitEdges(ctx context.Context, direction TraversalDirection, start, end EdgeID, visitor EdgeVisitor) error {
	if visitor == nil {
		return ErrNilSnapshotVisitor
	}
	if snapshot == nil {
		return ErrInvalidSnapshotRange
	}
	var edges []PackedEdge
	switch direction {
	case TraversalOutgoing:
		edges = snapshot.forwardEdges
	case TraversalIncoming:
		edges = snapshot.reverseEdges
	default:
		return ErrInvalidCSRDirection
	}
	if !validSnapshotRange(len(edges), uint64(start), uint64(end)) {
		return ErrInvalidSnapshotRange
	}
	if ctx == nil {
		ctx = context.Background()
	}
	for id := start; id < end; id++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := visitor(id, edges[id]); err != nil {
			return err
		}
	}
	return nil
}

func validSnapshotRange(length int, start, end uint64) bool {
	return start <= end && end <= uint64(length)
}

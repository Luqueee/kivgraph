package hotsnapshot

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
)

// Every encoder here writes one record in the width its section declares, and
// every decoder reads exactly that. The pairs are deliberately adjacent: a
// field added to one and forgotten in the other is the defect this format can
// hide best, and reading the two together is what makes it visible.
//
// Nothing writes the memory a Go struct occupies. A struct layout is not a
// format: one reordered field would change what every existing file means
// without changing a version number.

func encodeRepositories(records []RepositoryRecord) []byte {
	out := make([]byte, 0, len(records)*repositoryElemSize)
	for _, record := range records {
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Key))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Name))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Path))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Languages))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Commit))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Branch))
		out = append(out, boolByte(record.Dirty))
	}
	return out
}

func decodeRepositories(data []byte, count uint64) []RepositoryRecord {
	records := make([]RepositoryRecord, count)
	for index := range records {
		row := data[uint64(index)*repositoryElemSize:]
		records[index] = RepositoryRecord{
			Key:       InternedString(binary.LittleEndian.Uint32(row[0:4])),
			Name:      InternedString(binary.LittleEndian.Uint32(row[4:8])),
			Path:      InternedString(binary.LittleEndian.Uint32(row[8:12])),
			Languages: InternedString(binary.LittleEndian.Uint32(row[12:16])),
			Commit:    InternedString(binary.LittleEndian.Uint32(row[16:20])),
			Branch:    InternedString(binary.LittleEndian.Uint32(row[20:24])),
			Dirty:     row[24] != 0,
		}
	}
	return records
}

func encodePackages(records []PackageRecord) []byte {
	out := make([]byte, 0, len(records)*packageElemSize)
	for _, record := range records {
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Key))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Repository))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Language))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Name))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.ModulePath))
	}
	return out
}

func decodePackages(data []byte, count uint64) []PackageRecord {
	records := make([]PackageRecord, count)
	for index := range records {
		row := data[uint64(index)*packageElemSize:]
		records[index] = PackageRecord{
			Key:        InternedString(binary.LittleEndian.Uint32(row[0:4])),
			Repository: RepositoryID(binary.LittleEndian.Uint32(row[4:8])),
			Language:   InternedString(binary.LittleEndian.Uint32(row[8:12])),
			Name:       InternedString(binary.LittleEndian.Uint32(row[12:16])),
			ModulePath: InternedString(binary.LittleEndian.Uint32(row[16:20])),
		}
	}
	return records
}

func encodeFiles(records []FileRecord) []byte {
	out := make([]byte, 0, len(records)*fileElemSize)
	for _, record := range records {
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Key))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Repository))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Package))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Path))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Language))
		out = append(out, record.ContentDigest[:]...)
	}
	return out
}

func decodeFiles(data []byte, count uint64) []FileRecord {
	records := make([]FileRecord, count)
	for index := range records {
		row := data[uint64(index)*fileElemSize:]
		record := FileRecord{
			Key:        InternedString(binary.LittleEndian.Uint32(row[0:4])),
			Repository: RepositoryID(binary.LittleEndian.Uint32(row[4:8])),
			Package:    PackageID(binary.LittleEndian.Uint32(row[8:12])),
			Path:       InternedString(binary.LittleEndian.Uint32(row[12:16])),
			Language:   InternedString(binary.LittleEndian.Uint32(row[16:20])),
		}
		copy(record.ContentDigest[:], row[20:20+sha256.Size])
		records[index] = record
	}
	return records
}

// encodeSymbols writes everything but the stable key, which is a string and
// travels in its own two sections. Phase 2 of ADR 0045 is where that field
// stops being a string; until then the split is what keeps this section fixed
// width, which is what lets a reader reject a truncated table.
func encodeSymbols(records []SymbolRecord) []byte {
	out := make([]byte, 0, len(records)*symbolElemSize)
	for _, record := range records {
		out = binary.LittleEndian.AppendUint32(out, uint32(record.CanonicalIdentity))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.File))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Language))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Name))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.QualifiedName))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Kind))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Signature))
		out = append(out, boolByte(record.Exported))
		out = binary.LittleEndian.AppendUint32(out, record.StartLine)
		out = binary.LittleEndian.AppendUint32(out, record.EndLine)
	}
	return out
}

func decodeSymbols(data []byte, count uint64) []SymbolRecord {
	records := make([]SymbolRecord, count)
	for index := range records {
		row := data[uint64(index)*symbolElemSize:]
		records[index] = SymbolRecord{
			CanonicalIdentity: InternedString(binary.LittleEndian.Uint32(row[0:4])),
			File:              FileID(binary.LittleEndian.Uint32(row[4:8])),
			Language:          InternedString(binary.LittleEndian.Uint32(row[8:12])),
			Name:              InternedString(binary.LittleEndian.Uint32(row[12:16])),
			QualifiedName:     InternedString(binary.LittleEndian.Uint32(row[16:20])),
			Kind:              InternedString(binary.LittleEndian.Uint32(row[20:24])),
			Signature:         InternedString(binary.LittleEndian.Uint32(row[24:28])),
			Exported:          row[28] != 0,
			StartLine:         binary.LittleEndian.Uint32(row[29:33]),
			EndLine:           binary.LittleEndian.Uint32(row[33:37]),
		}
	}
	return records
}

// encodeSymbolKeys writes the stable keys as one blob plus n+1 offsets, so a
// key's bytes are a range rather than a length-prefixed record: the extra
// trailing offset is what makes the last key's end explicit instead of implied
// by the blob's size.
func encodeSymbolKeys(records []SymbolRecord) ([]uint32, []byte) {
	offsets := make([]uint32, 0, len(records)+1)
	blob := make([]byte, 0, len(records)*32)
	for _, record := range records {
		offsets = append(offsets, uint32(len(blob)))
		blob = append(blob, record.StableKey...)
	}
	offsets = append(offsets, uint32(len(blob)))
	return offsets, blob
}

// restoreSymbolKeys puts the keys back on the records, rejecting offsets that
// do not describe the blob they index. A key read from the wrong range is a
// symbol answering to another symbol's identity, which no later validation can
// catch: the index built from it would agree with it.
func restoreSymbolKeys(records []SymbolRecord, offsets []uint32, blob []byte) error {
	if len(offsets) != len(records)+1 {
		return fmt.Errorf("%w: %d key offsets for %d symbols", ErrInvalidSnapshotFile, len(offsets), len(records))
	}
	for index := range records {
		start, end := offsets[index], offsets[index+1]
		if start > end || uint64(end) > uint64(len(blob)) {
			return fmt.Errorf("%w: symbol %d claims key bytes %d..%d of %d",
				ErrInvalidSnapshotFile, index, start, end, len(blob))
		}
		records[index].StableKey = StableKey(blob[start:end])
	}
	return nil
}

func encodeEvidence(records []EvidenceRecord) []byte {
	out := make([]byte, 0, len(records)*evidenceElemSize)
	for _, record := range records {
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Key))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.SourceFile))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.TargetFile))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Kind))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Provenance))
	}
	return out
}

func decodeEvidence(data []byte, count uint64) []EvidenceRecord {
	records := make([]EvidenceRecord, count)
	for index := range records {
		row := data[uint64(index)*evidenceElemSize:]
		records[index] = EvidenceRecord{
			Key:        InternedString(binary.LittleEndian.Uint32(row[0:4])),
			SourceFile: FileID(binary.LittleEndian.Uint32(row[4:8])),
			TargetFile: FileID(binary.LittleEndian.Uint32(row[8:12])),
			Kind:       InternedString(binary.LittleEndian.Uint32(row[12:16])),
			Provenance: InternedString(binary.LittleEndian.Uint32(row[16:20])),
		}
	}
	return records
}

func encodePackageDependencies(records []PackageDependencyRecord) []byte {
	out := make([]byte, 0, len(records)*packageDependencyElemSize)
	for _, record := range records {
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Source))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Target))
		out = append(out, record.Kind, record.Confidence, record.Provenance)
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Evidence))
	}
	return out
}

func decodePackageDependencies(data []byte, count uint64) []PackageDependencyRecord {
	records := make([]PackageDependencyRecord, count)
	for index := range records {
		row := data[uint64(index)*packageDependencyElemSize:]
		records[index] = PackageDependencyRecord{
			Source:     PackageID(binary.LittleEndian.Uint32(row[0:4])),
			Target:     PackageID(binary.LittleEndian.Uint32(row[4:8])),
			Kind:       row[8],
			Confidence: row[9],
			Provenance: row[10],
			Evidence:   InternedString(binary.LittleEndian.Uint32(row[11:15])),
		}
	}
	return records
}

func encodeUnresolved(records []UnresolvedReferenceRecord) []byte {
	out := make([]byte, 0, len(records)*unresolvedElemSize)
	for _, record := range records {
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Key))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Repository))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.File))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Source))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Language))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.RequestedPackage))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.RequestedSymbol))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Reason))
		out = binary.LittleEndian.AppendUint32(out, uint32(record.Detail))
		out = binary.LittleEndian.AppendUint32(out, record.StartLine)
		out = binary.LittleEndian.AppendUint32(out, record.StartColumn)
		out = binary.LittleEndian.AppendUint32(out, record.StartOffset)
	}
	return out
}

func decodeUnresolved(data []byte, count uint64) []UnresolvedReferenceRecord {
	records := make([]UnresolvedReferenceRecord, count)
	for index := range records {
		row := data[uint64(index)*unresolvedElemSize:]
		records[index] = UnresolvedReferenceRecord{
			Key:              InternedString(binary.LittleEndian.Uint32(row[0:4])),
			Repository:       RepositoryID(binary.LittleEndian.Uint32(row[4:8])),
			File:             FileID(binary.LittleEndian.Uint32(row[8:12])),
			Source:           SymbolID(binary.LittleEndian.Uint32(row[12:16])),
			Language:         InternedString(binary.LittleEndian.Uint32(row[16:20])),
			RequestedPackage: InternedString(binary.LittleEndian.Uint32(row[20:24])),
			RequestedSymbol:  InternedString(binary.LittleEndian.Uint32(row[24:28])),
			Reason:           InternedString(binary.LittleEndian.Uint32(row[28:32])),
			Detail:           InternedString(binary.LittleEndian.Uint32(row[32:36])),
			StartLine:        binary.LittleEndian.Uint32(row[36:40]),
			StartColumn:      binary.LittleEndian.Uint32(row[40:44]),
			StartOffset:      binary.LittleEndian.Uint32(row[44:48]),
		}
	}
	return records
}

func encodeEdges(edges []PackedEdge) []byte {
	out := make([]byte, 0, len(edges)*edgeElemSize)
	for _, edge := range edges {
		out = binary.LittleEndian.AppendUint32(out, uint32(edge.Target))
		out = binary.LittleEndian.AppendUint32(out, uint32(edge.Evidence))
		out = append(out, edge.Kind, edge.Confidence, edge.Provenance, edge.Flags)
	}
	return out
}

func decodeEdges(data []byte, count uint64) []PackedEdge {
	edges := make([]PackedEdge, count)
	for index := range edges {
		row := data[uint64(index)*edgeElemSize:]
		edges[index] = PackedEdge{
			Target:     SymbolID(binary.LittleEndian.Uint32(row[0:4])),
			Evidence:   EvidenceID(binary.LittleEndian.Uint32(row[4:8])),
			Kind:       row[8],
			Confidence: row[9],
			Provenance: row[10],
			Flags:      row[11],
		}
	}
	return edges
}

func encodeOffsets(offsets []uint32) []byte {
	out := make([]byte, 0, len(offsets)*offsetElemSize)
	for _, offset := range offsets {
		out = binary.LittleEndian.AppendUint32(out, offset)
	}
	return out
}

func decodeOffsets(data []byte, count uint64) []uint32 {
	offsets := make([]uint32, count)
	for index := range offsets {
		offsets[index] = binary.LittleEndian.Uint32(data[uint64(index)*offsetElemSize:])
	}
	return offsets
}

func boolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}

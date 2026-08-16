package hotsnapshot

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"
)

// ErrInvalidSnapshotFile reports that a published snapshot file cannot be
// trusted: a foreign magic, a format this build does not know, a section table
// that does not describe its own payload, or a payload whose digest does not
// match the header.
//
// It is never a reason to fail a server. A generation always carries the
// canonical graph the snapshot was derived from, so a reader that rejects the
// file falls back to deriving the snapshot again, and says so. See ADR 0045.
var ErrInvalidSnapshotFile = errors.New("invalid snapshot file")

// snapshotFileMagic identifies the format and carries its own generation byte,
// so a file from a future layout is rejected by the magic rather than by a
// version field a reader has to reach past the header to find.
var snapshotFileMagic = [8]byte{'K', 'V', 'S', 'N', 'A', 'P', 0x01, 0x00}

// SnapshotFileFormatVersion versions the byte layout below, and nothing else.
// It is distinct from the snapshot's own Version -- which describes the row
// shaping that produced it -- and from the canonical schema version. It changes
// when this file's layout changes in a way an older reader would misread.
const SnapshotFileFormatVersion uint32 = 1

// Section kinds. They are explicit numbers rather than an iota over a Go slice
// order, because the number is written to disk: reordering the constants must
// not silently reinterpret an existing file.
const (
	sectionStrings           uint32 = 1
	sectionRepositories      uint32 = 2
	sectionPackages          uint32 = 3
	sectionFiles             uint32 = 4
	sectionSymbols           uint32 = 5
	sectionSymbolKeyOffsets  uint32 = 6
	sectionSymbolKeyBytes    uint32 = 7
	sectionEvidence          uint32 = 8
	sectionPackageDependency uint32 = 9
	sectionUnresolved        uint32 = 10
	sectionForwardOffsets    uint32 = 11
	sectionForwardEdges      uint32 = 12
	sectionReverseOffsets    uint32 = 13
	sectionReverseEdges      uint32 = 14
	sectionResolverVersion   uint32 = 15
)

// Element widths. Every record is written field by field in a declared width,
// never as the memory a Go struct happens to occupy: a struct layout is not a
// format, and one added field would otherwise change the meaning of every file
// ever written without changing a version number.
const (
	repositoryElemSize        = 6*4 + 1
	packageElemSize           = 5 * 4
	fileElemSize              = 5*4 + sha256.Size
	symbolElemSize            = 7*4 + 1 + 2*4
	evidenceElemSize          = 5 * 4
	packageDependencyElemSize = 2*4 + 3 + 4
	unresolvedElemSize        = 12 * 4
	edgeElemSize              = 2*4 + 4
	offsetElemSize            = 4
	byteElemSize              = 1
)

const (
	snapshotFileHeaderSize = 8 + 4 + 4 + 8 + 8 + 4 + 4 + sha256.Size + sha256.Size
	sectionEntrySize       = 4 + 4 + 8 + 8 + 8
)

// section describes one payload run. count and elemSize are both written so a
// reader can reject a length that does not divide into whole records instead of
// decoding a truncated one as a short table.
type section struct {
	kind     uint32
	elemSize uint32
	count    uint64
	offset   uint64
	length   uint64
}

// WriteSnapshot writes one published snapshot in the format ReadSnapshot
// accepts, and returns the digest of the payload it wrote.
//
// contentDigest is the identity of the graph the snapshot was derived from --
// what a generation stores in snapshot.sha256 -- and travels in the header so a
// reader can tell that a file belongs to the generation it was found in. The
// payload digest that the header also carries answers a different question:
// whether the bytes are intact.
func WriteSnapshot(writer io.Writer, snapshot *GraphSnapshot, contentDigest [sha256.Size]byte) ([sha256.Size]byte, error) {
	if snapshot == nil {
		return [sha256.Size]byte{}, fmt.Errorf("%w: no snapshot", ErrInvalidSnapshotFile)
	}
	strings, err := snapshot.strings.MarshalBinary()
	if err != nil {
		return [sha256.Size]byte{}, fmt.Errorf("%w: marshal string table: %w", ErrInvalidSnapshotFile, err)
	}
	keyOffsets, keyBytes := encodeSymbolKeys(snapshot.symbols)

	payloads := []struct {
		kind     uint32
		elemSize uint32
		count    uint64
		bytes    []byte
	}{
		{sectionStrings, byteElemSize, uint64(len(strings)), strings},
		{sectionResolverVersion, byteElemSize, uint64(len(snapshot.metadata.ResolverVersion)), []byte(snapshot.metadata.ResolverVersion)},
		{sectionRepositories, repositoryElemSize, uint64(len(snapshot.repositories)), encodeRepositories(snapshot.repositories)},
		{sectionPackages, packageElemSize, uint64(len(snapshot.packages)), encodePackages(snapshot.packages)},
		{sectionFiles, fileElemSize, uint64(len(snapshot.files)), encodeFiles(snapshot.files)},
		{sectionSymbols, symbolElemSize, uint64(len(snapshot.symbols)), encodeSymbols(snapshot.symbols)},
		{sectionSymbolKeyOffsets, offsetElemSize, uint64(len(keyOffsets)), encodeOffsets(keyOffsets)},
		{sectionSymbolKeyBytes, byteElemSize, uint64(len(keyBytes)), keyBytes},
		{sectionEvidence, evidenceElemSize, uint64(len(snapshot.evidence)), encodeEvidence(snapshot.evidence)},
		{sectionPackageDependency, packageDependencyElemSize, uint64(len(snapshot.packageDependencies)), encodePackageDependencies(snapshot.packageDependencies)},
		{sectionUnresolved, unresolvedElemSize, uint64(len(snapshot.unresolved)), encodeUnresolved(snapshot.unresolved)},
		{sectionForwardOffsets, offsetElemSize, uint64(len(snapshot.forwardOffsets)), encodeOffsets(snapshot.forwardOffsets)},
		{sectionForwardEdges, edgeElemSize, uint64(len(snapshot.forwardEdges)), encodeEdges(snapshot.forwardEdges)},
		{sectionReverseOffsets, offsetElemSize, uint64(len(snapshot.reverseOffsets)), encodeOffsets(snapshot.reverseOffsets)},
		{sectionReverseEdges, edgeElemSize, uint64(len(snapshot.reverseEdges)), encodeEdges(snapshot.reverseEdges)},
	}

	sections := make([]section, 0, len(payloads))
	offset := uint64(0)
	for _, payload := range payloads {
		sections = append(sections, section{
			kind: payload.kind, elemSize: payload.elemSize, count: payload.count,
			offset: offset, length: uint64(len(payload.bytes)),
		})
		offset += uint64(len(payload.bytes))
	}

	digest := sha256.New()
	for _, payload := range payloads {
		digest.Write(payload.bytes)
	}
	var payloadDigest [sha256.Size]byte
	copy(payloadDigest[:], digest.Sum(nil))

	header := encodeHeader(snapshot.metadata, uint32(len(sections)), contentDigest, payloadDigest)
	if _, err := writer.Write(header); err != nil {
		return payloadDigest, fmt.Errorf("write snapshot header: %w", err)
	}
	table := make([]byte, 0, len(sections)*sectionEntrySize)
	for _, entry := range sections {
		table = binary.LittleEndian.AppendUint32(table, entry.kind)
		table = binary.LittleEndian.AppendUint32(table, entry.elemSize)
		table = binary.LittleEndian.AppendUint64(table, entry.count)
		table = binary.LittleEndian.AppendUint64(table, entry.offset)
		table = binary.LittleEndian.AppendUint64(table, entry.length)
	}
	if _, err := writer.Write(table); err != nil {
		return payloadDigest, fmt.Errorf("write snapshot section table: %w", err)
	}
	for _, payload := range payloads {
		if _, err := writer.Write(payload.bytes); err != nil {
			return payloadDigest, fmt.Errorf("write snapshot section %d: %w", payload.kind, err)
		}
	}
	return payloadDigest, nil
}

// ReadSnapshot restores a snapshot written by WriteSnapshot.
//
// It rebuilds the four lookup indexes from the tables rather than reading them,
// and then hands everything to NewGraphSnapshot, which validates that those
// indexes agree with the tables they claim to index. A reader therefore cannot
// publish a snapshot whose index disagrees with its data: the same check that
// guards a freshly built snapshot guards a loaded one.
//
// contentDigest, when not zero, is required to match the header. That is how a
// caller asserts that this file belongs to the generation it was found beside.
func ReadSnapshot(data []byte, contentDigest [sha256.Size]byte) (*GraphSnapshot, error) {
	header, sections, payload, err := parseSnapshotFile(data)
	if err != nil {
		return nil, err
	}
	if contentDigest != ([sha256.Size]byte{}) && header.contentDigest != contentDigest {
		return nil, fmt.Errorf("%w: content digest is %x, expected %x",
			ErrInvalidSnapshotFile, header.contentDigest[:8], contentDigest[:8])
	}

	input := GraphSnapshotInput{
		ID:            header.id,
		CreatedAt:     header.createdAt,
		Version:       header.version,
		SchemaVersion: header.schemaVersion,
	}
	var symbolKeyOffsets []uint32
	var symbolKeyBytes []byte
	for _, entry := range sections {
		bytes := payload[entry.offset : entry.offset+entry.length]
		switch entry.kind {
		case sectionStrings:
			table, err := UnmarshalStringTable(bytes)
			if err != nil {
				return nil, fmt.Errorf("%w: string table: %w", ErrInvalidSnapshotFile, err)
			}
			input.Strings = table
		case sectionResolverVersion:
			input.ResolverVersion = string(bytes)
		case sectionRepositories:
			input.Repositories = decodeRepositories(bytes, entry.count)
		case sectionPackages:
			input.Packages = decodePackages(bytes, entry.count)
		case sectionFiles:
			input.Files = decodeFiles(bytes, entry.count)
		case sectionSymbols:
			input.Symbols = decodeSymbols(bytes, entry.count)
		case sectionSymbolKeyOffsets:
			symbolKeyOffsets = decodeOffsets(bytes, entry.count)
		case sectionSymbolKeyBytes:
			symbolKeyBytes = bytes
		case sectionEvidence:
			input.Evidence = decodeEvidence(bytes, entry.count)
		case sectionPackageDependency:
			input.PackageDependencies = decodePackageDependencies(bytes, entry.count)
		case sectionUnresolved:
			input.Unresolved = decodeUnresolved(bytes, entry.count)
		case sectionForwardOffsets:
			input.ForwardOffsets = decodeOffsets(bytes, entry.count)
		case sectionForwardEdges:
			input.ForwardEdges = decodeEdges(bytes, entry.count)
		case sectionReverseOffsets:
			input.ReverseOffsets = decodeOffsets(bytes, entry.count)
		case sectionReverseEdges:
			input.ReverseEdges = decodeEdges(bytes, entry.count)
		default:
			// An unknown section is not a corrupt file: a newer writer may
			// carry something this reader does not need. Every section this
			// reader *does* need is checked for presence below, so ignoring
			// the rest cannot produce a partial graph.
			continue
		}
	}
	if err := restoreSymbolKeys(input.Symbols, symbolKeyOffsets, symbolKeyBytes); err != nil {
		return nil, err
	}
	if err := indexSnapshotInput(&input); err != nil {
		return nil, err
	}
	snapshot, err := NewGraphSnapshot(input)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSnapshotFile, err)
	}
	return snapshot, nil
}

// indexSnapshotInput derives the four lookup indexes from the tables.
//
// It is the only place a reader builds them, and it builds them from the
// records alone: a stable key or a repository/path pair that appears twice is
// rejected here rather than silently keeping whichever row came last, because a
// dense ID that two keys resolve to is a wrong answer a query cannot detect.
func indexSnapshotInput(input *GraphSnapshotInput) error {
	input.SymbolByStableKey = make(map[StableKey]SymbolID, len(input.Symbols))
	input.SymbolsByName = make(map[InternedString][]SymbolID)
	input.SymbolsByQName = make(map[InternedString][]SymbolID)
	input.FileByRepoPath = make(map[RepoPathKey]FileID, len(input.Files))
	for index, record := range input.Symbols {
		id := SymbolID(index)
		if _, duplicated := input.SymbolByStableKey[record.StableKey]; duplicated {
			return fmt.Errorf("%w: stable key %q appears twice", ErrInvalidSnapshotFile, record.StableKey)
		}
		input.SymbolByStableKey[record.StableKey] = id
		input.SymbolsByName[record.Name] = append(input.SymbolsByName[record.Name], id)
		input.SymbolsByQName[record.QualifiedName] = append(input.SymbolsByQName[record.QualifiedName], id)
	}
	for index, record := range input.Files {
		key := RepoPathKey{Repository: record.Repository, Path: record.Path}
		if _, duplicated := input.FileByRepoPath[key]; duplicated {
			return fmt.Errorf("%w: repository %d path %d appears twice",
				ErrInvalidSnapshotFile, record.Repository, record.Path)
		}
		input.FileByRepoPath[key] = FileID(index)
	}
	return nil
}

type snapshotFileHeader struct {
	id            uint64
	createdAt     time.Time
	version       uint32
	schemaVersion int
	contentDigest [sha256.Size]byte
	payloadDigest [sha256.Size]byte
}

func encodeHeader(metadata SnapshotMetadata, sectionCount uint32, contentDigest, payloadDigest [sha256.Size]byte) []byte {
	header := make([]byte, 0, snapshotFileHeaderSize)
	header = append(header, snapshotFileMagic[:]...)
	header = binary.LittleEndian.AppendUint32(header, SnapshotFileFormatVersion)
	header = binary.LittleEndian.AppendUint32(header, sectionCount)
	header = binary.LittleEndian.AppendUint64(header, metadata.ID)
	header = binary.LittleEndian.AppendUint64(header, uint64(metadata.CreatedAt.UnixNano()))
	header = binary.LittleEndian.AppendUint32(header, metadata.Version)
	header = binary.LittleEndian.AppendUint32(header, uint32(metadata.SchemaVersion))
	header = append(header, contentDigest[:]...)
	header = append(header, payloadDigest[:]...)
	return header
}

// parseSnapshotFile validates the envelope before anything reads a record: the
// magic, the format version, that the section table fits, that every section
// lies inside the payload, that every length divides into whole records, and
// that the payload digest is the one the header claims.
func parseSnapshotFile(data []byte) (snapshotFileHeader, []section, []byte, error) {
	var header snapshotFileHeader
	if len(data) < snapshotFileHeaderSize {
		return header, nil, nil, fmt.Errorf("%w: %d bytes cannot hold a header", ErrInvalidSnapshotFile, len(data))
	}
	if [8]byte(data[0:8]) != snapshotFileMagic {
		return header, nil, nil, fmt.Errorf("%w: foreign magic %x", ErrInvalidSnapshotFile, data[0:8])
	}
	if formatVersion := binary.LittleEndian.Uint32(data[8:12]); formatVersion != SnapshotFileFormatVersion {
		return header, nil, nil, fmt.Errorf("%w: format version %d, this build reads %d",
			ErrInvalidSnapshotFile, formatVersion, SnapshotFileFormatVersion)
	}
	sectionCount := binary.LittleEndian.Uint32(data[12:16])
	header.id = binary.LittleEndian.Uint64(data[16:24])
	header.createdAt = time.Unix(0, int64(binary.LittleEndian.Uint64(data[24:32]))).UTC()
	header.version = binary.LittleEndian.Uint32(data[32:36])
	header.schemaVersion = int(int32(binary.LittleEndian.Uint32(data[36:40])))
	copy(header.contentDigest[:], data[40:40+sha256.Size])
	copy(header.payloadDigest[:], data[40+sha256.Size:snapshotFileHeaderSize])

	tableSize := uint64(sectionCount) * sectionEntrySize
	if uint64(len(data)) < uint64(snapshotFileHeaderSize)+tableSize {
		return header, nil, nil, fmt.Errorf("%w: %d sections do not fit in %d bytes",
			ErrInvalidSnapshotFile, sectionCount, len(data))
	}
	payload := data[uint64(snapshotFileHeaderSize)+tableSize:]
	sections := make([]section, 0, sectionCount)
	for index := range sectionCount {
		entryStart := uint64(snapshotFileHeaderSize) + uint64(index)*sectionEntrySize
		entry := data[entryStart : entryStart+sectionEntrySize]
		parsed := section{
			kind:     binary.LittleEndian.Uint32(entry[0:4]),
			elemSize: binary.LittleEndian.Uint32(entry[4:8]),
			count:    binary.LittleEndian.Uint64(entry[8:16]),
			offset:   binary.LittleEndian.Uint64(entry[16:24]),
			length:   binary.LittleEndian.Uint64(entry[24:32]),
		}
		if parsed.elemSize == 0 {
			return header, nil, nil, fmt.Errorf("%w: section %d declares no element size", ErrInvalidSnapshotFile, parsed.kind)
		}
		if parsed.offset > uint64(len(payload)) || parsed.length > uint64(len(payload))-parsed.offset {
			return header, nil, nil, fmt.Errorf("%w: section %d spans %d..%d of %d payload bytes",
				ErrInvalidSnapshotFile, parsed.kind, parsed.offset, parsed.offset+parsed.length, len(payload))
		}
		if parsed.count*uint64(parsed.elemSize) != parsed.length {
			return header, nil, nil, fmt.Errorf("%w: section %d holds %d bytes, not %d records of %d",
				ErrInvalidSnapshotFile, parsed.kind, parsed.length, parsed.count, parsed.elemSize)
		}
		sections = append(sections, parsed)
	}

	digest := sha256.New()
	for _, entry := range sections {
		digest.Write(payload[entry.offset : entry.offset+entry.length])
	}
	var computed [sha256.Size]byte
	copy(computed[:], digest.Sum(nil))
	if computed != header.payloadDigest {
		return header, nil, nil, fmt.Errorf("%w: payload digest is %x, header says %x",
			ErrInvalidSnapshotFile, computed[:8], header.payloadDigest[:8])
	}
	return header, sections, payload, nil
}

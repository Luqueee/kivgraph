package hotsnapshot

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"slices"
	"sort"
	"strings"
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

// ErrSnapshotFileVersion narrows ErrInvalidSnapshotFile to the one cause that is
// not a defect: a file this build does not read because the layout moved.
//
// The distinction matters outside this package. A corrupt file of the current
// format means something is wrong with the store and is worth waking up for; a
// file written by an older build means somebody upgraded, and the next index
// replaces it. Reporting both as a failure makes every upgrade look like
// corruption, which is how a real one stops being noticed.
//
// It wraps ErrInvalidSnapshotFile, so every existing caller that asks the broad
// question keeps getting the same answer.
var ErrSnapshotFileVersion = fmt.Errorf("%w: unsupported format version", ErrInvalidSnapshotFile)

// snapshotFileMagic identifies the format and carries its own generation byte,
// so a file from a future layout is rejected by the magic rather than by a
// version field a reader has to reach past the header to find.
var snapshotFileMagic = [8]byte{'K', 'V', 'S', 'N', 'A', 'P', 0x01, 0x00}

// SnapshotFileFormatVersion versions the byte layout below, and nothing else.
// It is distinct from the snapshot's own Version -- which describes the row
// shaping that produced it -- and from the canonical schema version. It changes
// when this file's layout changes in a way an older reader would misread.
//
// Version 2 aligns every section, which an older reader would in fact read
// correctly: the padding is not named by any section and not digested, so its
// offsets and its digest still describe the same bytes. The bump is for the
// other direction. This reader enforces alignment, because a reader that only
// hopes for it cannot take a view over a section, and a version mismatch is a
// stable, self-healing refusal -- the caller derives the snapshot again -- while
// an unaligned section rejected as "misaligned" would blame the wrong thing.
const SnapshotFileFormatVersion uint32 = 2

// sectionAlignment is where every section starts, counted from the payload.
//
// The task that introduced this said "aligned to the size of its element", and
// that is not what this does, for a reason worth writing down: several elements
// here are 25, 37, 15 and 52 bytes wide, and aligning to a number that is not a
// power of two is not an alignment. What the decision protects is a view over a
// section -- []uint32 today, []uint64 conceivably -- which on some architectures
// is undefined at an arbitrary address. Eight covers every scalar this format
// stores, so one rule replaces a table that could disagree with itself. The
// payload itself starts 8-aligned in the file, because the header is 104 bytes
// and every section entry is 32.
const sectionAlignment = 8

// padTo reports the bytes needed to reach the next multiple of alignment.
func padTo(offset, alignment uint64) uint64 {
	if remainder := offset % alignment; remainder != 0 {
		return alignment - remainder
	}
	return 0
}

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
	// sectionStringOrder carries the lookup order of the string table, which is
	// derived and deterministic: the writer sorts once so that no reader has to.
	// It is optional by design -- a reader that does not find it sorts, and a
	// reader that does not know the section ignores it, which is what lets a
	// file written by a newer build stay readable by an older one.
	sectionStringOrder uint32 = 16
	// sectionStringArena and sectionStringOffsets carry the values as one block
	// of bytes with a boundary per id, which is the only shape a reader can map
	// instead of copying. They replace sectionStrings, whose length-prefixed
	// form cannot be read in place; a reader that finds neither says so and its
	// caller derives the snapshot, which is what an older build does with a file
	// written by this one.
	sectionStringArena   uint32 = 17
	sectionStringOffsets uint32 = 18
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

// sectionElemSize is the one place a kind's element width is declared. The
// writer stamps what it returns and the reader enforces it, so the two cannot
// drift.
//
// Enforcing it is not redundant with the length check. A section that claims one
// byte per record passes every other test -- its length divides by its own
// declared element size, and it lies inside the payload -- and then the decoder
// for that kind reads thirty-seven bytes per record out of a buffer that has
// one. That is a panic, not a refusal, and a panic is not "never served".
//
// An unknown kind has no width here and none is required: the reader ignores
// kinds it does not know, by design, so nothing decodes them.
func sectionElemSize(kind uint32) (uint32, bool) {
	switch kind {
	case sectionStringArena, sectionSymbolKeyBytes, sectionResolverVersion, sectionStrings:
		return byteElemSize, true
	case sectionStringOffsets, sectionStringOrder, sectionSymbolKeyOffsets,
		sectionForwardOffsets, sectionReverseOffsets:
		return offsetElemSize, true
	case sectionRepositories:
		return repositoryElemSize, true
	case sectionPackages:
		return packageElemSize, true
	case sectionFiles:
		return fileElemSize, true
	case sectionSymbols:
		return symbolElemSize, true
	case sectionEvidence:
		return evidenceElemSize, true
	case sectionPackageDependency:
		return packageDependencyElemSize, true
	case sectionUnresolved:
		return unresolvedElemSize, true
	case sectionForwardEdges, sectionReverseEdges:
		return edgeElemSize, true
	default:
		return 0, false
	}
}

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
	keyOffsets, keyBytes := encodeSymbolKeys(snapshot.stableKeys)

	payloads := []struct {
		kind  uint32
		count uint64
		bytes []byte
	}{
		{sectionStringArena, uint64(len(snapshot.strings.arena)), snapshot.strings.arena},
		{sectionStringOffsets, uint64(len(snapshot.strings.offsets)), encodeOffsets(snapshot.strings.offsets)},
		{sectionStringOrder, uint64(len(snapshot.strings.order)), encodeInterned(snapshot.strings.order)},
		{sectionResolverVersion, uint64(len(snapshot.metadata.ResolverVersion)), []byte(snapshot.metadata.ResolverVersion)},
		{sectionRepositories, uint64(len(snapshot.repositories)), encodeRepositories(snapshot.repositories)},
		{sectionPackages, uint64(len(snapshot.packages)), encodePackages(snapshot.packages)},
		{sectionFiles, uint64(len(snapshot.files)), encodeFiles(snapshot.files)},
		{sectionSymbols, uint64(len(snapshot.symbols)), encodeSymbols(snapshot.symbols)},
		{sectionSymbolKeyOffsets, uint64(len(keyOffsets)), encodeOffsets(keyOffsets)},
		{sectionSymbolKeyBytes, uint64(len(keyBytes)), keyBytes},
		{sectionEvidence, uint64(len(snapshot.evidence)), encodeEvidence(snapshot.evidence)},
		{sectionPackageDependency, uint64(len(snapshot.packageDependencies)), encodePackageDependencies(snapshot.packageDependencies)},
		{sectionUnresolved, uint64(len(snapshot.unresolved)), encodeUnresolved(snapshot.unresolved)},
		{sectionForwardOffsets, uint64(len(snapshot.forwardOffsets)), encodeOffsets(snapshot.forwardOffsets)},
		{sectionForwardEdges, uint64(len(snapshot.forwardEdges)), encodeEdges(snapshot.forwardEdges)},
		{sectionReverseOffsets, uint64(len(snapshot.reverseOffsets)), encodeOffsets(snapshot.reverseOffsets)},
		{sectionReverseEdges, uint64(len(snapshot.reverseEdges)), encodeEdges(snapshot.reverseEdges)},
	}

	sections := make([]section, 0, len(payloads))
	padded := make([][]byte, 0, len(payloads)*2)
	offset := uint64(0)
	for _, payload := range payloads {
		if pad := padTo(offset, sectionAlignment); pad > 0 {
			padded = append(padded, make([]byte, pad))
			offset += pad
		}
		width, known := sectionElemSize(payload.kind)
		if !known {
			return [sha256.Size]byte{}, fmt.Errorf("%w: no element width declared for section %d", ErrInvalidSnapshotFile, payload.kind)
		}
		sections = append(sections, section{
			kind: payload.kind, elemSize: width, count: payload.count,
			offset: offset, length: uint64(len(payload.bytes)),
		})
		padded = append(padded, payload.bytes)
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
	// The padding runs are written but never digested and never named by a
	// section, so a reader that ignores alignment still recomputes the same
	// payload digest over exactly the same bytes.
	for index, block := range padded {
		if _, err := writer.Write(block); err != nil {
			return payloadDigest, fmt.Errorf("write snapshot payload block %d: %w", index, err)
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
	return readSnapshot(data, contentDigest, false)
}

// MapSnapshot restores a snapshot that reads its string values in place, out of
// data, instead of copying them.
//
// The caller owns keeping data valid for as long as anything can reach the
// snapshot: on a mapped file that means not unmapping it, and the collector
// cannot help, because it does not see that memory. In exchange, the largest
// single part of a snapshot -- some fifty megabytes of string bytes on a real
// corpus -- is read rather than allocated, and two processes reading the same
// generation share those pages.
//
// Everything else is copied either way, so the arena is the only thing this
// contract is about. See ADR 0045.
func MapSnapshot(data []byte, contentDigest [sha256.Size]byte) (*GraphSnapshot, error) {
	return readSnapshot(data, contentDigest, true)
}

func readSnapshot(data []byte, contentDigest [sha256.Size]byte, borrowed bool) (*GraphSnapshot, error) {
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
	var stringValues []byte
	var stringArena []byte
	var stringOffsets []uint32
	var stringOrder []InternedString
	for _, entry := range sections {
		bytes := payload[entry.offset : entry.offset+entry.length]
		switch entry.kind {
		case sectionStrings:
			stringValues = bytes
		case sectionStringArena:
			stringArena = bytes
		case sectionStringOffsets:
			stringOffsets = decodeOffsets(bytes, entry.count)
		case sectionStringOrder:
			stringOrder = decodeInterned(bytes, entry.count)
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
	table, err := readStringTable(stringArena, stringOffsets, stringValues, stringOrder, borrowed)
	if err != nil {
		return nil, err
	}
	input.Strings = table
	keys, err := restoreSymbolKeys(len(input.Symbols), symbolKeyOffsets, symbolKeyBytes, borrowed)
	if err != nil {
		return nil, err
	}
	input.StableKeys = keys
	if err := indexSnapshotInput(&input); err != nil {
		return nil, err
	}
	snapshot, err := NewGraphSnapshot(input)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidSnapshotFile, err)
	}
	return snapshot, nil
}

// indexSnapshotInput derives the three remaining lookup indexes from the tables.
//
// It is the only place a reader builds them, and it builds them from the
// records alone: a repository/path pair that appears twice is rejected here
// rather than silently keeping whichever row came last, because a dense ID that
// two keys resolve to is a wrong answer a query cannot detect.
//
// Stable keys are no longer among them. A duplicate key used to be caught here;
// it is now caught by the key table itself, which refuses entries that are not
// in strict byte order -- and two equal keys are not.
func indexSnapshotInput(input *GraphSnapshotInput) error {
	input.SymbolsByName = make(map[InternedString][]SymbolID)
	input.SymbolsByQName = make(map[InternedString][]SymbolID)
	input.FileByRepoPath = make(map[RepoPathKey]FileID, len(input.Files))
	for index, record := range input.Symbols {
		id := SymbolID(index)
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
			ErrSnapshotFileVersion, formatVersion, SnapshotFileFormatVersion)
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
	seen := make(map[uint32]struct{}, sectionCount)
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
		// A repeated kind is not a harmless duplicate. The decoder below is a
		// switch, so the last entry would silently win and the file would have
		// two answers for one table.
		if _, duplicated := seen[parsed.kind]; duplicated {
			return header, nil, nil, fmt.Errorf("%w: section %d appears twice", ErrInvalidSnapshotFile, parsed.kind)
		}
		seen[parsed.kind] = struct{}{}
		if expected, known := sectionElemSize(parsed.kind); known && parsed.elemSize != expected {
			return header, nil, nil, fmt.Errorf("%w: section %d declares %d bytes per record, this build writes %d",
				ErrInvalidSnapshotFile, parsed.kind, parsed.elemSize, expected)
		}
		if parsed.offset%sectionAlignment != 0 {
			return header, nil, nil, fmt.Errorf("%w: section %d starts at %d, which is not a multiple of %d",
				ErrInvalidSnapshotFile, parsed.kind, parsed.offset, sectionAlignment)
		}
		if parsed.offset > uint64(len(payload)) || parsed.length > uint64(len(payload))-parsed.offset {
			return header, nil, nil, fmt.Errorf("%w: section %d spans %d..%d of %d payload bytes",
				ErrInvalidSnapshotFile, parsed.kind, parsed.offset, parsed.offset+parsed.length, len(payload))
		}
		// Checked as a division rather than a multiplication, because the
		// multiplication wraps: a count of 2^61 with eight bytes per record
		// produces zero, which matches a zero length and lets a file through
		// whose decoder would then loop 2^61 times over an empty slice.
		if parsed.length%uint64(parsed.elemSize) != 0 || parsed.length/uint64(parsed.elemSize) != parsed.count {
			return header, nil, nil, fmt.Errorf("%w: section %d holds %d bytes, not %d records of %d",
				ErrInvalidSnapshotFile, parsed.kind, parsed.length, parsed.count, parsed.elemSize)
		}
		sections = append(sections, parsed)
	}
	// Overlap is checked across the whole table rather than per entry, because it
	// is a relation between two of them: two sections sharing bytes would both
	// decode, both validate, and disagree about what those bytes mean.
	ordered := append([]section(nil), sections...)
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].offset < ordered[right].offset })
	for position := 1; position < len(ordered); position++ {
		previous := ordered[position-1]
		if previous.offset+previous.length > ordered[position].offset {
			return header, nil, nil, fmt.Errorf("%w: sections %d and %d overlap at %d",
				ErrInvalidSnapshotFile, previous.kind, ordered[position].kind, ordered[position].offset)
		}
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

// readStringTable rebuilds the table from its values and, when the file carried
// one, the lookup order it already computed.
//
// A file with no order section is not a defect: an older writer produced it, and
// sorting is exactly what that costs. What is a defect is no values at all,
// because every record in every other section names its strings by id.
func readStringTable(arena []byte, offsets []uint32, values []byte, order []InternedString, borrowed bool) (StringTable, error) {
	if offsets == nil {
		// A file written before the arena sections existed carries the values
		// length-prefixed, which cannot be read in place: it is parsed into an
		// arena this table owns. Supporting it costs these four lines and saves a
		// server that has just been upgraded from deriving the whole graph until
		// the next generation is published.
		if values == nil {
			return StringTable{}, fmt.Errorf("%w: no string table", ErrInvalidSnapshotFile)
		}
		parsed, err := parseStringValues(values)
		if err != nil {
			return StringTable{}, fmt.Errorf("%w: string table: %w", ErrInvalidSnapshotFile, err)
		}
		arena, offsets, borrowed = parsed.arena, parsed.offsets, false
	}
	if !borrowed {
		arena = append([]byte(nil), arena...)
	}
	if order == nil {
		// A file that carries the values but not their order is not a defect: an
		// older writer produced it, and sorting is exactly what that costs.
		order = sortedStringOrder(arena, offsets)
	}
	table, err := StringTableFromArena(arena, offsets, order, borrowed)
	if err != nil {
		return StringTable{}, fmt.Errorf("%w: string table: %w", ErrInvalidSnapshotFile, err)
	}
	return table, nil
}

func encodeInterned(ids []InternedString) []byte {
	out := make([]byte, 0, len(ids)*offsetElemSize)
	for _, id := range ids {
		out = binary.LittleEndian.AppendUint32(out, uint32(id))
	}
	return out
}

func decodeInterned(data []byte, count uint64) []InternedString {
	ids := make([]InternedString, count)
	for index := range ids {
		ids[index] = InternedString(binary.LittleEndian.Uint32(data[uint64(index)*offsetElemSize:]))
	}
	return ids
}

// sortedStringOrder builds the lookup order of a table whose file did not carry
// one. It is the same order Freeze computes: ids sorted by their value.
func sortedStringOrder(arena []byte, offsets []uint32) []InternedString {
	order := make([]InternedString, len(offsets)-1)
	for index := range order {
		order[index] = InternedString(index)
	}
	value := func(id InternedString) string {
		return string(arena[offsets[id]:offsets[id+1]])
	}
	slices.SortFunc(order, func(left, right InternedString) int {
		return strings.Compare(value(left), value(right))
	})
	return order
}

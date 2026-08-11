//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unsafe"
)

// scanCanonicalArrow reads the whole canonical graph in Arrow chunks.
//
// The tuple reader asks the engine for one value at a time: sixteen cgo calls
// for a Symbol row, and a graph has as many Symbol rows as the corpus has
// declarations. Arrow answers a chunk of rows per call and hands back the
// columns as buffers, so the per-row cost stops being a round trip and
// becomes a pointer read.
//
// Correctness is not argued from the code: scanCanonicalTuples stays in the
// package as the reference implementation, and a test asserts the two produce
// the identical CanonicalGraph over a fixture that exercises every column
// type, NULL, an empty table and a multi-chunk read.
func scanCanonicalArrow(ctx context.Context, path string) (CanonicalGraph, error) {
	if err := validatePath(path); err != nil {
		return CanonicalGraph{}, err
	}
	if err := ctx.Err(); err != nil {
		return CanonicalGraph{}, &Error{Op: "scan canonical", Err: err}
	}

	// Every collection starts empty rather than nil: a table with no rows
	// is an empty table, and the tuple reader answers the same way.
	graph := CanonicalGraph{
		Repositories: make([]CanonicalRepository, 0),
		Packages:     make([]CanonicalPackage, 0),
		Files:        make([]CanonicalFile, 0),
		Symbols:      make([]CanonicalSymbol, 0),
		Evidence:     make([]CanonicalEvidence, 0),
		Unresolved:   make([]CanonicalUnresolvedReference, 0),
		Edges:        make([]CanonicalEdge, 0),
	}
	err := canonicalArrowScan(ctx, path, func(query arrowQueryFunc) error {
		read := func(name, statement string, formats []string, decode func(*canonicalArrowChunk) error) error {
			err := query(statement, formats, func(columns []arrowColumn, rowCount int64) error {
				chunk, err := newCanonicalArrowChunk(columns, formats, rowCount)
				if err != nil {
					return err
				}
				return decode(chunk)
			})
			if err != nil {
				return fmt.Errorf("scan %s: %w", name, err)
			}
			return nil
		}

		// Metadata, and the schema version it carries, are read and
		// checked before anything else: a graph this code does not
		// understand must fail before a single Symbol or edge row is
		// paged in, not after.
		metadata := make(map[string]string)
		if err := read("GraphMetadata", `MATCH (n:GraphMetadata) RETURN n.key, n.value ORDER BY n.key`,
			[]string{arrowUTF8, arrowUTF8}, func(chunk *canonicalArrowChunk) error {
				return chunk.rows(func(row int64) error {
					metadata[chunk.text(row, 0)] = chunk.text(row, 1)
					return nil
				})
			}); err != nil {
			return err
		}
		schemaVersion, err := canonicalScanSchemaVersion(metadata)
		if err != nil {
			return err
		}
		graph.SchemaVersion = schemaVersion
		graph.Metadata = metadata

		for _, table := range canonicalArrowNodeTables(&graph) {
			if err := read(table.name, table.query, table.formats, table.decode); err != nil {
				return err
			}
		}

		// Every relationship table of the vocabulary is read in turn,
		// in CanonicalRelationshipTables order; the result is sorted
		// once afterwards so the final order never depends on that
		// per-table iteration order, only on what is stored.
		for _, table := range canonicalEdgeVocabularyTables() {
			statement, formats, err := canonicalArrowEdgeQuery(table)
			if err != nil {
				return fmt.Errorf("scan %s: %w", table.Name, err)
			}
			if err := read(table.Name, statement, formats, canonicalArrowEdgeDecoder(&graph, table)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		var scanErr *Error
		if errors.As(err, &scanErr) {
			return CanonicalGraph{}, err
		}
		return CanonicalGraph{}, &Error{Op: "scan canonical", Err: err}
	}
	sort.Slice(graph.Edges, func(i, j int) bool { return canonicalEdgeLess(graph.Edges[i], graph.Edges[j]) })
	return graph, nil
}

// arrowQueryFunc runs one query and hands each Arrow chunk to consume.
type arrowQueryFunc func(query string, formats []string, consume func([]arrowColumn, int64) error) error

// canonicalArrowTable is one node table's query, the Arrow layout it must
// come back in, and what to do with the rows.
type canonicalArrowTable struct {
	name    string
	query   string
	formats []string
	decode  func(*canonicalArrowChunk) error
}

// Arrow format strings for the three column types the canonical schema uses.
// scanArrowQuery compares them against what the engine actually reports, so a
// list that drifts from the schema fails the scan instead of misreading it.
const (
	arrowUTF8  = "u"
	arrowInt64 = "l"
	arrowBool  = "b"
)

func canonicalArrowNodeTables(graph *CanonicalGraph) []canonicalArrowTable {
	return []canonicalArrowTable{
		{
			name: "Repository",
			query: `MATCH (n:Repository)
RETURN n.stable_key, n.name, n.root_path, n.commit, n.branch, n.dirty, n.languages
ORDER BY n.stable_key`,
			formats: []string{arrowUTF8, arrowUTF8, arrowUTF8, arrowUTF8, arrowUTF8, arrowBool, arrowUTF8},
			decode: func(chunk *canonicalArrowChunk) error {
				return chunk.rows(func(row int64) error {
					graph.Repositories = append(graph.Repositories, CanonicalRepository{
						StableKey: chunk.text(row, 0), Name: chunk.text(row, 1),
						RootPath: chunk.text(row, 2), Commit: chunk.text(row, 3),
						Branch: chunk.text(row, 4), Dirty: chunk.flag(row, 5),
						Languages: chunk.text(row, 6),
					})
					return nil
				})
			},
		},
		{
			name: "Package",
			query: `MATCH (n:Package)
RETURN n.stable_key, n.repository_key, n.language, n.name, n.version, n.root_path, n.manifest_path, n.container
ORDER BY n.stable_key`,
			formats: repeatFormat(arrowUTF8, 8),
			decode: func(chunk *canonicalArrowChunk) error {
				return chunk.rows(func(row int64) error {
					graph.Packages = append(graph.Packages, CanonicalPackage{
						StableKey: chunk.text(row, 0), RepositoryKey: chunk.text(row, 1),
						Language: chunk.text(row, 2), Name: chunk.text(row, 3),
						Version: chunk.text(row, 4), RootPath: chunk.text(row, 5),
						ManifestPath: chunk.text(row, 6), Container: chunk.text(row, 7),
					})
					return nil
				})
			},
		},
		{
			name: "File",
			query: `MATCH (n:File)
RETURN n.stable_key, n.repository_key, n.package_key, n.path, n.language, n.content_hash, n.generated
ORDER BY n.stable_key`,
			formats: append(repeatFormat(arrowUTF8, 6), arrowBool),
			decode: func(chunk *canonicalArrowChunk) error {
				return chunk.rows(func(row int64) error {
					graph.Files = append(graph.Files, CanonicalFile{
						StableKey: chunk.text(row, 0), RepositoryKey: chunk.text(row, 1),
						PackageKey: chunk.text(row, 2), Path: chunk.text(row, 3),
						Language: chunk.text(row, 4), ContentHash: chunk.text(row, 5),
						Generated: chunk.flag(row, 6),
					})
					return nil
				})
			},
		},
		{
			name: "Symbol",
			query: `MATCH (n:Symbol)
RETURN n.stable_key, n.canonical_identity, n.repository_key, n.package_key, n.file_key,
       n.language, n.name, n.qualified_name, n.kind, n.exported, n.signature,
       n.start_line, n.start_column, n.start_offset, n.end_line, n.end_offset
ORDER BY n.stable_key`,
			formats: append(append(repeatFormat(arrowUTF8, 9), arrowBool, arrowUTF8), repeatFormat(arrowInt64, 5)...),
			decode: func(chunk *canonicalArrowChunk) error {
				return chunk.rows(func(row int64) error {
					graph.Symbols = append(graph.Symbols, CanonicalSymbol{
						StableKey: chunk.text(row, 0), CanonicalIdentity: chunk.text(row, 1),
						RepositoryKey: chunk.text(row, 2), PackageKey: chunk.text(row, 3),
						FileKey: chunk.text(row, 4), Language: chunk.text(row, 5),
						Name: chunk.text(row, 6), QualifiedName: chunk.text(row, 7),
						Kind: chunk.text(row, 8), Exported: chunk.flag(row, 9),
						Signature: chunk.text(row, 10), StartLine: chunk.number(row, 11),
						StartColumn: chunk.number(row, 12), StartOffset: chunk.number(row, 13),
						EndLine: chunk.number(row, 14), EndOffset: chunk.number(row, 15),
					})
					return nil
				})
			},
		},
		{
			name: "Evidence",
			query: `MATCH (n:Evidence)
RETURN n.stable_key, n.repository_key, n.file_key, n.start_line, n.start_column, n.start_offset, n.end_offset, n.text
ORDER BY n.stable_key`,
			formats: append(append(repeatFormat(arrowUTF8, 3), repeatFormat(arrowInt64, 4)...), arrowUTF8),
			decode: func(chunk *canonicalArrowChunk) error {
				return chunk.rows(func(row int64) error {
					graph.Evidence = append(graph.Evidence, CanonicalEvidence{
						StableKey: chunk.text(row, 0), RepositoryKey: chunk.text(row, 1),
						FileKey: chunk.text(row, 2), StartLine: chunk.number(row, 3),
						StartColumn: chunk.number(row, 4), StartOffset: chunk.number(row, 5),
						EndOffset: chunk.number(row, 6), Text: chunk.text(row, 7),
					})
					return nil
				})
			},
		},
		{
			name: "UnresolvedReference",
			query: `MATCH (n:UnresolvedReference)
RETURN n.stable_key, n.repository_key, n.file_key, n.source_symbol_key,
       n.language, n.requested_package, n.requested_symbol, n.reason, n.detail,
       n.start_line, n.start_column, n.start_offset
ORDER BY n.stable_key`,
			formats: append(repeatFormat(arrowUTF8, 9), repeatFormat(arrowInt64, 3)...),
			decode: func(chunk *canonicalArrowChunk) error {
				return chunk.rows(func(row int64) error {
					graph.Unresolved = append(graph.Unresolved, CanonicalUnresolvedReference{
						StableKey: chunk.text(row, 0), RepositoryKey: chunk.text(row, 1),
						FileKey: chunk.text(row, 2), SourceSymbolKey: chunk.text(row, 3),
						Language: chunk.text(row, 4), RequestedPackage: chunk.text(row, 5),
						RequestedSymbol: chunk.text(row, 6), Reason: chunk.text(row, 7),
						Detail: chunk.text(row, 8), StartLine: chunk.number(row, 9),
						StartColumn: chunk.number(row, 10), StartOffset: chunk.number(row, 11),
					})
					return nil
				})
			},
		},
	}
}

// canonicalArrowEdgeQuery renders one relationship table's query and the
// Arrow layout its columns must arrive in, both derived from
// table.Properties rather than a hand written list per edge kind -- the same
// rule scanCanonicalEdgeTable follows, for the same reason.
func canonicalArrowEdgeQuery(table SchemaRelationshipTable) (string, []string, error) {
	columns := make([]string, 0, 2+len(table.Properties))
	columns = append(columns, "source.stable_key", "target.stable_key")
	formats := []string{arrowUTF8, arrowUTF8}
	for _, property := range table.Properties {
		columns = append(columns, "edge."+property.Name)
		format, err := arrowFormatOf(property.Type)
		if err != nil {
			return "", nil, fmt.Errorf("relationship %s property %s: %w", table.Name, property.Name, err)
		}
		formats = append(formats, format)
	}
	query := fmt.Sprintf("MATCH (source)-[edge:%s]->(target) RETURN %s ORDER BY source.stable_key, target.stable_key",
		table.Name, strings.Join(columns, ", "))
	return query, formats, nil
}

// canonicalArrowEdgeDecoder fills one edge per row, reading each property by
// its declared name so a reordering of the schema's property lists can never
// silently swap two columns.
func canonicalArrowEdgeDecoder(graph *CanonicalGraph, table SchemaRelationshipTable) func(*canonicalArrowChunk) error {
	return func(chunk *canonicalArrowChunk) error {
		return chunk.rows(func(row int64) error {
			edge := CanonicalEdge{
				Table:     table.Name,
				SourceKey: chunk.text(row, 0),
				TargetKey: chunk.text(row, 1),
			}
			for index, property := range table.Properties {
				column := 2 + index
				switch property.Name {
				case "confidence":
					edge.Confidence = chunk.text(row, column)
				case "provenance":
					edge.Provenance = chunk.text(row, column)
				case "evidence_key":
					edge.EvidenceKey = chunk.text(row, column)
				case "source_snapshot":
					edge.SourceSnapshot = chunk.number(row, column)
				case "resolver_version":
					edge.ResolverVersion = chunk.text(row, column)
				default:
					return fmt.Errorf("relationship %s has unknown property %q", table.Name, property.Name)
				}
			}
			graph.Edges = append(graph.Edges, edge)
			return nil
		})
	}
}

func arrowFormatOf(columnType string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(columnType)) {
	case "STRING":
		return arrowUTF8, nil
	case "INT64":
		return arrowInt64, nil
	case "BOOL":
		return arrowBool, nil
	default:
		return "", fmt.Errorf("no Arrow format for column type %q", columnType)
	}
}

func repeatFormat(format string, count int) []string {
	formats := make([]string, count)
	for index := range formats {
		formats[index] = format
	}
	return formats
}

// canonicalArrowChunk is typed access to one chunk of rows.
//
// The string data of each column is copied once into a single arena and the
// row values point into it, so a chunk of a hundred thousand rows costs one
// allocation per string column instead of one per value.
type canonicalArrowChunk struct {
	rowCount int64
	kinds    []string
	strings  []arrowArenaColumn
	numbers  []arrowInt64Column
	flags    []arrowBoolColumn
	arena    []byte
	err      error
}

func newCanonicalArrowChunk(columns []arrowColumn, formats []string, rowCount int64) (*canonicalArrowChunk, error) {
	if len(columns) != len(formats) {
		return nil, fmt.Errorf("Arrow chunk has %d columns, want %d", len(columns), len(formats))
	}
	chunk := &canonicalArrowChunk{
		rowCount: rowCount,
		kinds:    formats,
		strings:  make([]arrowArenaColumn, len(columns)),
		numbers:  make([]arrowInt64Column, len(columns)),
		flags:    make([]arrowBoolColumn, len(columns)),
	}
	var total int64
	for index, format := range formats {
		switch format {
		case arrowUTF8:
			column, err := newArrowArenaColumn(columns[index])
			if err != nil {
				return nil, fmt.Errorf("column %d: %w", index, err)
			}
			if rowCount > 0 {
				start, end, err := column.dataRange(rowCount)
				if err != nil {
					return nil, fmt.Errorf("column %d: %w", index, err)
				}
				if end-start > int64(^uint(0)>>1)-total {
					return nil, fmt.Errorf("Arrow string data exceeds addressable memory")
				}
				column.sourceStart, column.sourceEnd = start, end
				column.destination = int(total)
				total += end - start
			}
			chunk.strings[index] = column
		case arrowInt64:
			column, err := newArrowInt64Column(columns[index])
			if err != nil {
				return nil, fmt.Errorf("column %d: %w", index, err)
			}
			chunk.numbers[index] = column
		case arrowBool:
			column, err := newArrowBoolColumn(columns[index])
			if err != nil {
				return nil, fmt.Errorf("column %d: %w", index, err)
			}
			chunk.flags[index] = column
		default:
			return nil, fmt.Errorf("column %d has unsupported Arrow format %q", index, format)
		}
	}
	chunk.arena = make([]byte, int(total))
	for index, format := range formats {
		if format != arrowUTF8 {
			continue
		}
		column := &chunk.strings[index]
		length := int(column.sourceEnd - column.sourceStart)
		if length == 0 {
			continue
		}
		copy(chunk.arena[column.destination:column.destination+length],
			unsafe.Slice((*byte)(unsafe.Add(column.data, column.sourceStart)), length))
	}
	return chunk, nil
}

// rows calls consume for every row of the chunk, and reports the first
// decoding failure a column accessor recorded. The accessors do not return
// errors so that a decoder reads like the row it builds; a malformed chunk
// still fails the scan rather than producing a value nobody can reproduce.
func (chunk *canonicalArrowChunk) rows(consume func(row int64) error) error {
	for row := int64(0); row < chunk.rowCount; row++ {
		if err := consume(row); err != nil {
			return err
		}
		if chunk.err != nil {
			return chunk.err
		}
	}
	return chunk.err
}

// text answers a STRING column.
//
// A NULL decodes to the empty string, which is what the value round trips
// from: LadybugDB's CSV loader stores an empty field as NULL, and
// CanonicalTableRows renders every STRING column unconditionally, empty or
// not. The tuple reader makes the same choice, in tupleOptionalString.
func (chunk *canonicalArrowChunk) text(row int64, column int) string {
	if chunk.kinds[column] != arrowUTF8 {
		chunk.fail(fmt.Errorf("column %d is %q, not a string", column, chunk.kinds[column]))
		return ""
	}
	entry := &chunk.strings[column]
	start, end, null, err := entry.offsetsOrNullAt(row)
	if err != nil {
		chunk.fail(fmt.Errorf("column %d: %w", column, err))
		return ""
	}
	if null || end == start {
		return ""
	}
	if start < entry.sourceStart || end > entry.sourceEnd {
		chunk.fail(fmt.Errorf("column %d offsets %d..%d outside chunk range %d..%d",
			column, start, end, entry.sourceStart, entry.sourceEnd))
		return ""
	}
	offset := entry.destination + int(start-entry.sourceStart)
	length := int(end - start)
	return unsafe.String(unsafe.SliceData(chunk.arena[offset:offset+length]), length)
}

func (chunk *canonicalArrowChunk) number(row int64, column int) int64 {
	if chunk.kinds[column] != arrowInt64 {
		chunk.fail(fmt.Errorf("column %d is %q, not an int64", column, chunk.kinds[column]))
		return 0
	}
	entry := &chunk.numbers[column]
	value, err := entry.valueAt(row)
	if err != nil {
		chunk.fail(fmt.Errorf("column %d: %w", column, err))
		return 0
	}
	return value
}

func (chunk *canonicalArrowChunk) flag(row int64, column int) bool {
	if chunk.kinds[column] != arrowBool {
		chunk.fail(fmt.Errorf("column %d is %q, not a bool", column, chunk.kinds[column]))
		return false
	}
	entry := &chunk.flags[column]
	value, err := entry.valueAt(row)
	if err != nil {
		chunk.fail(fmt.Errorf("column %d: %w", column, err))
		return false
	}
	return value
}

func (chunk *canonicalArrowChunk) fail(err error) {
	if chunk.err == nil {
		chunk.err = err
	}
}

func arrowBitSet(buffer unsafe.Pointer, index int64) bool {
	return *(*byte)(unsafe.Add(buffer, index/8))&(byte(1)<<uint(index%8)) != 0
}

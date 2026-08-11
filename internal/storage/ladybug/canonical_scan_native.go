//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	lbug "github.com/LadybugDB/go-ladybug"
)

// ScanCanonical reads the definitive graph out of the database at path.
//
// It reads in Arrow chunks (canonical_scan_arrow_native.go). This file used
// to argue the opposite -- that a tuple reader was fast enough and that one
// hand written columnar decoder per table was not worth its memory-safety
// surface -- and the measurement moved: on a workspace of 33 repositories the
// snapshot stage spent `2.8 s` of a `8.3 s` pass asking the engine for one
// value at a time, sixteen cgo crossings for every Symbol row. With the
// columnar reader that stage is `1.2 s`.
//
// What the old argument got right is that a pointer-arithmetic decoder can be
// wrong in ways that still produce a graph, so the tuple reader stays as the
// oracle: scanCanonicalTuples is the reference implementation, and
// TestScanCanonicalArrowMatchesTheTupleReader compares the two field by
// field over a fixture with every column type, a NULL in a nullable column,
// an empty table and a read that crosses chunk boundaries.
func ScanCanonical(ctx context.Context, path string) (CanonicalGraph, error) {
	return scanCanonicalArrow(ctx, path)
}

// scanCanonicalTuples reads the graph one value at a time through the Go
// binding. It is the reference implementation ScanCanonical's Arrow reader is
// tested against: a decoder that misreads a buffer produces a graph that
// looks plausible, and the only cheap way to disbelieve it is a second reader
// that cannot share the mistake.
func scanCanonicalTuples(ctx context.Context, path string) (CanonicalGraph, error) {
	if err := validatePath(path); err != nil {
		return CanonicalGraph{}, err
	}
	if err := ctx.Err(); err != nil {
		return CanonicalGraph{}, &Error{Op: "scan canonical", Err: err}
	}

	db, err := openCanonicalDatabase(ctx, path, true)
	if err != nil {
		return CanonicalGraph{}, err
	}
	defer db.Close()
	connection, err := openConnection(db)
	if err != nil {
		return CanonicalGraph{}, err
	}
	defer connection.Close()
	native := connection.native

	// Metadata, and the schema version it carries, are read and checked
	// before anything else: a graph this code does not understand must fail
	// before a single Symbol or edge row is paged in, not after.
	metadata, err := scanGraphMetadata(ctx, native)
	if err != nil {
		return CanonicalGraph{}, &Error{Op: "scan canonical", Err: fmt.Errorf("scan GraphMetadata: %w", err)}
	}
	schemaVersion, err := canonicalScanSchemaVersion(metadata)
	if err != nil {
		return CanonicalGraph{}, err
	}

	repositories, err := scanCanonicalRepositories(ctx, native)
	if err != nil {
		return CanonicalGraph{}, &Error{Op: "scan canonical", Err: fmt.Errorf("scan Repository: %w", err)}
	}
	packages, err := scanCanonicalPackages(ctx, native)
	if err != nil {
		return CanonicalGraph{}, &Error{Op: "scan canonical", Err: fmt.Errorf("scan Package: %w", err)}
	}
	files, err := scanCanonicalFiles(ctx, native)
	if err != nil {
		return CanonicalGraph{}, &Error{Op: "scan canonical", Err: fmt.Errorf("scan File: %w", err)}
	}
	symbols, err := scanCanonicalSymbols(ctx, native)
	if err != nil {
		return CanonicalGraph{}, &Error{Op: "scan canonical", Err: fmt.Errorf("scan Symbol: %w", err)}
	}
	evidence, err := scanCanonicalEvidence(ctx, native)
	if err != nil {
		return CanonicalGraph{}, &Error{Op: "scan canonical", Err: fmt.Errorf("scan Evidence: %w", err)}
	}
	unresolved, err := scanCanonicalUnresolvedReferences(ctx, native)
	if err != nil {
		return CanonicalGraph{}, &Error{Op: "scan canonical", Err: fmt.Errorf("scan UnresolvedReference: %w", err)}
	}

	// Every relationship table of the vocabulary is read in turn on this one
	// connection, in CanonicalRelationshipTables order; the result is then
	// sorted once by (Table, SourceKey, TargetKey) so the final order never
	// depends on that per-table iteration order, only on what is stored.
	vocabulary := canonicalEdgeVocabularyTables()
	edges := make([]CanonicalEdge, 0)
	for _, table := range vocabulary {
		tableEdges, err := scanCanonicalEdgeTable(ctx, native, table)
		if err != nil {
			return CanonicalGraph{}, &Error{Op: "scan canonical", Err: fmt.Errorf("scan %s: %w", table.Name, err)}
		}
		edges = append(edges, tableEdges...)
	}
	sort.Slice(edges, func(i, j int) bool { return canonicalEdgeLess(edges[i], edges[j]) })
	return CanonicalGraph{
		SchemaVersion: schemaVersion,
		Metadata:      metadata,
		Repositories:  repositories,
		Packages:      packages,
		Files:         files,
		Symbols:       symbols,
		Evidence:      evidence,
		Edges:         edges,
		Unresolved:    unresolved,
	}, nil
}

// canonicalScanSchemaVersion derives SchemaVersion from GraphMetadata and
// rejects a layout ScanCanonical does not understand. A HotSnapshot built
// from a graph whose schema has moved on would silently misread every
// column after the ones that happen to still line up by accident, so this
// fails loudly instead, wrapped over ErrInvalidCanonicalScan like every
// other domain validation error in this package (compare
// ErrInvalidCanonicalLoad in canonical_load.go): a bad schema version is a
// caller mistake, not a native engine failure, so it is never wrapped in
// *Error the way an open or query failure is.
func canonicalScanSchemaVersion(metadata map[string]string) (int, error) {
	stored, present := metadata["schema_version"]
	if !present {
		return 0, fmt.Errorf("%w: GraphMetadata has no schema_version", ErrInvalidCanonicalScan)
	}
	version, err := strconv.Atoi(stored)
	if err != nil {
		return 0, fmt.Errorf("%w: schema_version %q is not numeric: %v", ErrInvalidCanonicalScan, stored, err)
	}
	if version != CanonicalSchemaVersion {
		return 0, fmt.Errorf("%w: schema_version %d, want %d", ErrInvalidCanonicalScan, version, CanonicalSchemaVersion)
	}
	return version, nil
}

func scanGraphMetadata(ctx context.Context, connection *lbug.Connection) (map[string]string, error) {
	const query = `MATCH (n:GraphMetadata) RETURN n.key, n.value ORDER BY n.key`
	rows, err := scanCanonicalRows(ctx, connection, query, func(tuple *lbug.FlatTuple) ([2]string, error) {
		key, err1 := tupleOptionalString(tuple, 0)
		value, err2 := tupleOptionalString(tuple, 1)
		if err := errors.Join(err1, err2); err != nil {
			return [2]string{}, err
		}
		return [2]string{key, value}, nil
	})
	if err != nil {
		return nil, err
	}
	metadata := make(map[string]string, len(rows))
	for _, row := range rows {
		metadata[row[0]] = row[1]
	}
	return metadata, nil
}

func scanCanonicalRepositories(ctx context.Context, connection *lbug.Connection) ([]CanonicalRepository, error) {
	const query = `MATCH (n:Repository)
RETURN n.stable_key, n.name, n.root_path, n.commit, n.branch, n.dirty, n.languages
ORDER BY n.stable_key`
	return scanCanonicalRows(ctx, connection, query, decodeCanonicalRepository)
}

func decodeCanonicalRepository(tuple *lbug.FlatTuple) (CanonicalRepository, error) {
	stableKey, err1 := tupleOptionalString(tuple, 0)
	name, err2 := tupleOptionalString(tuple, 1)
	rootPath, err3 := tupleOptionalString(tuple, 2)
	commit, err4 := tupleOptionalString(tuple, 3)
	branch, err5 := tupleOptionalString(tuple, 4)
	dirty, err6 := tupleBool(tuple, 5)
	languages, err7 := tupleOptionalString(tuple, 6)
	if err := errors.Join(err1, err2, err3, err4, err5, err6, err7); err != nil {
		return CanonicalRepository{}, err
	}
	return CanonicalRepository{
		StableKey: stableKey, Name: name, RootPath: rootPath, Commit: commit,
		Branch: branch, Dirty: dirty, Languages: languages,
	}, nil
}

func scanCanonicalPackages(ctx context.Context, connection *lbug.Connection) ([]CanonicalPackage, error) {
	const query = `MATCH (n:Package)
RETURN n.stable_key, n.repository_key, n.language, n.name, n.version, n.root_path, n.manifest_path, n.container
ORDER BY n.stable_key`
	return scanCanonicalRows(ctx, connection, query, decodeCanonicalPackage)
}

func decodeCanonicalPackage(tuple *lbug.FlatTuple) (CanonicalPackage, error) {
	stableKey, err1 := tupleOptionalString(tuple, 0)
	repositoryKey, err2 := tupleOptionalString(tuple, 1)
	language, err3 := tupleOptionalString(tuple, 2)
	name, err4 := tupleOptionalString(tuple, 3)
	version, err5 := tupleOptionalString(tuple, 4)
	rootPath, err6 := tupleOptionalString(tuple, 5)
	manifestPath, err7 := tupleOptionalString(tuple, 6)
	container, err8 := tupleOptionalString(tuple, 7)
	if err := errors.Join(err1, err2, err3, err4, err5, err6, err7, err8); err != nil {
		return CanonicalPackage{}, err
	}
	return CanonicalPackage{
		StableKey: stableKey, RepositoryKey: repositoryKey, Language: language, Name: name,
		Version: version, RootPath: rootPath, ManifestPath: manifestPath, Container: container,
	}, nil
}

func scanCanonicalFiles(ctx context.Context, connection *lbug.Connection) ([]CanonicalFile, error) {
	const query = `MATCH (n:File)
RETURN n.stable_key, n.repository_key, n.package_key, n.path, n.language, n.content_hash, n.generated
ORDER BY n.stable_key`
	return scanCanonicalRows(ctx, connection, query, decodeCanonicalFile)
}

func decodeCanonicalFile(tuple *lbug.FlatTuple) (CanonicalFile, error) {
	stableKey, err1 := tupleOptionalString(tuple, 0)
	repositoryKey, err2 := tupleOptionalString(tuple, 1)
	packageKey, err3 := tupleOptionalString(tuple, 2)
	path, err4 := tupleOptionalString(tuple, 3)
	language, err5 := tupleOptionalString(tuple, 4)
	contentHash, err6 := tupleOptionalString(tuple, 5)
	generated, err7 := tupleBool(tuple, 6)
	if err := errors.Join(err1, err2, err3, err4, err5, err6, err7); err != nil {
		return CanonicalFile{}, err
	}
	return CanonicalFile{
		StableKey: stableKey, RepositoryKey: repositoryKey, PackageKey: packageKey, Path: path,
		Language: language, ContentHash: contentHash, Generated: generated,
	}, nil
}

func scanCanonicalSymbols(ctx context.Context, connection *lbug.Connection) ([]CanonicalSymbol, error) {
	const query = `MATCH (n:Symbol)
RETURN n.stable_key, n.canonical_identity, n.repository_key, n.package_key, n.file_key,
       n.language, n.name, n.qualified_name, n.kind, n.exported, n.signature,
       n.start_line, n.start_column, n.start_offset, n.end_line, n.end_offset
ORDER BY n.stable_key`
	return scanCanonicalRows(ctx, connection, query, decodeCanonicalSymbol)
}

func decodeCanonicalSymbol(tuple *lbug.FlatTuple) (CanonicalSymbol, error) {
	stableKey, err1 := tupleOptionalString(tuple, 0)
	canonicalIdentity, err2 := tupleOptionalString(tuple, 1)
	repositoryKey, err3 := tupleOptionalString(tuple, 2)
	packageKey, err4 := tupleOptionalString(tuple, 3)
	fileKey, err5 := tupleOptionalString(tuple, 4)
	language, err6 := tupleOptionalString(tuple, 5)
	name, err7 := tupleOptionalString(tuple, 6)
	qualifiedName, err8 := tupleOptionalString(tuple, 7)
	kind, err9 := tupleOptionalString(tuple, 8)
	exported, err10 := tupleBool(tuple, 9)
	signature, err11 := tupleOptionalString(tuple, 10)
	startLine, err12 := tupleInt64(tuple, 11)
	startColumn, err13 := tupleInt64(tuple, 12)
	startOffset, err14 := tupleInt64(tuple, 13)
	endLine, err15 := tupleInt64(tuple, 14)
	endOffset, err16 := tupleInt64(tuple, 15)
	if err := errors.Join(err1, err2, err3, err4, err5, err6, err7, err8, err9, err10, err11, err12, err13, err14, err15, err16); err != nil {
		return CanonicalSymbol{}, err
	}
	return CanonicalSymbol{
		StableKey: stableKey, CanonicalIdentity: canonicalIdentity, RepositoryKey: repositoryKey,
		PackageKey: packageKey, FileKey: fileKey, Language: language, Name: name, QualifiedName: qualifiedName,
		Kind: kind, Exported: exported, Signature: signature, StartLine: startLine, StartColumn: startColumn,
		StartOffset: startOffset, EndLine: endLine, EndOffset: endOffset,
	}, nil
}

func scanCanonicalEvidence(ctx context.Context, connection *lbug.Connection) ([]CanonicalEvidence, error) {
	const query = `MATCH (n:Evidence)
RETURN n.stable_key, n.repository_key, n.file_key, n.start_line, n.start_column, n.start_offset, n.end_offset, n.text
ORDER BY n.stable_key`
	return scanCanonicalRows(ctx, connection, query, decodeCanonicalEvidence)
}

func decodeCanonicalEvidence(tuple *lbug.FlatTuple) (CanonicalEvidence, error) {
	stableKey, err1 := tupleOptionalString(tuple, 0)
	repositoryKey, err2 := tupleOptionalString(tuple, 1)
	fileKey, err3 := tupleOptionalString(tuple, 2)
	startLine, err4 := tupleInt64(tuple, 3)
	startColumn, err5 := tupleInt64(tuple, 4)
	startOffset, err6 := tupleInt64(tuple, 5)
	endOffset, err7 := tupleInt64(tuple, 6)
	text, err8 := tupleOptionalString(tuple, 7)
	if err := errors.Join(err1, err2, err3, err4, err5, err6, err7, err8); err != nil {
		return CanonicalEvidence{}, err
	}
	return CanonicalEvidence{
		StableKey: stableKey, RepositoryKey: repositoryKey, FileKey: fileKey,
		StartLine: startLine, StartColumn: startColumn, StartOffset: startOffset, EndOffset: endOffset, Text: text,
	}, nil
}

func scanCanonicalUnresolvedReferences(ctx context.Context, connection *lbug.Connection) ([]CanonicalUnresolvedReference, error) {
	const query = `MATCH (n:UnresolvedReference)
RETURN n.stable_key, n.repository_key, n.file_key, n.source_symbol_key,
       n.language, n.requested_package, n.requested_symbol, n.reason, n.detail,
       n.start_line, n.start_column, n.start_offset
ORDER BY n.stable_key`
	return scanCanonicalRows(ctx, connection, query, decodeCanonicalUnresolvedReference)
}

func decodeCanonicalUnresolvedReference(tuple *lbug.FlatTuple) (CanonicalUnresolvedReference, error) {
	stableKey, err1 := tupleOptionalString(tuple, 0)
	repositoryKey, err2 := tupleOptionalString(tuple, 1)
	fileKey, err3 := tupleOptionalString(tuple, 2)
	sourceSymbolKey, err4 := tupleOptionalString(tuple, 3)
	language, err5 := tupleOptionalString(tuple, 4)
	requestedPackage, err6 := tupleOptionalString(tuple, 5)
	requestedSymbol, err7 := tupleOptionalString(tuple, 6)
	reason, err8 := tupleOptionalString(tuple, 7)
	detail, err9 := tupleOptionalString(tuple, 8)
	startLine, err10 := tupleInt64(tuple, 9)
	startColumn, err11 := tupleInt64(tuple, 10)
	startOffset, err12 := tupleInt64(tuple, 11)
	if err := errors.Join(err1, err2, err3, err4, err5, err6, err7, err8, err9, err10, err11, err12); err != nil {
		return CanonicalUnresolvedReference{}, err
	}
	return CanonicalUnresolvedReference{
		StableKey: stableKey, RepositoryKey: repositoryKey, FileKey: fileKey, SourceSymbolKey: sourceSymbolKey,
		Language: language, RequestedPackage: requestedPackage, RequestedSymbol: requestedSymbol,
		Reason: reason, Detail: detail, StartLine: startLine, StartColumn: startColumn, StartOffset: startOffset,
	}, nil
}

// scanCanonicalEdgeTable reads every row of one relationship table. The
// RETURN clause is built from table.Properties -- never a hand written list
// of column names per edge kind -- so a structural table (confidence,
// provenance) and a semantic table (those two plus evidence_key,
// source_snapshot, resolver_version) are each queried for exactly the
// columns canonical_schema.go says they have. Source and target node labels
// are left unlabelled in the pattern: the relationship type already fixes
// both endpoint node tables (a REFERENCES edge can only ever connect two
// Symbol rows), the same reasoning canonical_integrity_native.go and
// query_native.go's scanEdgesQuery already rely on.
func scanCanonicalEdgeTable(ctx context.Context, connection *lbug.Connection, table SchemaRelationshipTable) ([]CanonicalEdge, error) {
	columns := make([]string, 0, 2+len(table.Properties))
	columns = append(columns, "source.stable_key", "target.stable_key")
	for _, property := range table.Properties {
		columns = append(columns, "edge."+property.Name)
	}
	query := fmt.Sprintf("MATCH (source)-[edge:%s]->(target) RETURN %s ORDER BY source.stable_key, target.stable_key",
		table.Name, strings.Join(columns, ", "))
	return scanCanonicalRows(ctx, connection, query, func(tuple *lbug.FlatTuple) (CanonicalEdge, error) {
		return decodeCanonicalEdge(tuple, table)
	})
}

// decodeCanonicalEdge decodes source.stable_key, target.stable_key and then
// exactly the properties table.Properties declares, in that same order. Each
// property is matched by name rather than by position so a future reordering
// of edgeProperties/containmentProperties in canonical_schema.go can never
// silently swap two columns here.
func decodeCanonicalEdge(tuple *lbug.FlatTuple, table SchemaRelationshipTable) (CanonicalEdge, error) {
	sourceKey, err1 := tupleOptionalString(tuple, 0)
	targetKey, err2 := tupleOptionalString(tuple, 1)
	edge := CanonicalEdge{Table: table.Name, SourceKey: sourceKey, TargetKey: targetKey}
	decodeErrors := []error{err1, err2}
	for index, property := range table.Properties {
		column := uint64(2 + index)
		var propertyErr error
		switch property.Name {
		case "confidence":
			edge.Confidence, propertyErr = tupleOptionalString(tuple, column)
		case "provenance":
			edge.Provenance, propertyErr = tupleOptionalString(tuple, column)
		case "evidence_key":
			edge.EvidenceKey, propertyErr = tupleOptionalString(tuple, column)
		case "source_snapshot":
			edge.SourceSnapshot, propertyErr = tupleInt64(tuple, column)
		case "resolver_version":
			edge.ResolverVersion, propertyErr = tupleOptionalString(tuple, column)
		default:
			propertyErr = fmt.Errorf("canonical scan: table %s has unrecognised edge property %q", table.Name, property.Name)
		}
		decodeErrors = append(decodeErrors, propertyErr)
	}
	if err := errors.Join(decodeErrors...); err != nil {
		return CanonicalEdge{}, err
	}
	return edge, nil
}

// scanCanonicalRows executes query and decodes every row with decode. It
// keeps the same deadline handling as every other looped query in this
// package (queryCount, runIntegritySampleQuery, reader.references): a
// timeout derived from ctx before the call, cleared after, so a caller's
// cancellation reaches a long running scan without leaking the connection's
// timeout into whatever runs on it next.
func scanCanonicalRows[T any](ctx context.Context, connection *lbug.Connection, query string, decode func(*lbug.FlatTuple) (T, error)) ([]T, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := setQueryDeadline(connection, ctx); err != nil {
		return nil, err
	}
	defer connection.SetTimeout(0)
	result, err := connection.Query(query)
	if err != nil {
		if result != nil {
			result.Close()
		}
		return nil, err
	}
	defer result.Close()

	rows := make([]T, 0)
	for result.HasNext() {
		tuple, err := nextTuple(result)
		if err != nil {
			return nil, err
		}
		row, err := decode(tuple)
		tuple.Close()
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, ctx.Err()
}

// tupleOptionalString decodes a STRING column that may be NULL. LadybugDB's
// CSV loader stores an empty CSV field as NULL rather than as an empty
// string -- verified directly against the pinned v0.13.1 engine: loading a
// fact set whose Package.Version and an edge's EvidenceKey are both "" and
// reading them straight back returns a nil Go value, not "". A NULL here
// must therefore decode back to Go's zero string to round trip the value
// CanonicalTableRows originally wrote (canonical_load.go renders every
// STRING column unconditionally, empty or not), not surface as a decode
// error.
func tupleOptionalString(tuple *lbug.FlatTuple, index uint64) (string, error) {
	value, err := tuple.GetValue(index)
	if err != nil {
		return "", err
	}
	if value == nil {
		return "", nil
	}
	result, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("column %d has type %T, want string", index, value)
	}
	return result, nil
}

// tupleBool decodes a BOOL column. Every BOOL column of the canonical schema
// is written by strconv.FormatBool, which never produces an empty CSV field,
// so unlike tupleOptionalString this is never expected to observe NULL.
func tupleBool(tuple *lbug.FlatTuple, index uint64) (bool, error) {
	value, err := tuple.GetValue(index)
	if err != nil {
		return false, err
	}
	result, ok := value.(bool)
	if !ok {
		return false, fmt.Errorf("column %d has type %T, want bool", index, value)
	}
	return result, nil
}

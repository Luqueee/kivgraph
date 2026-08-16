package ladybug

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/Luqueee/kivgraph/internal/facts"
)

// ErrInvalidCanonicalDelta reports a facts.Delta that cannot be applied to
// the canonical graph: one that fails its own facts.Delta.Validate, one
// aimed at a database whose GraphMetadata is not the canonical schema at
// its expected version, or one whose Upsert asserts an edge whose endpoint
// does not exist once retirement has run.
var ErrInvalidCanonicalDelta = errors.New("invalid canonical delta")

// CanonicalMutationResult accounts for what one ApplyCanonicalDelta call
// actually changed. Every field is a count read back from the queries that
// performed the change, never an estimate derived from the delta's own
// input sizes: Diff legitimately repeats an unchanged Repository or Package
// in Upsert (facts.Delta.Validate's doc comment), and re-applying an
// already-applied delta legitimately retires nothing the second time
// around, so "what the delta named" and "what actually moved" are
// different numbers -- this reports the second one.
type CanonicalMutationResult struct {
	RemovedFiles    int64
	RemovedSymbols  int64
	RemovedEvidence int64
	RemovedEdges    int64
	UpsertedNodes   int64
	UpsertedEdges   int64
}

// canonicalPlaceholderFillerText fills the required-non-empty text fields
// (Name, RootPath, Path, CanonicalIdentity, QualifiedName) of a
// completeUpsertForRendering placeholder. The placeholder's own Key is
// always the real missing key, never this constant -- only the fields
// Set.Validate demands be non-empty but that canonicalUpsertRows never
// reads back (every such row is dropped before writing) need a value at
// all, and this is a visibly synthetic one, unrelated to any real durable
// key format (every real one names a concrete entity kind -- "repository:",
// "package:", "file:" -- unlike this literal).
const canonicalPlaceholderFillerText = "__canonical_delta_external_placeholder__"

// canonicalEdgeEndpointTables returns the entity table name -- "Repository",
// "Package", "File" or "Symbol" -- that kind's source and target must
// resolve against, derived from CanonicalRelationshipTables' own From/To
// rather than a second hand written switch over facts.EdgeKind values.
func canonicalEdgeEndpointTables(kind facts.EdgeKind) (from, to string, ok bool) {
	for _, table := range CanonicalRelationshipTables() {
		if table.Name == string(kind) {
			return table.From, table.To, true
		}
	}
	return "", "", false
}

// completeUpsertForRendering returns a clone of upsert with one minimal,
// self consistent placeholder record added for every edge endpoint upsert's
// own Repositories/Packages/Files/Symbols do not already resolve, plus the
// set of keys those placeholders were added under.
//
// A file's delta legitimately carries an edge whose other endpoint lives in
// a file the delta does not touch -- "alguien te llama desde fuera", or the
// touched file calling out to code that never changed -- and
// facts.Delta.Validate deliberately allows that through its own Upsert
// check (see its doc comment: Upsert is a fragment, not a closed graph).
// facts.Set.Validate does not: it demands every edge's source and target
// resolve inside the very set being validated, and CanonicalTableRows calls
// it unconditionally. Without this completion step, rendering upsert
// directly would reject exactly the deltas this package exists to apply.
//
// The placeholder for a missing key reuses that same key as its own
// container chain -- a placeholder Symbol's FileKey/PackageKey/
// RepositoryKey all point back at itself, a placeholder File/Package's
// RepositoryKey too -- because Set.Validate resolves every reference
// against its own collection's key set, never across collections, so even
// self-referencing placeholders satisfy it. They are never meant to reach
// the database: canonicalUpsertRows renders through the exact same
// CanonicalTableRows the full load uses (this is not a second renderer),
// then drops every row whose primary key is one of these placeholder keys.
// Restating a node this database already holds, correctly, under its real
// values is Diff's job (design decision 4, facts.Delta's doc comment), not
// this completion step's -- a placeholder is scaffolding for one
// Set.Validate call, nothing more.
func completeUpsertForRendering(upsert facts.Set) (facts.Set, map[string]struct{}) {
	repositories := make(map[string]struct{}, len(upsert.Repositories))
	for _, repository := range upsert.Repositories {
		repositories[repository.Key] = struct{}{}
	}
	packages := make(map[string]struct{}, len(upsert.Packages))
	for _, entry := range upsert.Packages {
		packages[entry.Key] = struct{}{}
	}
	files := make(map[string]struct{}, len(upsert.Files))
	for _, file := range upsert.Files {
		files[file.Key] = struct{}{}
	}
	symbols := make(map[string]struct{}, len(upsert.Symbols))
	for _, symbol := range upsert.Symbols {
		symbols[symbol.Key] = struct{}{}
	}

	completed := facts.Set{
		Repositories: append([]facts.Repository(nil), upsert.Repositories...),
		Packages:     append([]facts.Package(nil), upsert.Packages...),
		Files:        append([]facts.File(nil), upsert.Files...),
		Symbols:      append([]facts.Symbol(nil), upsert.Symbols...),
		Evidence:     upsert.Evidence,
		Edges:        upsert.Edges,
		Unresolved:   upsert.Unresolved,
	}
	placeholders := make(map[string]struct{})

	var addPlaceholder func(entityTable, key string)
	addPlaceholder = func(entityTable, key string) {
		if key == "" {
			return
		}
		switch entityTable {
		case "Repository":
			if _, known := repositories[key]; known {
				return
			}
			repositories[key] = struct{}{}
			placeholders[key] = struct{}{}
			completed.Repositories = append(completed.Repositories, facts.Repository{
				Key: key, Name: canonicalPlaceholderFillerText, RootPath: canonicalPlaceholderFillerText,
			})
		case "Package":
			if _, known := packages[key]; known {
				return
			}
			packages[key] = struct{}{}
			placeholders[key] = struct{}{}
			completed.Packages = append(completed.Packages, facts.Package{
				Key: key, RepositoryKey: key, Name: canonicalPlaceholderFillerText, RootPath: canonicalPlaceholderFillerText,
			})
			addPlaceholder("Repository", key)
		case "File":
			if _, known := files[key]; known {
				return
			}
			files[key] = struct{}{}
			placeholders[key] = struct{}{}
			completed.Files = append(completed.Files, facts.File{
				Key: key, RepositoryKey: key, Path: canonicalPlaceholderFillerText,
			})
			addPlaceholder("Repository", key)
		case "Symbol":
			if _, known := symbols[key]; known {
				return
			}
			symbols[key] = struct{}{}
			placeholders[key] = struct{}{}
			completed.Symbols = append(completed.Symbols, facts.Symbol{
				Key: key, CanonicalIdentity: canonicalPlaceholderFillerText, QualifiedName: canonicalPlaceholderFillerText,
				RepositoryKey: key, PackageKey: key, FileKey: key,
			})
			addPlaceholder("File", key)
			addPlaceholder("Package", key)
		}
	}

	for _, edge := range completed.Edges {
		from, to, ok := canonicalEdgeEndpointTables(edge.Kind)
		if !ok {
			continue
		}
		addPlaceholder(from, edge.SourceKey)
		addPlaceholder(to, edge.TargetKey)
	}

	return completed, placeholders
}

// canonicalUpsertRows renders upsert into canonical table rows through
// CanonicalTableRows -- completing it first so an edge reaching outside the
// fragment does not make facts.Set.Validate reject a legitimate delta (see
// completeUpsertForRendering) -- and then drops every node row whose
// primary key is one of the placeholders that completion added. Relationship
// rows are never filtered: an edge's own from/to columns are always the
// real endpoint keys the delta actually asserts, placeholder or not.
func canonicalUpsertRows(upsert facts.Set, options CanonicalLoadOptions) (map[string][][]string, error) {
	completed, placeholders := completeUpsertForRendering(upsert)
	tableRows, err := CanonicalTableRows(completed, options)
	if err != nil {
		return nil, err
	}
	if len(placeholders) == 0 {
		return tableRows, nil
	}
	for _, table := range CanonicalNodeTables() {
		rows, exists := tableRows[table.Name]
		if !exists {
			continue
		}
		kept := rows[:0]
		for _, row := range rows {
			if _, isPlaceholder := placeholders[row[0]]; !isPlaceholder {
				kept = append(kept, row)
			}
		}
		if len(kept) == 0 {
			delete(tableRows, table.Name)
		} else {
			tableRows[table.Name] = kept
		}
	}
	return tableRows, nil
}

// canonicalColumnTypes returns the declared LadybugDB type ("STRING",
// "INT64" or "BOOL") of every column CanonicalColumns(table) would render
// for table, keyed by column name. CanonicalColumns itself intentionally
// drops this information -- a CSV column is text regardless of the type it
// loads into -- but a Cypher UNWIND row needs it back: ON CREATE/ON MATCH
// SET rejects an implicit STRING -> INT64 cast from a struct field
// (verified directly against the pinned v0.13.1 engine: MERGE (n:T {pk:
// row.pk}) ... SET n.start_line = row.start_line fails with "Binder
// exception: ... has data type STRING but expected INT64" when
// row.start_line is a string, even though the identical value bound as a
// plain top level parameter, not a struct field, is cast implicitly).
// "from"/"to" (relationship endpoint keys) and every node primary key are
// always STRING: a canonical key is always a durable Kivgraph string.
func canonicalColumnTypes(table string) (map[string]string, bool) {
	for _, node := range CanonicalNodeTables() {
		if node.Name != table {
			continue
		}
		types := make(map[string]string, len(node.Properties)+1)
		types[node.PrimaryKey.Name] = node.PrimaryKey.Type
		for _, property := range node.Properties {
			types[property.Name] = property.Type
		}
		return types, true
	}
	for _, relationship := range CanonicalRelationshipTables() {
		if relationship.Name != table {
			continue
		}
		types := make(map[string]string, len(relationship.Properties)+2)
		types["from"] = "STRING"
		types["to"] = "STRING"
		for _, property := range relationship.Properties {
			types[property.Name] = property.Type
		}
		return types, true
	}
	return nil, false
}

// canonicalTypedRows converts CanonicalTableRows' CSV shaped rows for one
// table -- columns and rows both come from that same call, never
// reconstructed by hand -- into the typed parameter rows a Cypher UNWIND
// can bind. This is not a second renderer: the column-to-value mapping
// stays entirely CanonicalTableRows'/rowFromColumns' own; this only
// restores, from the schema types canonicalColumnTypes reads, the Go type
// each column's text already represents.
func canonicalTypedRows(columns []string, types map[string]string, rows [][]string) ([]map[string]any, error) {
	typed := make([]map[string]any, len(rows))
	for rowIndex, row := range rows {
		entry := make(map[string]any, len(columns))
		for columnIndex, column := range columns {
			raw := row[columnIndex]
			switch types[column] {
			case "INT64":
				value, err := strconv.ParseInt(raw, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("%w: column %s value %q is not an integer: %v", ErrInvalidCanonicalDelta, column, raw, err)
				}
				entry[column] = value
			case "BOOL":
				value, err := strconv.ParseBool(raw)
				if err != nil {
					return nil, fmt.Errorf("%w: column %s value %q is not a boolean: %v", ErrInvalidCanonicalDelta, column, raw, err)
				}
				entry[column] = value
			default:
				entry[column] = raw
			}
		}
		typed[rowIndex] = entry
	}
	return typed, nil
}

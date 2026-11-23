package ladybug

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// ErrInvalidCanonicalLoad reports a fact set or option that cannot become a
// canonical bulk load.
var ErrInvalidCanonicalLoad = errors.New("invalid canonical load")

// CanonicalLoadOptions carries the provenance stamped on every semantic edge.
type CanonicalLoadOptions struct {
	// SnapshotID identifies the generation this load belongs to. It is
	// stamped on every semantic edge as source_snapshot.
	SnapshotID int64
	// ResolverVersion identifies the resolver build that produced the facts.
	ResolverVersion string
}

// CanonicalColumns returns the CSV column order of one canonical table.
// Node tables start with the primary key; relationship tables start with the
// source key and the target key, which is what LadybugDB COPY expects.
func CanonicalColumns(table string) ([]string, bool) {
	for _, node := range CanonicalNodeTables() {
		if node.Name != table {
			continue
		}
		columns := make([]string, 0, len(node.Properties)+1)
		columns = append(columns, node.PrimaryKey.Name)
		for _, property := range node.Properties {
			columns = append(columns, property.Name)
		}
		return columns, true
	}
	for _, relationship := range CanonicalRelationshipTables() {
		if relationship.Name != table {
			continue
		}
		columns := make([]string, 0, len(relationship.Properties)+2)
		columns = append(columns, "from", "to")
		for _, property := range relationship.Properties {
			columns = append(columns, property.Name)
		}
		return columns, true
	}
	return nil, false
}

// columnsOrError looks up a table Kivgraph itself names, as opposed to a table
// name derived from fact data (an edge kind). Any miss here means this file
// and canonical_schema.go have drifted apart, which is a bug, not bad input.
func columnsOrError(table string) ([]string, error) {
	columns, exists := CanonicalColumns(table)
	if !exists {
		return nil, fmt.Errorf("%w: canonical schema has no table %q", ErrInvalidCanonicalLoad, table)
	}
	return columns, nil
}

// rowFromColumns renders one row in schema column order. A column absent
// from values becomes an empty field; that only happens for the relationship
// properties a given edge kind does not carry, such as evidence_key on a
// containment edge.
func rowFromColumns(columns []string, values map[string]string) []string {
	row := make([]string, len(columns))
	for index, column := range columns {
		row[index] = values[column]
	}
	return row
}

// CanonicalTableRows renders a fact set as the rows of every canonical table.
// A table with no rows is absent from the map.
//
// It holds every table at once, which is what a comparison wants and what a
// load must not do: LoadCanonical stages one table at a time through
// renderCanonicalTables.
func CanonicalTableRows(set facts.Set, options CanonicalLoadOptions) (map[string][][]string, error) {
	tables := make(map[string][][]string)
	err := renderCanonicalTables(set, options, func(table string, rows [][]string) error {
		tables[table] = rows
		return nil
	})
	if err != nil {
		return nil, err
	}
	return tables, nil
}

// renderCanonicalTables renders the non-empty canonical tables of a fact set,
// one at a time, in the order a load needs: node tables in
// CanonicalNodeTables order, then relationship tables in
// CanonicalRelationshipTables order. emit receives a table and is done with its
// rows when it returns, so the rows of one table are all that is alive on top of
// the facts.
//
// Rendering every table before writing any of them held a second copy of the
// whole graph as strings -- every row a slice of strings -- beside the facts it
// came from, for the length of the load.
func renderCanonicalTables(set facts.Set, options CanonicalLoadOptions, emit func(table string, rows [][]string) error) error {
	if err := set.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidCanonicalLoad, err)
	}
	if strings.TrimSpace(options.ResolverVersion) == "" {
		return fmt.Errorf("%w: resolver version is empty", ErrInvalidCanonicalLoad)
	}

	// A sorted view: the caller's set is never changed, and a collection that
	// is already in key order -- every merged set is -- is not copied.
	renderer := canonicalRenderer{
		set:      set.Sorted(),
		options:  options,
		snapshot: strconv.FormatInt(options.SnapshotID, 10),
	}
	// Every semantic and containment relation is keyed by its own EdgeKind,
	// so the table name always comes straight from the edge. The columns of a
	// kind are resolved once: CanonicalColumns rebuilds the entire schema on
	// every call, and the graph holds one edge per reference in the corpus. A
	// kind with no table is refused before anything is emitted.
	counts := make(map[facts.EdgeKind]int)
	for _, edge := range renderer.set.Edges {
		counts[edge.Kind]++
	}
	renderer.edgesByKind = make(map[facts.EdgeKind][]int, len(counts))
	renderer.columnsByKind = make(map[facts.EdgeKind][]string, len(counts))
	for kind, count := range counts {
		columns, exists := CanonicalColumns(string(kind))
		if !exists {
			return fmt.Errorf("%w: edge kind %q has no canonical relationship table", ErrInvalidCanonicalLoad, kind)
		}
		renderer.columnsByKind[kind] = columns
		renderer.edgesByKind[kind] = make([]int, 0, count)
	}
	for index, edge := range renderer.set.Edges {
		renderer.edgesByKind[edge.Kind] = append(renderer.edgesByKind[edge.Kind], index)
	}

	for _, table := range CanonicalNodeTables() {
		if err := renderer.emit(table.Name, emit); err != nil {
			return err
		}
	}
	for _, table := range CanonicalRelationshipTables() {
		if err := renderer.emit(table.Name, emit); err != nil {
			return err
		}
	}
	return nil
}

// canonicalRenderer holds what rendering any one table needs.
type canonicalRenderer struct {
	set           facts.Set
	options       CanonicalLoadOptions
	snapshot      string
	edgesByKind   map[facts.EdgeKind][]int
	columnsByKind map[facts.EdgeKind][]string
}

// emit renders one table, hands it over when it has rows, and lets go of the
// facts no later table reads.
func (renderer *canonicalRenderer) emit(table string, emit func(string, [][]string) error) error {
	rows, err := renderer.table(table)
	if err != nil {
		return err
	}
	if len(rows) != 0 {
		if err := emit(table, rows); err != nil {
			return err
		}
	}
	// Edges, evidence and unresolved references are read by the relationship
	// tables that come after the nodes; these four are read by their own table
	// and no other.
	switch table {
	case "Repository":
		renderer.set.Repositories = nil
	case "Package":
		renderer.set.Packages = nil
	case "File":
		renderer.set.Files = nil
	case "Symbol":
		renderer.set.Symbols = nil
	}
	return nil
}

// table renders one canonical table, or nothing when the facts imply no row.
func (renderer *canonicalRenderer) table(name string) ([][]string, error) {
	set, options := renderer.set, renderer.options
	switch name {
	case "GraphMetadata":
		columns, err := columnsOrError(name)
		if err != nil {
			return nil, err
		}
		return graphMetadataRows(columns, options), nil
	case "Repository":
		return renderRows(name, set.Repositories, func(columns []string, repository facts.Repository) []string {
			return rowFromColumns(columns, map[string]string{
				"stable_key": repository.Key,
				"name":       repository.Name,
				"root_path":  repository.RootPath,
				"commit":     repository.Commit,
				"branch":     repository.Branch,
				"dirty":      strconv.FormatBool(repository.Dirty),
				"languages":  joinLanguages(repository.Languages),
			})
		})
	case "Package":
		return renderRows(name, set.Packages, func(columns []string, pkg facts.Package) []string {
			return rowFromColumns(columns, map[string]string{
				"stable_key":     pkg.Key,
				"repository_key": pkg.RepositoryKey,
				"language":       string(pkg.Language),
				"name":           pkg.Name,
				"version":        pkg.Version,
				"root_path":      pkg.RootPath,
				"manifest_path":  pkg.ManifestPath,
				"container":      pkg.Container,
			})
		})
	case "File":
		return renderRows(name, set.Files, func(columns []string, file facts.File) []string {
			return rowFromColumns(columns, map[string]string{
				"stable_key":     file.Key,
				"repository_key": file.RepositoryKey,
				"package_key":    file.PackageKey,
				"path":           file.Path,
				"language":       string(file.Language),
				"content_hash":   file.ContentHash,
				"generated":      strconv.FormatBool(file.Generated),
			})
		})
	case "Symbol":
		return renderRows(name, set.Symbols, func(columns []string, symbol facts.Symbol) []string {
			return rowFromColumns(columns, map[string]string{
				"stable_key":         symbol.Key,
				"canonical_identity": symbol.CanonicalIdentity,
				"repository_key":     symbol.RepositoryKey,
				"package_key":        symbol.PackageKey,
				"file_key":           symbol.FileKey,
				"language":           string(symbol.Language),
				"name":               symbol.Name,
				"qualified_name":     symbol.QualifiedName,
				"kind":               symbol.Kind,
				"exported":           strconv.FormatBool(symbol.Exported),
				"signature":          symbol.Signature,
				"start_line":         strconv.Itoa(symbol.Start.Line),
				"start_column":       strconv.Itoa(symbol.Start.Column),
				"start_offset":       strconv.Itoa(symbol.Start.Offset),
				"end_line":           strconv.Itoa(symbol.End.Line),
				"end_offset":         strconv.Itoa(symbol.End.Offset),
			})
		})
	case "Evidence":
		return renderRows(name, set.Evidence, func(columns []string, evidence facts.Evidence) []string {
			return rowFromColumns(columns, map[string]string{
				"stable_key":     evidence.Key,
				"repository_key": evidence.RepositoryKey,
				"file_key":       evidence.FileKey,
				"start_line":     strconv.Itoa(evidence.Start.Line),
				"start_column":   strconv.Itoa(evidence.Start.Column),
				"start_offset":   strconv.Itoa(evidence.Start.Offset),
				"end_offset":     strconv.Itoa(evidence.End.Offset),
				"text":           evidence.Text,
			})
		})
	case "UnresolvedReference":
		return renderRows(name, set.Unresolved, func(columns []string, unresolved facts.UnresolvedReference) []string {
			return rowFromColumns(columns, map[string]string{
				"stable_key":        facts.UnresolvedKey(unresolved),
				"repository_key":    unresolved.RepositoryKey,
				"file_key":          unresolved.FileKey,
				"language":          string(unresolved.Language),
				"source_symbol_key": unresolved.SourceSymbolKey,
				"requested_package": unresolved.RequestedPackage,
				"requested_symbol":  unresolved.RequestedSymbol,
				"reason":            unresolved.Reason,
				"detail":            unresolved.Detail,
				"start_line":        strconv.Itoa(unresolved.Start.Line),
				"start_column":      strconv.Itoa(unresolved.Start.Column),
				"start_offset":      strconv.Itoa(unresolved.Start.Offset),
			})
		})
	case "OBSERVED_IN":
		// OBSERVED_IN and REPORTS_UNRESOLVED have no facts.EdgeKind of their
		// own; Evidence and UnresolvedReference already carry the owning key,
		// so the relation is derived rather than read off an edge.
		columns, err := columnsOrError(name)
		if err != nil {
			return nil, err
		}
		var rows [][]string
		for _, evidence := range set.Evidence {
			if evidence.FileKey != "" {
				rows = append(rows, rowFromColumns(columns, map[string]string{"from": evidence.Key, "to": evidence.FileKey}))
			}
		}
		return rows, nil
	case "REPORTS_UNRESOLVED":
		return renderRows(name, set.Unresolved, func(columns []string, unresolved facts.UnresolvedReference) []string {
			return rowFromColumns(columns, map[string]string{"from": unresolved.RepositoryKey, "to": facts.UnresolvedKey(unresolved)})
		})
	}

	kind := facts.EdgeKind(name)
	indexes := renderer.edgesByKind[kind]
	if len(indexes) == 0 {
		return nil, nil
	}
	columns := renderer.columnsByKind[kind]
	rows := make([][]string, len(indexes))
	for position, index := range indexes {
		edge := set.Edges[index]
		rows[position] = rowFromColumns(columns, map[string]string{
			"from":             edge.SourceKey,
			"to":               edge.TargetKey,
			"confidence":       string(edge.Confidence),
			"provenance":       string(edge.Provenance),
			"evidence_key":     edge.EvidenceKey,
			"source_snapshot":  renderer.snapshot,
			"resolver_version": options.ResolverVersion,
		})
	}
	return rows, nil
}

// renderRows renders one row per item in the column order of the table, and
// nothing for a table the facts give no row.
func renderRows[Item any](table string, items []Item, render func(columns []string, item Item) []string) ([][]string, error) {
	if len(items) == 0 {
		return nil, nil
	}
	columns, err := columnsOrError(table)
	if err != nil {
		return nil, err
	}
	rows := make([][]string, len(items))
	for index, item := range items {
		rows[index] = render(columns, item)
	}
	return rows, nil
}

// graphMetadataRows renders the fixed identity of the stored graph, sorted by
// key so the load is byte deterministic. It never carries a timestamp or a
// machine path: those would make two rebuilds of the same facts differ.
func graphMetadataRows(columns []string, options CanonicalLoadOptions) [][]string {
	entries := map[string]string{
		"schema_version":    strconv.Itoa(CanonicalSchemaVersion),
		"resolver_version":  options.ResolverVersion,
		"snapshot_id":       strconv.FormatInt(options.SnapshotID, 10),
		"stable_key_format": strconv.FormatUint(uint64(hotsnapshot.StableKeyFormatVersion), 10),
	}
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	rows := make([][]string, len(keys))
	for index, key := range keys {
		rows[index] = rowFromColumns(columns, map[string]string{"key": key, "value": entries[key]})
	}
	return rows
}

// joinLanguages renders a repository's languages the way the schema
// documents them: comma separated and sorted, so discovery order never leaks
// into the stored graph.
func joinLanguages(languages []facts.Language) string {
	if len(languages) == 0 {
		return ""
	}
	names := make([]string, len(languages))
	for index, language := range languages {
		names[index] = string(language)
	}
	sort.Strings(names)
	return strings.Join(names, ",")
}

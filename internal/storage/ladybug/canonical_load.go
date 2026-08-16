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
func CanonicalTableRows(set facts.Set, options CanonicalLoadOptions) (map[string][][]string, error) {
	if err := set.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidCanonicalLoad, err)
	}
	if strings.TrimSpace(options.ResolverVersion) == "" {
		return nil, fmt.Errorf("%w: resolver version is empty", ErrInvalidCanonicalLoad)
	}

	// A defensive copy: Sort mutates in place, and a caller that handed us a
	// set has every right to expect it back unchanged.
	clone := cloneSorted(set)
	tables := make(map[string][][]string)

	metadataColumns, err := columnsOrError("GraphMetadata")
	if err != nil {
		return nil, err
	}
	tables["GraphMetadata"] = graphMetadataRows(metadataColumns, options)

	if len(clone.Repositories) > 0 {
		columns, err := columnsOrError("Repository")
		if err != nil {
			return nil, err
		}
		rows := make([][]string, len(clone.Repositories))
		for index, repository := range clone.Repositories {
			rows[index] = rowFromColumns(columns, map[string]string{
				"stable_key": repository.Key,
				"name":       repository.Name,
				"root_path":  repository.RootPath,
				"commit":     repository.Commit,
				"branch":     repository.Branch,
				"dirty":      strconv.FormatBool(repository.Dirty),
				"languages":  joinLanguages(repository.Languages),
			})
		}
		tables["Repository"] = rows
	}

	if len(clone.Packages) > 0 {
		columns, err := columnsOrError("Package")
		if err != nil {
			return nil, err
		}
		rows := make([][]string, len(clone.Packages))
		for index, pkg := range clone.Packages {
			rows[index] = rowFromColumns(columns, map[string]string{
				"stable_key":     pkg.Key,
				"repository_key": pkg.RepositoryKey,
				"language":       string(pkg.Language),
				"name":           pkg.Name,
				"version":        pkg.Version,
				"root_path":      pkg.RootPath,
				"manifest_path":  pkg.ManifestPath,
				"container":      pkg.Container,
			})
		}
		tables["Package"] = rows
	}

	if len(clone.Files) > 0 {
		columns, err := columnsOrError("File")
		if err != nil {
			return nil, err
		}
		rows := make([][]string, len(clone.Files))
		for index, file := range clone.Files {
			rows[index] = rowFromColumns(columns, map[string]string{
				"stable_key":     file.Key,
				"repository_key": file.RepositoryKey,
				"package_key":    file.PackageKey,
				"path":           file.Path,
				"language":       string(file.Language),
				"content_hash":   file.ContentHash,
				"generated":      strconv.FormatBool(file.Generated),
			})
		}
		tables["File"] = rows
	}

	if len(clone.Symbols) > 0 {
		columns, err := columnsOrError("Symbol")
		if err != nil {
			return nil, err
		}
		rows := make([][]string, len(clone.Symbols))
		for index, symbol := range clone.Symbols {
			rows[index] = rowFromColumns(columns, map[string]string{
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
		}
		tables["Symbol"] = rows
	}

	if len(clone.Evidence) > 0 {
		columns, err := columnsOrError("Evidence")
		if err != nil {
			return nil, err
		}
		rows := make([][]string, len(clone.Evidence))
		for index, evidence := range clone.Evidence {
			rows[index] = rowFromColumns(columns, map[string]string{
				"stable_key":     evidence.Key,
				"repository_key": evidence.RepositoryKey,
				"file_key":       evidence.FileKey,
				"start_line":     strconv.Itoa(evidence.Start.Line),
				"start_column":   strconv.Itoa(evidence.Start.Column),
				"start_offset":   strconv.Itoa(evidence.Start.Offset),
				"end_offset":     strconv.Itoa(evidence.End.Offset),
				"text":           evidence.Text,
			})
		}
		tables["Evidence"] = rows
	}

	if len(clone.Unresolved) > 0 {
		columns, err := columnsOrError("UnresolvedReference")
		if err != nil {
			return nil, err
		}
		rows := make([][]string, len(clone.Unresolved))
		for index, unresolved := range clone.Unresolved {
			rows[index] = rowFromColumns(columns, map[string]string{
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
		}
		tables["UnresolvedReference"] = rows
	}

	// Every semantic and containment relation is keyed by its own EdgeKind,
	// so the table name always comes straight from the edge.
	//
	// The columns of a kind are resolved once, and the snapshot rendered
	// once: CanonicalColumns rebuilds the entire schema on every call, and
	// the graph holds one edge per reference in the corpus.
	snapshot := strconv.FormatInt(options.SnapshotID, 10)
	edgesByKind := make(map[facts.EdgeKind]int)
	for _, edge := range clone.Edges {
		edgesByKind[edge.Kind]++
	}
	columnsByKind := make(map[facts.EdgeKind][]string, len(edgesByKind))
	for kind := range edgesByKind {
		columns, exists := CanonicalColumns(string(kind))
		if !exists {
			return nil, fmt.Errorf("%w: edge kind %q has no canonical relationship table", ErrInvalidCanonicalLoad, kind)
		}
		columnsByKind[kind] = columns
		tables[string(kind)] = make([][]string, 0, edgesByKind[kind])
	}
	for _, edge := range clone.Edges {
		table := string(edge.Kind)
		tables[table] = append(tables[table], rowFromColumns(columnsByKind[edge.Kind], map[string]string{
			"from":             edge.SourceKey,
			"to":               edge.TargetKey,
			"confidence":       string(edge.Confidence),
			"provenance":       string(edge.Provenance),
			"evidence_key":     edge.EvidenceKey,
			"source_snapshot":  snapshot,
			"resolver_version": options.ResolverVersion,
		}))
	}

	// OBSERVED_IN and REPORTS_UNRESOLVED have no facts.EdgeKind of their own;
	// Evidence and UnresolvedReference already carry the owning key, so the
	// relation is derived rather than read off an edge.
	observedColumns, err := columnsOrError("OBSERVED_IN")
	if err != nil {
		return nil, err
	}
	for _, evidence := range clone.Evidence {
		if evidence.FileKey == "" {
			continue
		}
		tables["OBSERVED_IN"] = append(tables["OBSERVED_IN"], rowFromColumns(observedColumns, map[string]string{
			"from": evidence.Key,
			"to":   evidence.FileKey,
		}))
	}

	reportsColumns, err := columnsOrError("REPORTS_UNRESOLVED")
	if err != nil {
		return nil, err
	}
	for _, unresolved := range clone.Unresolved {
		tables["REPORTS_UNRESOLVED"] = append(tables["REPORTS_UNRESOLVED"], rowFromColumns(reportsColumns, map[string]string{
			"from": unresolved.RepositoryKey,
			"to":   facts.UnresolvedKey(unresolved),
		}))
	}

	return tables, nil
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

// cloneSorted copies every collection before sorting, so a caller's set is
// never mutated just because it asked for deterministic rows.
func cloneSorted(set facts.Set) facts.Set {
	clone := facts.Set{
		Repositories: append([]facts.Repository(nil), set.Repositories...),
		Packages:     append([]facts.Package(nil), set.Packages...),
		Files:        append([]facts.File(nil), set.Files...),
		Symbols:      append([]facts.Symbol(nil), set.Symbols...),
		Evidence:     append([]facts.Evidence(nil), set.Evidence...),
		Edges:        append([]facts.Edge(nil), set.Edges...),
		Unresolved:   append([]facts.UnresolvedReference(nil), set.Unresolved...),
	}
	clone.Sort()
	return clone
}

//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/Luqueee/luque/internal/facts"
)

// Retirement queries. Every one filters on a bound $files/$keys list
// parameter -- verified directly against the pinned v0.13.1 engine to
// accept `WHERE x.stable_key IN $param` with a genuine list argument,
// distinct from the literal Cypher list syntax the rest of this package's
// read paths use -- so the caller must never invoke one with an empty
// slice: binding an empty Go slice as a LIST parameter is itself a bind
// error ("failed to create LIST value because the slice is empty"),
// verified the same way.
const (
	// canonicalRetiredSymbolsQuery finds every symbol a retired file
	// declares, via its DEFINES edge.
	canonicalRetiredSymbolsQuery = `MATCH (file:File)-[:DEFINES]->(symbol:Symbol)
WHERE file.stable_key IN $files
RETURN symbol.stable_key`
	// canonicalRetiredEvidenceQuery finds every observation a retired file
	// carries, via its OBSERVED_IN edge.
	canonicalRetiredEvidenceQuery = `MATCH (evidence:Evidence)-[:OBSERVED_IN]->(file:File)
WHERE file.stable_key IN $files
RETURN evidence.stable_key`
	// canonicalRetiredUnresolvedQuery finds every unresolved reference a
	// retired file carries. UnresolvedReference has no edge to File --
	// REPORTS_UNRESOLVED runs from Repository, because a module level
	// failure has no file at all -- so file ownership is the file_key
	// property instead of a graph pattern.
	canonicalRetiredUnresolvedQuery = `MATCH (unresolved:UnresolvedReference)
WHERE unresolved.file_key IN $files
RETURN unresolved.stable_key`

	deleteCanonicalSymbolsQuery = `MATCH (symbol:Symbol)
WHERE symbol.stable_key IN $keys
DELETE symbol
RETURN count(*)`
	deleteCanonicalEvidenceQuery = `MATCH (evidence:Evidence)
WHERE evidence.stable_key IN $keys
DELETE evidence
RETURN count(*)`
	deleteCanonicalUnresolvedQuery = `MATCH (unresolved:UnresolvedReference)
WHERE unresolved.stable_key IN $keys
DELETE unresolved
RETURN count(*)`
	deleteCanonicalFilesQuery = `MATCH (file:File)
WHERE file.stable_key IN $keys
DELETE file
RETURN count(*)`
)

// ApplyCanonicalDelta applies one delta to the canonical graph at path, in a
// single transaction: a failure at any point leaves the database exactly as
// it was (see the deferred rollback below).
//
// The unit of a delta is the file (facts.Delta's doc comment): retirement
// withdraws everything ReplacedFiles and RemovedFiles asserted -- their
// symbols, evidence, unresolved references, and every edge anchored on any
// of those, incoming and outgoing, across every canonical relationship
// table -- before Upsert restates the current truth. That ordering, and
// retiring both directions rather than just outgoing edges, is what keeps a
// replaced or removed file from ever leaving a ghost symbol or a ghost edge
// behind, the LUQUE-1007 gate this type exists to satisfy.
func ApplyCanonicalDelta(ctx context.Context, path string, delta facts.Delta, options CanonicalLoadOptions) (result CanonicalMutationResult, returnErr error) {
	if err := delta.Validate(); err != nil {
		return CanonicalMutationResult{}, fmt.Errorf("%w: %v", ErrInvalidCanonicalDelta, err)
	}
	if err := validatePath(path); err != nil {
		return CanonicalMutationResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return CanonicalMutationResult{}, &Error{Op: "apply canonical delta", Err: err}
	}

	db, err := openCanonicalDatabase(ctx, path, false)
	if err != nil {
		return CanonicalMutationResult{}, err
	}
	defer db.Close()
	connection, err := openConnection(db)
	if err != nil {
		return CanonicalMutationResult{}, err
	}
	defer connection.Close()
	native := connection.native

	// The schema version is checked before anything else, exactly as
	// ScanCanonical checks it before reading a single node: a database this
	// code does not understand must fail loudly, not have a transaction
	// opened against it.
	metadata, err := scanGraphMetadata(ctx, native)
	if err != nil {
		return CanonicalMutationResult{}, &Error{Op: "apply canonical delta", Err: fmt.Errorf("read graph metadata: %w", err)}
	}
	if err := requireCanonicalSchemaVersion(metadata); err != nil {
		return CanonicalMutationResult{}, err
	}

	if err := queryWithDeadline(ctx, native, "BEGIN TRANSACTION"); err != nil {
		return CanonicalMutationResult{}, &Error{Op: "apply canonical delta", Err: fmt.Errorf("begin: %w", err)}
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		native.SetTimeout(0)
		rollbackResult, rollbackErr := native.Query("ROLLBACK")
		if rollbackResult != nil {
			rollbackResult.Close()
		}
		if rollbackErr != nil {
			returnErr = errors.Join(returnErr, &Error{Op: "apply canonical delta rollback", Err: rollbackErr})
		}
	}()

	retirement, err := retireCanonicalFiles(ctx, native, delta.ReplacedFiles, delta.RemovedFiles)
	if err != nil {
		return CanonicalMutationResult{}, wrapCanonicalMutationError("apply canonical delta", err)
	}

	upserted, err := applyCanonicalUpsert(ctx, native, delta.Upsert, options)
	if err != nil {
		return CanonicalMutationResult{}, wrapCanonicalMutationError("apply canonical delta", err)
	}

	if err := queryWithDeadline(ctx, native, "COMMIT"); err != nil {
		return CanonicalMutationResult{}, &Error{Op: "apply canonical delta", Err: fmt.Errorf("commit: %w", err)}
	}
	committed = true

	return CanonicalMutationResult{
		RemovedFiles:    retirement.RemovedFiles,
		RemovedSymbols:  retirement.RemovedSymbols,
		RemovedEvidence: retirement.RemovedEvidence,
		RemovedEdges:    retirement.RemovedEdges,
		UpsertedNodes:   upserted.UpsertedNodes,
		UpsertedEdges:   upserted.UpsertedEdges,
	}, nil
}

// requireCanonicalSchemaVersion mirrors canonicalScanSchemaVersion
// (canonical_scan_native.go) but wraps ErrInvalidCanonicalDelta instead of
// ErrInvalidCanonicalScan: the two read the identical GraphMetadata shape,
// for two different callers that must each report their own sentinel.
func requireCanonicalSchemaVersion(metadata map[string]string) error {
	stored, present := metadata["schema_version"]
	if !present {
		return fmt.Errorf("%w: GraphMetadata has no schema_version", ErrInvalidCanonicalDelta)
	}
	version, err := strconv.Atoi(stored)
	if err != nil {
		return fmt.Errorf("%w: schema_version %q is not numeric: %v", ErrInvalidCanonicalDelta, stored, err)
	}
	if version != CanonicalSchemaVersion {
		return fmt.Errorf("%w: schema_version %d, want %d", ErrInvalidCanonicalDelta, version, CanonicalSchemaVersion)
	}
	return nil
}

// retireCanonicalFiles withdraws everything replacedFiles and removedFiles
// asserted: their symbols, their evidence, their unresolved references, and
// every edge anchored on any of those nodes, incoming and outgoing, across
// every canonical relationship table. removedFiles additionally lose their
// own File node and its CONTAINS_FILE edge.
//
// replacedFiles keep their File node exactly as it is here: there is
// nothing File-specific to retire for a replace, because CONTAINS_FILE's
// ONE_MANY multiplicity means a File row that persists never has more than
// the single CONTAINS_FILE edge it already had, and applyCanonicalUpsert's
// generic node upsert rewrites the File row in place if Upsert.Files brings
// one for the same key, leaving it untouched otherwise.
func retireCanonicalFiles(ctx context.Context, native *lbug.Connection, replacedFiles, removedFiles []string) (CanonicalMutationResult, error) {
	var result CanonicalMutationResult
	retired := make([]string, 0, len(replacedFiles)+len(removedFiles))
	retired = append(retired, replacedFiles...)
	retired = append(retired, removedFiles...)
	if len(retired) == 0 {
		return result, nil
	}

	symbolKeys, err := canonicalKeyRows(ctx, native, canonicalRetiredSymbolsQuery, map[string]any{"files": retired})
	if err != nil {
		return CanonicalMutationResult{}, fmt.Errorf("find retired symbols: %w", err)
	}
	evidenceKeys, err := canonicalKeyRows(ctx, native, canonicalRetiredEvidenceQuery, map[string]any{"files": retired})
	if err != nil {
		return CanonicalMutationResult{}, fmt.Errorf("find retired evidence: %w", err)
	}
	unresolvedKeys, err := canonicalKeyRows(ctx, native, canonicalRetiredUnresolvedQuery, map[string]any{"files": retired})
	if err != nil {
		return CanonicalMutationResult{}, fmt.Errorf("find retired unresolved references: %w", err)
	}

	// Every edge touching a withdrawn symbol/evidence/unresolved reference
	// is removed before the node itself: LadybugDB, like the old synthetic
	// schema's writer, requires a node's edges gone before the node can be
	// deleted.
	for _, keys := range [][]string{symbolKeys, evidenceKeys, unresolvedKeys} {
		count, err := deleteCanonicalEdgesTouching(ctx, native, keys)
		if err != nil {
			return CanonicalMutationResult{}, fmt.Errorf("retire anchored edges: %w", err)
		}
		result.RemovedEdges += count
	}

	// An edge whose evidence is being retired goes with it even when both of
	// its endpoints survive: see deleteCanonicalEdgesEvidencedBy.
	evidencedCount, err := deleteCanonicalEdgesEvidencedBy(ctx, native, evidenceKeys)
	if err != nil {
		return CanonicalMutationResult{}, fmt.Errorf("retire evidenced edges: %w", err)
	}
	result.RemovedEdges += evidencedCount

	if len(symbolKeys) > 0 {
		count, err := canonicalMutationCount(ctx, native, deleteCanonicalSymbolsQuery, map[string]any{"keys": symbolKeys})
		if err != nil {
			return CanonicalMutationResult{}, fmt.Errorf("delete retired symbols: %w", err)
		}
		result.RemovedSymbols = count
	}
	if len(evidenceKeys) > 0 {
		count, err := canonicalMutationCount(ctx, native, deleteCanonicalEvidenceQuery, map[string]any{"keys": evidenceKeys})
		if err != nil {
			return CanonicalMutationResult{}, fmt.Errorf("delete retired evidence: %w", err)
		}
		result.RemovedEvidence = count
	}
	if len(unresolvedKeys) > 0 {
		if _, err := canonicalMutationCount(ctx, native, deleteCanonicalUnresolvedQuery, map[string]any{"keys": unresolvedKeys}); err != nil {
			return CanonicalMutationResult{}, fmt.Errorf("delete retired unresolved references: %w", err)
		}
	}

	if len(removedFiles) > 0 {
		// The File node's own remaining edge, CONTAINS_FILE, is swept the
		// same generic way: deleteCanonicalEdgesTouching does not care that
		// this is the only relationship table where removedFiles is a
		// target rather than a source.
		count, err := deleteCanonicalEdgesTouching(ctx, native, removedFiles)
		if err != nil {
			return CanonicalMutationResult{}, fmt.Errorf("retire removed file edges: %w", err)
		}
		result.RemovedEdges += count

		count, err = canonicalMutationCount(ctx, native, deleteCanonicalFilesQuery, map[string]any{"keys": removedFiles})
		if err != nil {
			return CanonicalMutationResult{}, fmt.Errorf("delete removed files: %w", err)
		}
		result.RemovedFiles = count
	}

	return result, nil
}

// deleteCanonicalEdgesTouching removes every relationship row of every
// canonical table -- derived from CanonicalRelationshipTables, never a hand
// written subset -- whose source or target stable_key is in keys, in either
// direction. This is retirement's cascade: whatever anchors on a withdrawn
// node, incoming or outgoing, on any of the eighteen EdgeKind tables plus
// OBSERVED_IN and REPORTS_UNRESOLVED, is exactly the ghost edge LUQUE-1007's
// "0 ghost edges" gate forbids leaving behind -- including an edge from
// another repository (design decision 2), which this makes no attempt to
// distinguish from any other source.
func deleteCanonicalEdgesTouching(ctx context.Context, native *lbug.Connection, keys []string) (int64, error) {
	if len(keys) == 0 {
		return 0, nil
	}
	var total int64
	for _, table := range CanonicalRelationshipTables() {
		query := fmt.Sprintf(`MATCH (source:%s)-[edge:%s]->(target:%s)
WHERE source.stable_key IN $keys OR target.stable_key IN $keys
DELETE edge
RETURN count(*)`, table.From, table.Name, table.To)
		count, err := canonicalMutationCount(ctx, native, query, map[string]any{"keys": keys})
		if err != nil {
			return 0, fmt.Errorf("delete %s: %w", table.Name, err)
		}
		total += count
	}
	return total, nil
}

// deleteCanonicalEdgesEvidencedBy withdraws every edge asserted by evidence
// that is being retired, even when neither of its endpoints is.
//
// PACKAGE_DEPENDS_ON and MODULE_DEPENDS_ON connect two Package nodes, which
// a file grained delta never retires, yet they are asserted by the file
// where the import was observed and carry that file's evidence. Deleting
// only edges that touch a retired node leaves them behind pointing at
// evidence that no longer exists -- a ghost edge, which the incremental
// gate forbids and `missing_evidence_file` catches. Anchoring them by their
// evidence is the same rule the model applies in facts.edgeAnchor.
func deleteCanonicalEdgesEvidencedBy(ctx context.Context, native *lbug.Connection, evidenceKeys []string) (int64, error) {
	if len(evidenceKeys) == 0 {
		return 0, nil
	}
	var total int64
	for _, table := range CanonicalRelationshipTables() {
		if !hasCanonicalProperty(table, "evidence_key") {
			continue
		}
		query := fmt.Sprintf(`MATCH (source:%s)-[edge:%s]->(target:%s)
WHERE edge.evidence_key IN $keys
DELETE edge
RETURN count(*)`, table.From, table.Name, table.To)
		count, err := canonicalMutationCount(ctx, native, query, map[string]any{"keys": evidenceKeys})
		if err != nil {
			return 0, fmt.Errorf("delete %s by evidence: %w", table.Name, err)
		}
		total += count
	}
	return total, nil
}

// hasCanonicalProperty reports whether a relationship table declares one
// property, so the sweep above is derived from the schema instead of a hand
// written list of the tables that carry evidence.
func hasCanonicalProperty(table SchemaRelationshipTable, name string) bool {
	for _, property := range table.Properties {
		if property.Name == name {
			return true
		}
	}
	return false
}

// applyCanonicalUpsert renders upsert through canonicalUpsertRows -- which
// is CanonicalTableRows itself, completed and filtered, never a second
// renderer -- and writes nodes before relationships, each in
// CanonicalNodeTables/CanonicalRelationshipTables order, exactly as
// LoadCanonical's own COPY sequence does and for the identical reason: no
// relationship write may run before the endpoints it connects exist.
func applyCanonicalUpsert(ctx context.Context, native *lbug.Connection, upsert facts.Set, options CanonicalLoadOptions) (CanonicalMutationResult, error) {
	var result CanonicalMutationResult
	tableRows, err := canonicalUpsertRows(upsert, options)
	if err != nil {
		return result, fmt.Errorf("%w: %v", ErrInvalidCanonicalDelta, err)
	}

	for _, table := range CanonicalNodeTables() {
		rows := tableRows[table.Name]
		if len(rows) == 0 {
			continue
		}
		columns, err := columnsOrError(table.Name)
		if err != nil {
			return result, err
		}
		types, _ := canonicalColumnTypes(table.Name)
		typedRows, err := canonicalTypedRows(columns, types, rows)
		if err != nil {
			return result, err
		}
		count, err := mergeCanonicalNodes(ctx, native, table, typedRows)
		if err != nil {
			return result, fmt.Errorf("upsert %s: %w", table.Name, err)
		}
		result.UpsertedNodes += count
	}

	for _, table := range CanonicalRelationshipTables() {
		rows := tableRows[table.Name]
		if len(rows) == 0 {
			continue
		}
		columns, err := columnsOrError(table.Name)
		if err != nil {
			return result, err
		}
		types, _ := canonicalColumnTypes(table.Name)
		typedRows, err := canonicalTypedRows(columns, types, rows)
		if err != nil {
			return result, err
		}

		if hasRelationshipProperty(table, "evidence_key") {
			if err := normalizeCanonicalEvidenceKeys(ctx, native, table.Name); err != nil {
				return result, fmt.Errorf("normalize %s: %w", table.Name, err)
			}
		}
		if err := requireCanonicalEndpoints(ctx, native, table, typedRows); err != nil {
			return result, err
		}

		count, err := mergeCanonicalRelationships(ctx, native, table, typedRows)
		if err != nil {
			return result, fmt.Errorf("upsert %s: %w", table.Name, err)
		}
		result.UpsertedEdges += count
	}

	return result, nil
}

// mergeCanonicalNodes upserts one node table's rows by primary key: a row
// that already exists is updated in place, never duplicated and never
// rejected (verified directly against the pinned v0.13.1 engine: UNWIND
// $rows AS row MERGE (n:T {pk: row.pk}) ON CREATE SET ... ON MATCH SET ...
// RETURN count(*) returns len(rows) regardless of how many of those rows
// already existed -- MERGE, unlike a preceding MATCH, never filters a row
// out of the UNWIND for want of a match, it creates one instead).
func mergeCanonicalNodes(ctx context.Context, native *lbug.Connection, table SchemaNodeTable, rows []map[string]any) (int64, error) {
	assignments := make([]string, 0, len(table.Properties))
	for _, property := range table.Properties {
		assignments = append(assignments, fmt.Sprintf("node.%s = row.%s", property.Name, property.Name))
	}
	query := fmt.Sprintf("UNWIND $rows AS row\nMERGE (node:%s {%s: row.%s})", table.Name, table.PrimaryKey.Name, table.PrimaryKey.Name)
	if len(assignments) > 0 {
		setClause := strings.Join(assignments, ", ")
		query += fmt.Sprintf("\nON CREATE SET %s\nON MATCH SET %s", setClause, setClause)
	}
	query += "\nRETURN count(*)"
	return canonicalMutationCount(ctx, native, query, map[string]any{"rows": rows})
}

// mergeCanonicalRelationships upserts one relationship table's rows.
//
// Structural tables (no evidence_key: CONTAINS_PACKAGE, CONTAINS_FILE,
// DEFINES) MERGE on endpoints alone, which their ONE_MANY/MANY_ONE
// multiplicity already limits to a single edge per constrained endpoint, so
// restating the same containment updates it in place. Semantic tables (the
// fifteen that carry evidence_key) additionally key the MERGE on
// evidence_key, so two distinct occurrences of the same kind between the
// same two symbols -- two different call sites, each with its own evidence
// -- stay two edges, while restating the exact same occurrence updates it
// in place instead of duplicating it. OBSERVED_IN and REPORTS_UNRESOLVED
// carry no properties at all: their MERGE has nothing to SET, on endpoints
// their owning Evidence/UnresolvedReference already makes unique.
func mergeCanonicalRelationships(ctx context.Context, native *lbug.Connection, table SchemaRelationshipTable, rows []map[string]any) (int64, error) {
	mergeProperties := ""
	assignments := make([]string, 0, len(table.Properties))
	for _, property := range table.Properties {
		if property.Name == "evidence_key" {
			mergeProperties = " {evidence_key: row.evidence_key}"
			continue
		}
		assignments = append(assignments, fmt.Sprintf("edge.%s = row.%s", property.Name, property.Name))
	}

	query := fmt.Sprintf("UNWIND $rows AS row\nMATCH (source:%s {stable_key: row.from}), (target:%s {stable_key: row.to})\nMERGE (source)-[edge:%s%s]->(target)",
		table.From, table.To, table.Name, mergeProperties)
	if len(assignments) > 0 {
		setClause := strings.Join(assignments, ", ")
		query += fmt.Sprintf("\nON CREATE SET %s\nON MATCH SET %s", setClause, setClause)
	}
	query += "\nRETURN count(*)"
	return canonicalMutationCount(ctx, native, query, map[string]any{"rows": rows})
}

// requireCanonicalEndpoints rejects, before mergeCanonicalRelationships
// runs, any row of table whose source or target stable_key does not
// already exist in the database. Without this check the MATCH inside
// mergeCanonicalRelationships would simply drop that one row out of the
// UNWIND -- verified directly against the pinned v0.13.1 engine: a
// prepared UNWIND+MATCH+MERGE batch containing one row with a
// non-existent endpoint silently returns a count one short of len(rows),
// no error at all -- which is exactly the silently-omitted edge design
// decision 3 (and the contract this function implements) forbids. Node
// upsert always runs before this in applyCanonicalUpsert, so an endpoint
// Upsert itself creates already exists by the time this checks for it.
func requireCanonicalEndpoints(ctx context.Context, native *lbug.Connection, table SchemaRelationshipTable, rows []map[string]any) error {
	missingFrom, err := missingCanonicalKeys(ctx, native, table.From, distinctCanonicalStrings(rows, "from"))
	if err != nil {
		return fmt.Errorf("check %s sources: %w", table.Name, err)
	}
	missingTo, err := missingCanonicalKeys(ctx, native, table.To, distinctCanonicalStrings(rows, "to"))
	if err != nil {
		return fmt.Errorf("check %s targets: %w", table.Name, err)
	}
	if len(missingFrom) == 0 && len(missingTo) == 0 {
		return nil
	}

	missingFromSet := make(map[string]struct{}, len(missingFrom))
	for _, key := range missingFrom {
		missingFromSet[key] = struct{}{}
	}
	missingToSet := make(map[string]struct{}, len(missingTo))
	for _, key := range missingTo {
		missingToSet[key] = struct{}{}
	}
	for _, row := range rows {
		from, _ := row["from"].(string)
		to, _ := row["to"].(string)
		if _, bad := missingFromSet[from]; bad {
			return fmt.Errorf("%w: %s edge %s -> %s has no source %s %q", ErrInvalidCanonicalDelta, table.Name, from, to, table.From, from)
		}
		if _, bad := missingToSet[to]; bad {
			return fmt.Errorf("%w: %s edge %s -> %s has no target %s %q", ErrInvalidCanonicalDelta, table.Name, from, to, table.To, to)
		}
	}
	return fmt.Errorf("%w: %s has a row with a missing endpoint", ErrInvalidCanonicalDelta, table.Name)
}

// missingCanonicalKeys returns the subset of keys that nodeTable does not
// contain a stable_key for.
func missingCanonicalKeys(ctx context.Context, native *lbug.Connection, nodeTable string, keys []string) ([]string, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	query := fmt.Sprintf(`UNWIND $keys AS key
MATCH (node:%s) WHERE node.stable_key = key
RETURN key`, nodeTable)
	found, err := canonicalKeyRows(ctx, native, query, map[string]any{"keys": keys})
	if err != nil {
		return nil, err
	}
	foundSet := make(map[string]struct{}, len(found))
	for _, key := range found {
		foundSet[key] = struct{}{}
	}
	missing := make([]string, 0)
	for _, key := range keys {
		if _, exists := foundSet[key]; !exists {
			missing = append(missing, key)
		}
	}
	return missing, nil
}

// normalizeCanonicalEvidenceKeys rewrites any NULL evidence_key on table to
// an empty string.
//
// LoadCanonical's CSV COPY path stores an empty CSV field as NULL
// (canonical_scan_native.go's tupleOptionalString documents this verified
// engine behaviour), but mergeCanonicalRelationships binds evidence_key as
// a literal Go empty string through Cypher parameters, and MERGE's
// property pattern matches by strict equality, where NULL never equals an
// empty string -- verified directly: merging on an empty evidence_key
// parameter against a CSV-loaded row whose evidence_key was NULL created a
// second, parallel edge instead of updating the first. Normalizing a table
// before merging into it makes every future comparison on that table land
// on the same empty string, regardless of which path originally wrote the
// row, restoring the upsert idempotency design decision 3 requires. It is
// cheap: it only ever runs for a table this delta's Upsert actually has
// rows for, and is a no-op once a table has been normalized once.
func normalizeCanonicalEvidenceKeys(ctx context.Context, native *lbug.Connection, table string) error {
	query := fmt.Sprintf(`MATCH ()-[edge:%s]->() WHERE edge.evidence_key IS NULL SET edge.evidence_key = ''`, table)
	return queryWithDeadline(ctx, native, query)
}

// canonicalKeyRows runs a prepared query expected to return one STRING
// column per row and collects that column across every row -- the shape
// every key discovery and endpoint verification query in this file uses.
func canonicalKeyRows(ctx context.Context, native *lbug.Connection, query string, args map[string]any) ([]string, error) {
	var keys []string
	err := executeCanonicalMutation(ctx, native, query, args, func(result *lbug.QueryResult) error {
		keys = make([]string, 0)
		for result.HasNext() {
			tuple, err := nextTuple(result)
			if err != nil {
				return err
			}
			key, err := tupleString(tuple, 0)
			tuple.Close()
			if err != nil {
				return err
			}
			keys = append(keys, key)
		}
		return nil
	})
	return keys, err
}

// canonicalMutationCount runs a prepared query expected to return a single
// count(*) row and decodes it -- the same shape queryCount
// (canonical_load_native.go) and writer.executeCount (mutation_native.go)
// use, adapted to take parameters neither of those needs.
func canonicalMutationCount(ctx context.Context, native *lbug.Connection, query string, args map[string]any) (int64, error) {
	var count int64
	err := executeCanonicalMutation(ctx, native, query, args, func(result *lbug.QueryResult) error {
		if !result.HasNext() {
			return errors.New("canonical mutation query returned no count")
		}
		tuple, err := nextTuple(result)
		if err != nil {
			return err
		}
		defer tuple.Close()
		value, err := tupleInt64(tuple, 0)
		if err != nil {
			return err
		}
		count = value
		return nil
	})
	return count, err
}

// executeCanonicalMutation prepares query, executes it with args under
// ctx's deadline, and hands the live result to decode. It is the
// parameterized counterpart of queryWithDeadline/queryCount: those run a
// literal query string with native.Query, this runs a prepared statement
// with native.Execute so args can bind list parameters ($keys, $files,
// $rows) neither of those two ever needs.
func executeCanonicalMutation(ctx context.Context, native *lbug.Connection, query string, args map[string]any, decode func(*lbug.QueryResult) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	statement, err := native.Prepare(query)
	if err != nil {
		return err
	}
	defer statement.Close()
	if err := setQueryDeadline(native, ctx); err != nil {
		return err
	}
	defer native.SetTimeout(0)
	result, err := native.Execute(statement, args)
	if err != nil {
		if result != nil {
			result.Close()
		}
		return err
	}
	defer result.Close()
	if err := decode(result); err != nil {
		return err
	}
	return ctx.Err()
}

// distinctCanonicalStrings collects the distinct values of column across
// rows, in first-seen order.
func distinctCanonicalStrings(rows []map[string]any, column string) []string {
	seen := make(map[string]struct{}, len(rows))
	values := make([]string, 0, len(rows))
	for _, row := range rows {
		value, _ := row[column].(string)
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}

// wrapCanonicalMutationError wraps a retirement/upsert failure in *Error,
// the way every native engine failure in this package surfaces -- except a
// delta content failure (ErrInvalidCanonicalDelta), which is a caller
// mistake, not an engine failure, and so is returned exactly as the rest of
// this package already treats ErrInvalidCanonicalLoad/ErrInvalidCanonicalScan:
// never wrapped in *Error.
func wrapCanonicalMutationError(op string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInvalidCanonicalDelta) {
		return err
	}
	return &Error{Op: op, Err: err}
}

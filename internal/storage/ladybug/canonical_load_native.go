//go:build ladybug && cgo

package ladybug

import (
	"bufio"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/Luqueee/ladygraph/internal/facts"
)

// canonicalCopyOptions is the CSV dialect every canonical COPY must use.
//
// writeCanonicalCSV emits RFC 4180: fields are quoted with `"` and an embedded
// quote is doubled. The pinned engine (v0.13.1) defaults to `ESCAPE='\'`, so a
// doubled quote desynchronises its parser and a later row fails with
// "expected N values per row, but got more"; and its parallel reader rejects
// quoted newlines outright. Canonical text — Evidence.text, Symbol.signature —
// legitimately contains commas, quotes and newlines, so the dialect is stated
// explicitly instead of relying on defaults.
const canonicalCopyOptions = `(HEADER=true, PARALLEL=false, QUOTE='"', ESCAPE='"')`

// LoadReport records what a canonical load wrote.
type LoadReport struct {
	Tables    map[string]int64
	Nodes     int64
	Edges     int64
	StagingMS float64
	CopyMS    float64
}

// CanonicalProbe is one read executed against a freshly built graph.
type CanonicalProbe struct {
	Name      string
	SymbolKey string
	TargetKey string
	EdgeTable string
	MinRows   int64
}

// CanonicalProbeResult is the outcome of one probe.
type CanonicalProbeResult struct {
	Probe  string
	Rows   int64
	Passed bool
	Detail string
}

// LoadCanonical creates the database at path, applies the canonical schema and
// bulk loads the fact set. The path must not exist yet.
func LoadCanonical(ctx context.Context, path string, set facts.Set, options CanonicalLoadOptions) (report LoadReport, returnErr error) {
	if err := validatePath(path); err != nil {
		return LoadReport{}, err
	}
	if err := ctx.Err(); err != nil {
		return LoadReport{}, &Error{Op: "load canonical", Err: err}
	}
	if _, statErr := os.Stat(path); statErr == nil {
		return LoadReport{}, &Error{Op: "load canonical", Err: fmt.Errorf("%w: %s", ErrAlreadyExists, path)}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return LoadReport{}, &Error{Op: "load canonical", Err: statErr}
	}

	// Rows are rendered and staged to CSV before the database is touched, so a
	// problem with the fact set itself (an empty resolver version, a dangling
	// edge, ...) never leaves so much as an empty file at path.
	stagingStart := time.Now()
	tableRows, err := CanonicalTableRows(set, options)
	if err != nil {
		return LoadReport{}, err
	}
	stagingDir, err := os.MkdirTemp("", "ladygraph-ladybug-canonical-staging-*")
	if err != nil {
		return LoadReport{}, &Error{Op: "load canonical", Err: err}
	}
	defer os.RemoveAll(stagingDir)

	staged, err := stageCanonicalCSVs(stagingDir, tableRows)
	if err != nil {
		return LoadReport{}, err
	}
	report.StagingMS = millisecondsSince(stagingStart)

	// From here on the candidate file exists on disk: every remaining return
	// path runs through this cleanup so a failure never leaves a half-built
	// database behind, only either the finished graph or nothing at all.
	created := false
	defer func() {
		if created && returnErr != nil {
			_ = os.RemoveAll(path)
		}
	}()

	db, err := openCanonicalDatabase(ctx, path, false)
	if err != nil {
		return LoadReport{}, err
	}
	created = true
	defer db.Close()

	connection, err := openConnection(db)
	if err != nil {
		return LoadReport{}, err
	}
	defer connection.Close()

	for _, statement := range CanonicalSchemaStatements() {
		if err := queryWithDeadline(ctx, connection.native, statement); err != nil {
			return LoadReport{}, &Error{Op: "load canonical", Err: fmt.Errorf("apply canonical schema: %w", err)}
		}
	}

	// Nodes load before relationships, in schema declaration order, so no
	// relationship COPY ever runs before the endpoints it references exist.
	//
	// canonicalCopyOptions is not tuning: writeCanonicalCSV emits RFC 4180,
	// and the engine's defaults do not read it back. See its declaration.
	copyStart := time.Now()
	report.Tables = make(map[string]int64, len(staged))
	for _, table := range staged {
		query := fmt.Sprintf("COPY %s FROM %s %s", table.name, cypherString(table.csvPath), canonicalCopyOptions)
		if err := queryWithDeadline(ctx, connection.native, query); err != nil {
			return LoadReport{}, &Error{Op: "load canonical", Err: fmt.Errorf("copy %s: %w", table.name, err)}
		}
		report.Tables[table.name] = table.rows
		if table.isNode {
			report.Nodes += table.rows
		} else {
			report.Edges += table.rows
		}
	}
	report.CopyMS = millisecondsSince(copyStart)

	if err := ctx.Err(); err != nil {
		return LoadReport{}, err
	}
	return report, nil
}

// CanonicalTableCounts reads the row count of every canonical table.
func CanonicalTableCounts(ctx context.Context, path string) (map[string]int64, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, &Error{Op: "canonical table counts", Err: err}
	}

	db, err := openCanonicalDatabase(ctx, path, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	connection, err := openConnection(db)
	if err != nil {
		return nil, err
	}
	defer connection.Close()

	nodeNames := make(map[string]struct{}, len(CanonicalNodeTables()))
	for _, table := range CanonicalNodeTables() {
		nodeNames[table.Name] = struct{}{}
	}

	names := CanonicalTableNames()
	counts := make(map[string]int64, len(names))
	for _, name := range names {
		query := relationshipCountQuery(name)
		if _, isNode := nodeNames[name]; isNode {
			query = nodeCountQuery(name)
		}
		count, err := queryCount(ctx, connection.native, query)
		if err != nil {
			return nil, &Error{Op: "canonical table counts", Err: fmt.Errorf("count %s: %w", name, err)}
		}
		counts[name] = count
	}
	if err := ctx.Err(); err != nil {
		return nil, &Error{Op: "canonical table counts", Err: err}
	}
	return counts, nil
}

// RunCanonicalProbes executes one real read per probe against the canonical
// schema. A probe that reads fewer than MinRows is a reported failure, not a
// Go error; only the query engine itself can abort the whole run.
func RunCanonicalProbes(ctx context.Context, path string, probes []CanonicalProbe) ([]CanonicalProbeResult, error) {
	if err := validatePath(path); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, &Error{Op: "run canonical probes", Err: err}
	}
	if len(probes) == 0 {
		return nil, nil
	}

	db, err := openCanonicalDatabase(ctx, path, true)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	connection, err := openConnection(db)
	if err != nil {
		return nil, err
	}
	defer connection.Close()

	results := make([]CanonicalProbeResult, 0, len(probes))
	for _, probe := range probes {
		query, description, buildErr := canonicalProbeQuery(probe)
		if buildErr != nil {
			return nil, &Error{Op: "run canonical probes", Err: fmt.Errorf("probe %s: %w", probe.Name, buildErr)}
		}
		rows, err := queryCount(ctx, connection.native, query)
		if err != nil {
			return nil, &Error{Op: "run canonical probes", Err: fmt.Errorf("probe %s: %w", probe.Name, err)}
		}
		results = append(results, CanonicalProbeResult{
			Probe:  probe.Name,
			Rows:   rows,
			Passed: rows >= probe.MinRows,
			Detail: fmt.Sprintf("%s: rows=%d want>=%d", description, rows, probe.MinRows),
		})
	}
	if err := ctx.Err(); err != nil {
		return nil, &Error{Op: "run canonical probes", Err: err}
	}
	return results, nil
}

// canonicalStagedTable is one table written to CSV and ready for COPY.
type canonicalStagedTable struct {
	name    string
	csvPath string
	rows    int64
	isNode  bool
}

// stageCanonicalCSVs writes one CSV per non-empty table, node tables first in
// CanonicalNodeTables order and relationships after in CanonicalRelationshipTables
// order. LoadCanonical copies in this same order, which is what keeps a
// relationship from ever loading before the nodes it connects.
func stageCanonicalCSVs(stagingDir string, tableRows map[string][][]string) ([]canonicalStagedTable, error) {
	nodeTables := CanonicalNodeTables()
	relationshipTables := CanonicalRelationshipTables()
	nodeNames := make(map[string]struct{}, len(nodeTables))
	order := make([]string, 0, len(nodeTables)+len(relationshipTables))
	for _, table := range nodeTables {
		nodeNames[table.Name] = struct{}{}
		order = append(order, table.Name)
	}
	for _, table := range relationshipTables {
		order = append(order, table.Name)
	}

	staged := make([]canonicalStagedTable, 0, len(tableRows))
	for _, name := range order {
		rows, exists := tableRows[name]
		if !exists || len(rows) == 0 {
			continue
		}
		columns, exists := CanonicalColumns(name)
		if !exists {
			return nil, fmt.Errorf("%w: table %q has no declared column order", ErrInvalidCanonicalLoad, name)
		}
		csvPath := filepath.Join(stagingDir, name+".csv")
		if err := writeCanonicalCSV(csvPath, columns, rows); err != nil {
			return nil, &Error{Op: "load canonical", Err: fmt.Errorf("stage %s: %w", name, err)}
		}
		_, isNode := nodeNames[name]
		staged = append(staged, canonicalStagedTable{name: name, csvPath: csvPath, rows: int64(len(rows)), isNode: isNode})
	}
	return staged, nil
}

// writeCanonicalCSV writes a table's rows behind a header row, quoting fields
// per RFC 4180. The pinned engine (v0.13.1) was verified directly to accept
// `COPY ... (HEADER=true)` against a headered CSV, and to require
// `PARALLEL=false` whenever a quoted field contains a newline; see the COPY
// call sites.
func writeCanonicalCSV(path string, header []string, rows [][]string) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	// csv.Writer buffers at 4 KiB, which is one write syscall every few
	// dozen rows: a canonical load stages tens of megabytes.
	buffered := bufio.NewWriterSize(file, 1<<20)
	writer := csv.NewWriter(buffered)
	if err := writer.Write(header); err != nil {
		_ = file.Close()
		return err
	}
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			_ = file.Close()
			return err
		}
	}
	writer.Flush()
	if err := errors.Join(writer.Error(), buffered.Flush()); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}

func millisecondsSince(start time.Time) float64 {
	return float64(time.Since(start)) / float64(time.Millisecond)
}

// openCanonicalDatabase opens path through the package's own Open and hands
// back the concrete type: the Database interface exposes no way to reach a
// raw connection, and DDL and COPY need exactly that.
func openCanonicalDatabase(ctx context.Context, path string, readOnly bool) (*database, error) {
	config := DefaultConfig()
	config.ReadOnly = readOnly
	opened, err := Open(ctx, path, config)
	if err != nil {
		return nil, err
	}
	db, ok := opened.(*database)
	if !ok {
		_ = opened.Close()
		return nil, fmt.Errorf("%w: unexpected database implementation %T", ErrInvalidCanonicalLoad, opened)
	}
	return db, nil
}

func nodeCountQuery(table string) string {
	return fmt.Sprintf("MATCH (node:%s) RETURN count(*)", table)
}

func relationshipCountQuery(table string) string {
	return fmt.Sprintf("MATCH ()-[edge:%s]->() RETURN count(*)", table)
}

// queryCount runs one count(*) query to completion and decodes its single
// scalar result, the same shape every diagnostic and mutation count query in
// this package already returns.
func queryCount(ctx context.Context, connection *lbug.Connection, query string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if err := setQueryDeadline(connection, ctx); err != nil {
		return 0, err
	}
	defer connection.SetTimeout(0)
	result, err := connection.Query(query)
	if err != nil {
		if result != nil {
			result.Close()
		}
		return 0, err
	}
	defer result.Close()
	if !result.HasNext() {
		return 0, errors.New("canonical count query returned no rows")
	}
	tuple, err := nextTuple(result)
	if err != nil {
		return 0, err
	}
	defer tuple.Close()
	return tupleInt64(tuple, 0)
}

// canonicalProbeQuery renders one CanonicalProbe into the Cypher its shape
// implies: an outgoing edge count from a symbol, an edge count between two
// symbols, or a plain existence check when no edge table is given at all.
func canonicalProbeQuery(probe CanonicalProbe) (query, description string, err error) {
	if probe.SymbolKey == "" {
		return "", "", fmt.Errorf("%w: probe requires a symbol key", ErrInvalidCanonicalLoad)
	}
	switch {
	case probe.EdgeTable != "" && probe.TargetKey != "":
		query = fmt.Sprintf(
			"MATCH (source:Symbol)-[edge:%s]->(target:Symbol) WHERE source.stable_key = %s AND target.stable_key = %s RETURN count(*)",
			probe.EdgeTable, cypherString(probe.SymbolKey), cypherString(probe.TargetKey),
		)
		description = fmt.Sprintf("%s from %s to %s", probe.EdgeTable, probe.SymbolKey, probe.TargetKey)
	case probe.EdgeTable != "":
		query = fmt.Sprintf(
			"MATCH (source:Symbol)-[edge:%s]->() WHERE source.stable_key = %s RETURN count(*)",
			probe.EdgeTable, cypherString(probe.SymbolKey),
		)
		description = fmt.Sprintf("outgoing %s from %s", probe.EdgeTable, probe.SymbolKey)
	default:
		query = fmt.Sprintf("MATCH (symbol:Symbol) WHERE symbol.stable_key = %s RETURN count(*)", cypherString(probe.SymbolKey))
		description = fmt.Sprintf("symbol %s exists", probe.SymbolKey)
	}
	return query, description, nil
}

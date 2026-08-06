//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"
)

type requiredDiagnosticTable struct {
	name       string
	tableType  string
	properties []requiredDiagnosticProperty
	source     string
	target     string
}

type requiredDiagnosticProperty struct {
	name       string
	valueType  string
	primaryKey bool
}

// requiredDiagnosticTables is the frozen 001-synthetic schema, written by
// hand because nothing in this codebase generates it anymore; see
// canonicalRequiredDiagnosticTables for the canonical schema's derived
// equivalent.
var requiredDiagnosticTables = []requiredDiagnosticTable{
	{name: "Repository", tableType: "NODE", properties: []requiredDiagnosticProperty{
		{name: "stable_key", valueType: "STRING", primaryKey: true},
		{name: "name", valueType: "STRING"}, {name: "path", valueType: "STRING"}, {name: "language", valueType: "STRING"},
	}},
	{name: "File", tableType: "NODE", properties: []requiredDiagnosticProperty{
		{name: "stable_key", valueType: "STRING", primaryKey: true}, {name: "repository_key", valueType: "STRING"},
		{name: "path", valueType: "STRING"}, {name: "content_hash", valueType: "STRING"}, {name: "language", valueType: "STRING"},
	}},
	{name: "Symbol", tableType: "NODE", properties: []requiredDiagnosticProperty{
		{name: "stable_key", valueType: "STRING", primaryKey: true}, {name: "repository_key", valueType: "STRING"},
		{name: "file_key", valueType: "STRING"}, {name: "name", valueType: "STRING"}, {name: "qualified_name", valueType: "STRING"},
		{name: "kind", valueType: "STRING"}, {name: "signature", valueType: "STRING"},
		{name: "start_line", valueType: "INT64"}, {name: "end_line", valueType: "INT64"},
	}},
	{name: "CONTAINS", tableType: "REL", source: "Repository", target: "File", properties: []requiredDiagnosticProperty{{name: "relation_kind", valueType: "STRING"}}},
	{name: "DEFINES", tableType: "REL", source: "File", target: "Symbol", properties: []requiredDiagnosticProperty{{name: "relation_kind", valueType: "STRING"}}},
	{name: "REFERENCES", tableType: "REL", source: "Symbol", target: "Symbol", properties: []requiredDiagnosticProperty{
		{name: "evidence_kind", valueType: "STRING"}, {name: "source_file_key", valueType: "STRING"}, {name: "target_file_key", valueType: "STRING"},
	}},
	{name: "CALLS_DIRECT", tableType: "REL", source: "Symbol", target: "Symbol", properties: []requiredDiagnosticProperty{
		{name: "evidence_kind", valueType: "STRING"}, {name: "source_file_key", valueType: "STRING"}, {name: "target_file_key", valueType: "STRING"},
	}},
}

var diagnosticCountQueries = []struct {
	name  string
	query string
}{
	{name: "Repository", query: "MATCH (value:Repository) RETURN count(*)"},
	{name: "File", query: "MATCH (value:File) RETURN count(*)"},
	{name: "Symbol", query: "MATCH (value:Symbol) RETURN count(*)"},
	{name: "CONTAINS", query: "MATCH ()-[value:CONTAINS]->() RETURN count(*)"},
	{name: "DEFINES", query: "MATCH ()-[value:DEFINES]->() RETURN count(*)"},
	{name: "REFERENCES", query: "MATCH ()-[value:REFERENCES]->() RETURN count(*)"},
	{name: "CALLS_DIRECT", query: "MATCH ()-[value:CALLS_DIRECT]->() RETURN count(*)"},
}

var diagnosticIntegrityQueries = []struct {
	name  string
	query string
}{
	{name: "file_container_cardinality", query: "MATCH (file:File) OPTIONAL MATCH (:Repository)-[edge:CONTAINS]->(file) WITH file, count(edge) AS links WHERE links <> 1 RETURN count(*)"},
	{name: "file_repository_key", query: "MATCH (repository:Repository)-[:CONTAINS]->(file:File) WHERE repository.stable_key <> file.repository_key RETURN count(*)"},
	{name: "symbol_definition_cardinality", query: "MATCH (symbol:Symbol) OPTIONAL MATCH (:File)-[edge:DEFINES]->(symbol) WITH symbol, count(edge) AS links WHERE links <> 1 RETURN count(*)"},
	{name: "symbol_file_key", query: "MATCH (file:File)-[:DEFINES]->(symbol:Symbol) WHERE file.stable_key <> symbol.file_key RETURN count(*)"},
	{name: "references_file_keys", query: "MATCH (source:Symbol)-[edge:REFERENCES]->(target:Symbol) WHERE edge.source_file_key <> source.file_key OR edge.target_file_key <> target.file_key RETURN count(*)"},
	{name: "calls_direct_file_keys", query: "MATCH (source:Symbol)-[edge:CALLS_DIRECT]->(target:Symbol) WHERE edge.source_file_key <> source.file_key OR edge.target_file_key <> target.file_key RETURN count(*)"},
}

// DiagnoseStorage validates one existing LadybugDB database against
// whichever of the two schemas it actually has: canonical (see
// canonical_schema.go), or the frozen 001-synthetic layout. See
// detectSchemaKind for how that is decided, and StorageDiagnosis.Schema
// for how the result says which one it was.
func DiagnoseStorage(ctx context.Context, path string) (StorageDiagnosis, error) {
	if err := ctx.Err(); err != nil {
		return StorageDiagnosis{}, err
	}
	diagnosis, regular, err := newStorageDiagnosis(path)
	if err != nil {
		return StorageDiagnosis{}, err
	}
	if !regular {
		diagnosis.skipNativeChecks("database file is unavailable")
		return diagnosis, nil
	}

	lockPIDs, lockSupported, lockErr := externalStorageLocks(diagnosis.Path)
	switch {
	case lockErr != nil:
		diagnosis.addCheck("lock", DiagnosticFail, lockErr.Error())
	case !lockSupported:
		diagnosis.addCheck("lock", DiagnosticSkip, "external lock inspection is unsupported on this platform")
	case len(lockPIDs) > 0:
		diagnosis.addCheck("lock", DiagnosticFail, fmt.Sprintf("database lock held by external pids %v", lockPIDs))
	default:
		diagnosis.addCheck("lock", DiagnosticPass, "no external database lock detected")
	}

	configuration := lbug.DefaultSystemConfig()
	configuration.ReadOnly = true
	native, openErr := lbug.OpenDatabase(diagnosis.Path, configuration)
	if openErr != nil {
		diagnosis.addCheck("open", DiagnosticFail, openErr.Error())
		engineVersion, storageVersion := nativeVersion()
		diagnosis.EngineVersion = engineVersion
		diagnosis.StorageVersion = storageVersion
		diagnosis.addCheck("version", DiagnosticPass, versionDetail(diagnosis))
		for _, name := range []string{"schema", "transactions", "counts", "integrity"} {
			diagnosis.addCheck(name, DiagnosticSkip, "database did not open")
		}
		diagnosis.finalize()
		return diagnosis, nil
	}
	defer native.Close()
	diagnosis.addCheck("open", DiagnosticPass, "read-only open succeeded")

	engineVersion, storageVersion := nativeVersion()
	diagnosis.EngineVersion = engineVersion
	diagnosis.StorageVersion = storageVersion
	diagnosis.addCheck("version", DiagnosticPass, versionDetail(diagnosis))

	connection, err := lbug.OpenConnection(native)
	if err != nil {
		diagnosis.addCheck("schema", DiagnosticFail, fmt.Sprintf("open diagnostic connection: %v", err))
		for _, name := range []string{"transactions", "counts", "integrity"} {
			diagnosis.addCheck(name, DiagnosticSkip, "diagnostic connection did not open")
		}
		diagnosis.finalize()
		return diagnosis, nil
	}
	defer connection.Close()

	// canonical decides which of the two schemas the checks below (and
	// counts, and integrity) validate against; see detectSchemaKind.
	canonical, tableErr := detectSchemaKind(connection, &diagnosis)
	var schemaDetail string
	var schemaErr error
	switch {
	case tableErr != nil:
		schemaErr = tableErr
	case canonical:
		schemaDetail, schemaErr = diagnoseCanonicalSchema(connection, &diagnosis)
	default:
		schemaDetail, schemaErr = diagnoseSchema(connection, &diagnosis, requiredDiagnosticTables)
	}
	if schemaErr != nil {
		diagnosis.addCheck("schema", DiagnosticFail, schemaErr.Error())
	} else {
		diagnosis.addCheck("schema", DiagnosticPass, schemaDetail)
	}

	transactionDetail, transactionErr := diagnoseTransactions(ctx, diagnosis.Path)
	if transactionErr != nil {
		diagnosis.addCheck("transactions", DiagnosticFail, transactionErr.Error())
	} else {
		diagnosis.addCheck("transactions", DiagnosticPass, transactionDetail)
	}

	var countsDetail string
	var countsErr error
	if canonical {
		countsDetail, countsErr = diagnoseCanonicalCounts(connection, &diagnosis)
	} else {
		countsDetail, countsErr = diagnoseCounts(connection, &diagnosis)
	}
	if countsErr != nil {
		diagnosis.addCheck("counts", DiagnosticFail, countsErr.Error())
	} else {
		diagnosis.addCheck("counts", DiagnosticPass, countsDetail)
	}

	var integrityDetail string
	var integrityErr error
	if canonical {
		integrityDetail, integrityErr = diagnoseCanonicalCardinality(connection)
	} else {
		integrityDetail, integrityErr = diagnoseIntegrity(connection)
	}
	if integrityErr != nil {
		diagnosis.addCheck("integrity", DiagnosticFail, integrityErr.Error())
	} else {
		diagnosis.addCheck("integrity", DiagnosticPass, integrityDetail)
	}
	diagnosis.finalize()
	return diagnosis, nil
}

// detectSchemaKind lists every table once via show_tables(), recording
// them on diagnosis, and reports which of the two schemas doctor storage
// knows how to validate is present. The canonical schema is identified by
// its GraphMetadata node table (LoadCanonical and `luque rebuild` always
// write one); its absence means the frozen 001-synthetic layout the
// ladybug benchmarks still build against. diagnosis.Schema is set here,
// before either schema's checks run, so a check that later fails still
// says what it was measured against.
func detectSchemaKind(connection *lbug.Connection, diagnosis *StorageDiagnosis) (bool, error) {
	if err := loadDiagnosticTables(connection, diagnosis); err != nil {
		return false, err
	}
	if _, present := diagnosis.Tables["GraphMetadata"]; present {
		diagnosis.Schema = SchemaCanonical
		return true, nil
	}
	diagnosis.Schema = SchemaSynthetic
	return false, nil
}

// loadDiagnosticTables runs show_tables() once and records every table
// name and kind (NODE or REL) on diagnosis, ahead of validating either
// schema against it.
func loadDiagnosticTables(connection *lbug.Connection, diagnosis *StorageDiagnosis) error {
	rows, err := diagnosticRows(connection, "CALL show_tables() RETURN *")
	if err != nil {
		return err
	}
	for _, row := range rows {
		if len(row) < 3 {
			return fmt.Errorf("show_tables returned %d columns", len(row))
		}
		diagnosis.Tables[row[1]] = row[2]
	}
	return nil
}

// diagnoseSchema validates required against the tables loadDiagnosticTables
// already recorded on diagnosis.Tables: that every table exists with the
// right kind, that its properties (and, for a node table, its primary
// key) have the right type, and that a relationship table's declared
// endpoints match. The same validation engine drives both schemas doctor
// storage knows: requiredDiagnosticTables for the frozen 001-synthetic
// layout, and canonicalRequiredDiagnosticTables, derived fresh from
// canonical_schema.go, for the canonical one.
func diagnoseSchema(connection *lbug.Connection, diagnosis *StorageDiagnosis, required []requiredDiagnosticTable) (string, error) {
	var mismatches []string
	for _, table := range required {
		if actualType, found := diagnosis.Tables[table.name]; !found {
			mismatches = append(mismatches, fmt.Sprintf("missing table %s", table.name))
			continue
		} else if actualType != table.tableType {
			mismatches = append(mismatches, fmt.Sprintf("table %s type=%s want=%s", table.name, actualType, table.tableType))
			continue
		}
		properties, err := diagnosticRows(connection, fmt.Sprintf("CALL table_info('%s') RETURN *", table.name))
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("table_info %s: %v", table.name, err))
			continue
		}
		actualProperties := make(map[string]requiredDiagnosticProperty)
		for _, row := range properties {
			if len(row) < 3 {
				mismatches = append(mismatches, fmt.Sprintf("table_info %s returned %d columns", table.name, len(row)))
				continue
			}
			primary := len(row) >= 5 && strings.EqualFold(row[4], "true")
			actualProperties[row[1]] = requiredDiagnosticProperty{name: row[1], valueType: row[2], primaryKey: primary}
		}
		for _, property := range table.properties {
			actual, found := actualProperties[property.name]
			if !found {
				mismatches = append(mismatches, fmt.Sprintf("table %s missing property %s", table.name, property.name))
			} else if actual.valueType != property.valueType || actual.primaryKey != property.primaryKey {
				mismatches = append(mismatches, fmt.Sprintf("table %s property %s=(%s,pk=%t) want=(%s,pk=%t)", table.name, property.name, actual.valueType, actual.primaryKey, property.valueType, property.primaryKey))
			}
		}
		if table.tableType == "REL" {
			connectionRows, err := diagnosticRows(connection, fmt.Sprintf("CALL show_connection('%s') RETURN *", table.name))
			if err != nil || len(connectionRows) != 1 || len(connectionRows[0]) < 2 {
				mismatches = append(mismatches, fmt.Sprintf("table %s connection metadata is invalid: %v", table.name, err))
			} else if connectionRows[0][0] != table.source || connectionRows[0][1] != table.target {
				mismatches = append(mismatches, fmt.Sprintf("table %s endpoints=%s->%s want=%s->%s", table.name, connectionRows[0][0], connectionRows[0][1], table.source, table.target))
			}
		}
	}
	if len(mismatches) > 0 {
		return "", errors.New(strings.Join(mismatches, "; "))
	}
	tableNames := make([]string, 0, len(diagnosis.Tables))
	for name := range diagnosis.Tables {
		tableNames = append(tableNames, name)
	}
	sort.Strings(tableNames)
	return fmt.Sprintf("required schema present; tables=%s", strings.Join(tableNames, ",")), nil
}

// canonicalRequiredDiagnosticTables derives the full canonical schema as a
// diagnostic requirement list from CanonicalNodeTables and
// CanonicalRelationshipTables, so a table or property added there is
// checked automatically instead of drifting out of sync with a hand
// written copy — the risk requiredDiagnosticTables accepts on purpose,
// since nothing generates the frozen 001-synthetic schema anymore.
func canonicalRequiredDiagnosticTables() []requiredDiagnosticTable {
	nodes := CanonicalNodeTables()
	relationships := CanonicalRelationshipTables()
	required := make([]requiredDiagnosticTable, 0, len(nodes)+len(relationships))
	for _, table := range nodes {
		properties := make([]requiredDiagnosticProperty, 0, len(table.Properties)+1)
		properties = append(properties, requiredDiagnosticProperty{name: table.PrimaryKey.Name, valueType: table.PrimaryKey.Type, primaryKey: true})
		for _, property := range table.Properties {
			properties = append(properties, requiredDiagnosticProperty{name: property.Name, valueType: property.Type})
		}
		required = append(required, requiredDiagnosticTable{name: table.Name, tableType: "NODE", properties: properties})
	}
	for _, table := range relationships {
		properties := make([]requiredDiagnosticProperty, 0, len(table.Properties))
		for _, property := range table.Properties {
			properties = append(properties, requiredDiagnosticProperty{name: property.Name, valueType: property.Type})
		}
		required = append(required, requiredDiagnosticTable{name: table.Name, tableType: "REL", source: table.From, target: table.To, properties: properties})
	}
	return required
}

// diagnoseCanonicalSchema validates the canonical schema and then the
// schema_version GraphMetadata declares. A table or property mismatch is
// reported before the version is even read: an unreadable schema has no
// trustworthy version to report. A present but wrong version is always a
// FAIL, on purpose — luque never migrates a canonical database in place,
// so treating one as healthy would be reporting a compatibility promise
// this build does not keep.
func diagnoseCanonicalSchema(connection *lbug.Connection, diagnosis *StorageDiagnosis) (string, error) {
	detail, err := diagnoseSchema(connection, diagnosis, canonicalRequiredDiagnosticTables())
	if err != nil {
		return "", err
	}
	version, err := readGraphMetadataSchemaVersion(connection)
	if err != nil {
		return "", fmt.Errorf("read schema version: %w", err)
	}
	diagnosis.SchemaVersion = version
	if version != CanonicalSchemaVersion {
		return "", fmt.Errorf("GraphMetadata.schema_version=%d want=%d", version, CanonicalSchemaVersion)
	}
	return fmt.Sprintf("schema=canonical version=%d; %s", version, detail), nil
}

// readGraphMetadataSchemaVersion reads and parses the schema_version key
// GraphMetadata always carries in a canonical database. The parsed value
// is returned even when the caller will go on to reject it as the wrong
// version: only a read or parse failure is reported as this function's
// own error.
func readGraphMetadataSchemaVersion(connection *lbug.Connection) (int, error) {
	rows, err := diagnosticRows(connection, "MATCH (metadata:GraphMetadata) WHERE metadata.key = 'schema_version' RETURN metadata.value")
	if err != nil {
		return 0, err
	}
	if len(rows) != 1 || len(rows[0]) != 1 {
		return 0, fmt.Errorf("GraphMetadata.schema_version query returned %d rows", len(rows))
	}
	version, err := strconv.Atoi(rows[0][0])
	if err != nil {
		return 0, fmt.Errorf("GraphMetadata.schema_version %q is not numeric: %w", rows[0][0], err)
	}
	return version, nil
}

func diagnoseTransactions(ctx context.Context, sourcePath string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	directory, err := os.MkdirTemp("", "luque-doctor-transaction-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(directory)
	databasePath := filepath.Join(directory, "graph.db")
	if err := copyDiagnosticFile(sourcePath, databasePath); err != nil {
		return "", fmt.Errorf("copy database: %w", err)
	}
	native, err := lbug.OpenDatabase(databasePath, lbug.DefaultSystemConfig())
	if err != nil {
		return "", fmt.Errorf("open private copy: %w", err)
	}
	defer native.Close()
	connection, err := lbug.OpenConnection(native)
	if err != nil {
		return "", fmt.Errorf("open private connection: %w", err)
	}
	defer connection.Close()

	key := fmt.Sprintf("doctor-rollback-%d-%d", os.Getpid(), time.Now().UnixNano())
	if err := diagnosticExecute(connection, "BEGIN TRANSACTION"); err != nil {
		return "", fmt.Errorf("begin: %w", err)
	}
	rolledBack := false
	defer func() {
		if !rolledBack {
			_ = diagnosticExecute(connection, "ROLLBACK")
		}
	}()
	query := fmt.Sprintf(`CREATE (:Symbol {stable_key: '%s', repository_key: 'repository-0000', file_key: 'file-00000000', name: 'doctor_rollback', qualified_name: 'doctor.rollback', kind: 'function', signature: 'doctor_rollback()', start_line: 1, end_line: 1})`, key)
	if err := diagnosticExecute(connection, query); err != nil {
		return "", fmt.Errorf("transactional create: %w", err)
	}
	if err := diagnosticExecute(connection, "ROLLBACK"); err != nil {
		return "", fmt.Errorf("rollback: %w", err)
	}
	rolledBack = true
	count, err := diagnosticCount(connection, fmt.Sprintf("MATCH (symbol:Symbol) WHERE symbol.stable_key = '%s' RETURN count(*)", key))
	if err != nil {
		return "", fmt.Errorf("verify rollback: %w", err)
	}
	if count != 0 {
		return "", fmt.Errorf("rollback probe left %d symbols", count)
	}
	return "BEGIN, mutation, ROLLBACK and absence verification succeeded on a private copy", nil
}

func copyDiagnosticFile(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	target, err := os.OpenFile(targetPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(target, source); err != nil {
		_ = target.Close()
		return err
	}
	return target.Close()
}

func diagnoseCounts(connection *lbug.Connection, diagnosis *StorageDiagnosis) (string, error) {
	parts := make([]string, 0, len(diagnosticCountQueries))
	for _, item := range diagnosticCountQueries {
		count, err := diagnosticCount(connection, item.query)
		if err != nil {
			return "", fmt.Errorf("count %s: %w", item.name, err)
		}
		diagnosis.Counts[item.name] = count
		parts = append(parts, fmt.Sprintf("%s=%d", item.name, count))
	}
	return strings.Join(parts, " "), nil
}

// diagnoseCanonicalCounts counts every canonical table, including the
// ones a fact set leaves empty: CanonicalNodeTables and
// CanonicalRelationshipTables set the loop bound, not diagnosis.Tables,
// so a table LoadCanonical created but nothing populated still gets a
// zero entry instead of silently missing one.
func diagnoseCanonicalCounts(connection *lbug.Connection, diagnosis *StorageDiagnosis) (string, error) {
	nodes := CanonicalNodeTables()
	relationships := CanonicalRelationshipTables()
	parts := make([]string, 0, len(nodes)+len(relationships))
	for _, table := range nodes {
		count, err := diagnosticCount(connection, nodeCountQuery(table.Name))
		if err != nil {
			return "", fmt.Errorf("count %s: %w", table.Name, err)
		}
		diagnosis.Counts[table.Name] = count
		parts = append(parts, fmt.Sprintf("%s=%d", table.Name, count))
	}
	for _, table := range relationships {
		count, err := diagnosticCount(connection, relationshipCountQuery(table.Name))
		if err != nil {
			return "", fmt.Errorf("count %s: %w", table.Name, err)
		}
		diagnosis.Counts[table.Name] = count
		parts = append(parts, fmt.Sprintf("%s=%d", table.Name, count))
	}
	return strings.Join(parts, " "), nil
}

func diagnoseIntegrity(connection *lbug.Connection) (string, error) {
	parts := make([]string, 0, len(diagnosticIntegrityQueries))
	var violations int64
	for _, item := range diagnosticIntegrityQueries {
		count, err := diagnosticCount(connection, item.query)
		if err != nil {
			return "", fmt.Errorf("integrity %s: %w", item.name, err)
		}
		violations += count
		parts = append(parts, fmt.Sprintf("%s=%d", item.name, count))
	}
	if violations != 0 {
		return "", fmt.Errorf("%d integrity violations: %s", violations, strings.Join(parts, " "))
	}
	return "0 violations; " + strings.Join(parts, " "), nil
}

// canonicalCardinalityQueries checks the structural spine LoadCanonical
// and `luque rebuild` both depend on: Repository -CONTAINS_PACKAGE->
// Package -CONTAINS_FILE-> File -DEFINES-> Symbol, each hop ONE_MANY per
// canonical_schema.go, plus every repository_key/package_key/file_key
// column on Package/File/Symbol against the value that chain implies.
//
// This intentionally does not call into VerifyCanonicalIntegrity, even
// though RuleInvalidRepositoryOwner also compares a node's repository_key
// against its containment chain: that rule's OPTIONAL MATCH degrades to
// null and so only flags a genuine mismatch or a fully missing parent,
// but it never enforces "exactly one parent" — a File wired to two
// Packages that happen to agree on repository_key passes
// RuleInvalidRepositoryOwner outright. doctor storage runs standalone,
// before any doctor graph pass, so its own idea of "healthy" cannot
// depend on that later, more expensive check ever running; every
// cardinality query below is paired with the key check the same row
// would need anyway. package_key and file_key coherence have no
// equivalent at all among VerifyCanonicalIntegrity's six rules, which
// track confidence, provenance, evidence and duplicate keys, not the
// structural spine. doctor graph's rules stay untouched and remain the
// place invariants that need the fully resolved graph belong.
var canonicalCardinalityQueries = []struct {
	name  string
	query string
}{
	{name: "package_container_cardinality", query: "MATCH (package:Package) OPTIONAL MATCH (:Repository)-[edge:CONTAINS_PACKAGE]->(package) WITH package, count(edge) AS links WHERE links <> 1 RETURN count(*)"},
	{name: "package_repository_key", query: "MATCH (repository:Repository)-[:CONTAINS_PACKAGE]->(package:Package) WHERE repository.stable_key <> package.repository_key RETURN count(*)"},
	{name: "file_container_cardinality", query: "MATCH (file:File) OPTIONAL MATCH (:Package)-[edge:CONTAINS_FILE]->(file) WITH file, count(edge) AS links WHERE links <> 1 RETURN count(*)"},
	{name: "file_package_key", query: "MATCH (package:Package)-[:CONTAINS_FILE]->(file:File) WHERE package.stable_key <> file.package_key RETURN count(*)"},
	{name: "file_repository_key", query: "MATCH (repository:Repository)-[:CONTAINS_PACKAGE]->(package:Package)-[:CONTAINS_FILE]->(file:File) WHERE repository.stable_key <> file.repository_key RETURN count(*)"},
	{name: "symbol_definition_cardinality", query: "MATCH (symbol:Symbol) OPTIONAL MATCH (:File)-[edge:DEFINES]->(symbol) WITH symbol, count(edge) AS links WHERE links <> 1 RETURN count(*)"},
	{name: "symbol_file_key", query: "MATCH (file:File)-[:DEFINES]->(symbol:Symbol) WHERE file.stable_key <> symbol.file_key RETURN count(*)"},
	{name: "symbol_package_key", query: "MATCH (package:Package)-[:CONTAINS_FILE]->(file:File)-[:DEFINES]->(symbol:Symbol) WHERE package.stable_key <> symbol.package_key RETURN count(*)"},
	{name: "symbol_repository_key", query: "MATCH (repository:Repository)-[:CONTAINS_PACKAGE]->(package:Package)-[:CONTAINS_FILE]->(file:File)-[:DEFINES]->(symbol:Symbol) WHERE repository.stable_key <> symbol.repository_key RETURN count(*)"},
}

// diagnoseCanonicalCardinality sums canonicalCardinalityQueries the same
// way diagnoseIntegrity sums diagnosticIntegrityQueries: any non-zero
// count anywhere fails the whole check, with every named count in detail.
func diagnoseCanonicalCardinality(connection *lbug.Connection) (string, error) {
	parts := make([]string, 0, len(canonicalCardinalityQueries))
	var violations int64
	for _, item := range canonicalCardinalityQueries {
		count, err := diagnosticCount(connection, item.query)
		if err != nil {
			return "", fmt.Errorf("integrity %s: %w", item.name, err)
		}
		violations += count
		parts = append(parts, fmt.Sprintf("%s=%d", item.name, count))
	}
	if violations != 0 {
		return "", fmt.Errorf("%d integrity violations: %s", violations, strings.Join(parts, " "))
	}
	return "0 violations; " + strings.Join(parts, " "), nil
}

func diagnosticRows(connection *lbug.Connection, query string) ([][]string, error) {
	result, err := connection.Query(query)
	if result != nil {
		defer result.Close()
	}
	if err != nil {
		return nil, err
	}
	columnCount := int(result.GetNumberOfColumns())
	var rows [][]string
	for result.HasNext() {
		tuple, err := nextTuple(result)
		if err != nil {
			return nil, err
		}
		row := make([]string, columnCount)
		for index := range row {
			value, valueErr := tuple.GetValue(uint64(index))
			if valueErr != nil {
				tuple.Close()
				return nil, valueErr
			}
			row[index] = fmt.Sprint(value)
		}
		tuple.Close()
		rows = append(rows, row)
	}
	return rows, nil
}

func diagnosticCount(connection *lbug.Connection, query string) (int64, error) {
	result, err := connection.Query(query)
	if result != nil {
		defer result.Close()
	}
	if err != nil {
		return 0, err
	}
	if !result.HasNext() {
		return 0, errors.New("count query returned no rows")
	}
	tuple, err := nextTuple(result)
	if err != nil {
		return 0, err
	}
	defer tuple.Close()
	return tupleInt64(tuple, 0)
}

func diagnosticExecute(connection *lbug.Connection, query string) error {
	result, err := connection.Query(query)
	if result != nil {
		result.Close()
	}
	return err
}

func versionDetail(diagnosis StorageDiagnosis) string {
	return fmt.Sprintf("engine=%s storage=%d go_binding=%s", diagnosis.EngineVersion, diagnosis.StorageVersion, diagnosis.GoBindingVersion)
}

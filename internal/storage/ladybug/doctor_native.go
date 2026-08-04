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

// DiagnoseStorage validates one existing canonical LadybugDB database.
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

	schemaDetail, schemaErr := diagnoseSchema(connection, &diagnosis)
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

	countsDetail, countsErr := diagnoseCounts(connection, &diagnosis)
	if countsErr != nil {
		diagnosis.addCheck("counts", DiagnosticFail, countsErr.Error())
	} else {
		diagnosis.addCheck("counts", DiagnosticPass, countsDetail)
	}

	integrityDetail, integrityErr := diagnoseIntegrity(connection)
	if integrityErr != nil {
		diagnosis.addCheck("integrity", DiagnosticFail, integrityErr.Error())
	} else {
		diagnosis.addCheck("integrity", DiagnosticPass, integrityDetail)
	}
	diagnosis.finalize()
	return diagnosis, nil
}

func diagnoseSchema(connection *lbug.Connection, diagnosis *StorageDiagnosis) (string, error) {
	rows, err := diagnosticRows(connection, "CALL show_tables() RETURN *")
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if len(row) < 3 {
			return "", fmt.Errorf("show_tables returned %d columns", len(row))
		}
		diagnosis.Tables[row[1]] = row[2]
	}
	var mismatches []string
	for _, required := range requiredDiagnosticTables {
		if actualType, found := diagnosis.Tables[required.name]; !found {
			mismatches = append(mismatches, fmt.Sprintf("missing table %s", required.name))
			continue
		} else if actualType != required.tableType {
			mismatches = append(mismatches, fmt.Sprintf("table %s type=%s want=%s", required.name, actualType, required.tableType))
			continue
		}
		properties, err := diagnosticRows(connection, fmt.Sprintf("CALL table_info('%s') RETURN *", required.name))
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("table_info %s: %v", required.name, err))
			continue
		}
		actualProperties := make(map[string]requiredDiagnosticProperty)
		for _, row := range properties {
			if len(row) < 3 {
				mismatches = append(mismatches, fmt.Sprintf("table_info %s returned %d columns", required.name, len(row)))
				continue
			}
			primary := len(row) >= 5 && strings.EqualFold(row[4], "true")
			actualProperties[row[1]] = requiredDiagnosticProperty{name: row[1], valueType: row[2], primaryKey: primary}
		}
		for _, property := range required.properties {
			actual, found := actualProperties[property.name]
			if !found {
				mismatches = append(mismatches, fmt.Sprintf("table %s missing property %s", required.name, property.name))
			} else if actual.valueType != property.valueType || actual.primaryKey != property.primaryKey {
				mismatches = append(mismatches, fmt.Sprintf("table %s property %s=(%s,pk=%t) want=(%s,pk=%t)", required.name, property.name, actual.valueType, actual.primaryKey, property.valueType, property.primaryKey))
			}
		}
		if required.tableType == "REL" {
			connectionRows, err := diagnosticRows(connection, fmt.Sprintf("CALL show_connection('%s') RETURN *", required.name))
			if err != nil || len(connectionRows) != 1 || len(connectionRows[0]) < 2 {
				mismatches = append(mismatches, fmt.Sprintf("table %s connection metadata is invalid: %v", required.name, err))
			} else if connectionRows[0][0] != required.source || connectionRows[0][1] != required.target {
				mismatches = append(mismatches, fmt.Sprintf("table %s endpoints=%s->%s want=%s->%s", required.name, connectionRows[0][0], connectionRows[0][1], required.source, required.target))
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

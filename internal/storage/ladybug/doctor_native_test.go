//go:build ladybug && cgo

package ladybug

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"
)

func TestDiagnoseStorageValidatesCanonicalDatabaseWithoutChangingIt(t *testing.T) {
	path := newCanonicalDoctorDatabase(t)
	before := testFileHash(t, path)
	diagnosis, err := DiagnoseStorage(context.Background(), path)
	if err != nil {
		t.Fatalf("DiagnoseStorage() error = %v", err)
	}
	if !diagnosis.Healthy {
		t.Fatalf("Healthy = false; checks = %#v", diagnosis.Checks)
	}
	for _, name := range []string{"location", "size", "permissions", "lock", "open", "version", "schema", "transactions", "counts", "integrity"} {
		check := requireDiagnosticCheck(t, diagnosis, name)
		if check.Status != DiagnosticPass {
			t.Fatalf("check %s = %#v, want PASS", name, check)
		}
	}
	if diagnosis.EngineVersion == "" || diagnosis.StorageVersion == 0 || diagnosis.GoBindingVersion != GoBindingVersion {
		t.Fatalf("versions = engine %q storage %d binding %q", diagnosis.EngineVersion, diagnosis.StorageVersion, diagnosis.GoBindingVersion)
	}
	wantCounts := map[string]int64{
		"Repository": 2, "File": 8, "Symbol": 8, "CONTAINS": 8, "DEFINES": 8, "REFERENCES": 4, "CALLS_DIRECT": 3,
	}
	for name, want := range wantCounts {
		if got := diagnosis.Counts[name]; got != want {
			t.Fatalf("count %s = %d, want %d", name, got, want)
		}
	}
	if after := testFileHash(t, path); after != before {
		t.Fatalf("database hash changed: before=%x after=%x", before, after)
	}
}

func TestDiagnoseStorageReportsMissingCanonicalTables(t *testing.T) {
	databaseValue, reader := newQueryFixture(t)
	path := databaseValue.(*database).path
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := databaseValue.Close(); err != nil {
		t.Fatal(err)
	}

	diagnosis, err := DiagnoseStorage(context.Background(), path)
	if err != nil {
		t.Fatalf("DiagnoseStorage() error = %v", err)
	}
	if diagnosis.Healthy {
		t.Fatal("Healthy = true")
	}
	check := requireDiagnosticCheck(t, diagnosis, "schema")
	if check.Status != DiagnosticFail || !strings.Contains(check.Detail, "missing table Repository") {
		t.Fatalf("schema check = %#v", check)
	}
}

func TestDoctorLockHelper(t *testing.T) {
	path := os.Getenv("KIVGRAPH_DOCTOR_LOCK_HELPER")
	if path == "" {
		return
	}
	database, err := Open(context.Background(), path, DefaultConfig())
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	fmt.Println("ready")
	for {
		time.Sleep(time.Hour)
	}
}

func newCanonicalDoctorDatabase(t *testing.T) string {
	t.Helper()
	databaseValue, reader := newQueryFixture(t)
	database := databaseValue.(*database)
	connection, err := lbug.OpenConnection(database.native)
	if err != nil {
		t.Fatal(err)
	}
	mustExecuteQuery(t, connection, "CREATE NODE TABLE Repository(stable_key STRING PRIMARY KEY, name STRING, path STRING, language STRING)")
	mustExecuteQuery(t, connection, "CREATE REL TABLE CONTAINS(FROM Repository TO File, relation_kind STRING)")
	for index := range 2 {
		mustExecuteQuery(t, connection, fmt.Sprintf("CREATE (:Repository {stable_key: 'r%d', name: 'r%d', path: '/r%d', language: 'go'})", index, index, index))
	}
	for index := range 8 {
		repository := 0
		if index >= 6 {
			repository = 1
		}
		mustExecuteQuery(t, connection, fmt.Sprintf("MATCH (repository:Repository {stable_key: 'r%d'}), (file:File {stable_key: 'f%d'}) CREATE (repository)-[:CONTAINS {relation_kind: 'repository_file'}]->(file)", repository, index))
	}
	connection.Close()
	if err := reader.Close(); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	return database.path
}

func requireDiagnosticCheck(t *testing.T, diagnosis StorageDiagnosis, name string) DiagnosticCheck {
	t.Helper()
	check, found := diagnosis.Check(name)
	if !found {
		t.Fatalf("check %q not found in %#v", name, diagnosis.Checks)
	}
	return check
}

func testFileHash(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

func startExternalDoctorLock(t *testing.T, path string) (*exec.Cmd, int) {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestDoctorLockHelper$")
	command.Env = append(os.Environ(), "KIVGRAPH_DOCTOR_LOCK_HELPER="+path)
	stdout, err := command.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	command.Stderr = os.Stderr
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})
	scanner := bufio.NewScanner(stdout)
	if !scanner.Scan() || scanner.Text() != "ready" {
		t.Fatalf("lock helper output = %q, error = %v", scanner.Text(), scanner.Err())
	}
	return command, command.Process.Pid
}

// TestDiagnoseStorageValidatesCanonicalSchemaWithFullCounts builds a real
// canonical database with LoadCanonical from a complete facts.Set and
// checks that DiagnoseStorage reports it healthy, declares the canonical
// schema and its version, and counts every one of the 28 canonical tables
// (CanonicalTableNames), including the ones the fixture leaves empty.
func TestDiagnoseStorageValidatesCanonicalSchemaWithFullCounts(t *testing.T) {
	ctx := context.Background()
	set := canonicalFixtureSet(t)
	options := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "doctor-test-v1"}
	path := filepath.Join(t.TempDir(), "graph.db")
	if _, err := LoadCanonical(ctx, path, set, options); err != nil {
		t.Fatalf("LoadCanonical() error = %v", err)
	}
	expectedRows, err := CanonicalTableRows(set, options)
	if err != nil {
		t.Fatalf("CanonicalTableRows() error = %v", err)
	}
	before := testFileHash(t, path)

	diagnosis, err := DiagnoseStorage(ctx, path)
	if err != nil {
		t.Fatalf("DiagnoseStorage() error = %v", err)
	}
	if !diagnosis.Healthy {
		t.Fatalf("Healthy = false; checks = %#v", diagnosis.Checks)
	}
	if diagnosis.Schema != SchemaCanonical {
		t.Fatalf("Schema = %q, want %q", diagnosis.Schema, SchemaCanonical)
	}
	if diagnosis.SchemaVersion != CanonicalSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", diagnosis.SchemaVersion, CanonicalSchemaVersion)
	}
	for _, name := range []string{"location", "size", "permissions", "lock", "open", "version", "schema", "transactions", "counts", "integrity"} {
		check := requireDiagnosticCheck(t, diagnosis, name)
		if check.Status != DiagnosticPass {
			t.Fatalf("check %s = %#v, want PASS", name, check)
		}
	}
	names := CanonicalTableNames()
	if len(names) != 28 {
		t.Fatalf("CanonicalTableNames() = %d entries, want 28", len(names))
	}
	if len(diagnosis.Counts) != len(names) {
		t.Fatalf("Counts has %d entries, want %d: %#v", len(diagnosis.Counts), len(names), diagnosis.Counts)
	}
	for _, name := range names {
		want := int64(len(expectedRows[name]))
		if got := diagnosis.Counts[name]; got != want {
			t.Fatalf("Counts[%s] = %d, want %d", name, got, want)
		}
	}
	if after := testFileHash(t, path); after != before {
		t.Fatalf("database hash changed: before=%x after=%x", before, after)
	}
}

// TestDiagnoseStorageDeclaresSyntheticSchemaForLegacyDatabase covers the
// other branch of requirement 2: a database with no GraphMetadata table
// (the fixture every other test in this file already builds) is declared
// SchemaSynthetic, never left unset and never misread as canonical.
func TestDiagnoseStorageDeclaresSyntheticSchemaForLegacyDatabase(t *testing.T) {
	path := newCanonicalDoctorDatabase(t)
	diagnosis, err := DiagnoseStorage(context.Background(), path)
	if err != nil {
		t.Fatalf("DiagnoseStorage() error = %v", err)
	}
	if diagnosis.Schema != SchemaSynthetic {
		t.Fatalf("Schema = %q, want %q", diagnosis.Schema, SchemaSynthetic)
	}
	if diagnosis.SchemaVersion != 0 {
		t.Fatalf("SchemaVersion = %d, want 0 for a synthetic database", diagnosis.SchemaVersion)
	}
}

// TestDiagnoseStorageReportsMissingCanonicalTable builds every canonical
// table except one relationship and checks that the schema check fails
// naming it, exactly as TestDiagnoseStorageReportsMissingCanonicalTables
// above already proves for the synthetic schema.
func TestDiagnoseStorageReportsMissingCanonicalTable(t *testing.T) {
	const omitted = "OVERRIDES"
	path := newCanonicalSchemaDatabase(t, omitted, nil)

	diagnosis, err := DiagnoseStorage(context.Background(), path)
	if err != nil {
		t.Fatalf("DiagnoseStorage() error = %v", err)
	}
	if diagnosis.Healthy {
		t.Fatal("Healthy = true")
	}
	if diagnosis.Schema != SchemaCanonical {
		t.Fatalf("Schema = %q, want %q", diagnosis.Schema, SchemaCanonical)
	}
	check := requireDiagnosticCheck(t, diagnosis, "schema")
	if check.Status != DiagnosticFail || !strings.Contains(check.Detail, "missing table "+omitted) {
		t.Fatalf("schema check = %#v", check)
	}
}

// TestDiagnoseStorageReportsAlteredCanonicalTableProperty builds the
// canonical schema with Repository missing its commit property and
// checks that the schema check fails naming both the table and the
// property: the derived requirement list must inspect properties, not
// just table names.
func TestDiagnoseStorageReportsAlteredCanonicalTableProperty(t *testing.T) {
	overrides := map[string]string{
		"Repository": "CREATE NODE TABLE Repository(stable_key STRING PRIMARY KEY, name STRING, root_path STRING, branch STRING, dirty BOOL, languages STRING)",
	}
	path := newCanonicalSchemaDatabase(t, "", overrides)

	diagnosis, err := DiagnoseStorage(context.Background(), path)
	if err != nil {
		t.Fatalf("DiagnoseStorage() error = %v", err)
	}
	if diagnosis.Healthy {
		t.Fatal("Healthy = true")
	}
	check := requireDiagnosticCheck(t, diagnosis, "schema")
	if check.Status != DiagnosticFail || !strings.Contains(check.Detail, "table Repository missing property commit") {
		t.Fatalf("schema check = %#v", check)
	}
}

// TestDiagnoseStorageReportsUnexpectedCanonicalSchemaVersion loads a clean
// canonical graph and overwrites GraphMetadata.schema_version with raw
// Cypher, covering acceptance (d): the table shapes all check out, so the
// version comparison itself must be what fails.
func TestDiagnoseStorageReportsUnexpectedCanonicalSchemaVersion(t *testing.T) {
	path := buildCleanCanonicalGraph(t)
	injectRawCypher(t, path, `MATCH (m:GraphMetadata) WHERE m.key = 'schema_version' SET m.value = '999'`)

	diagnosis, err := DiagnoseStorage(context.Background(), path)
	if err != nil {
		t.Fatalf("DiagnoseStorage() error = %v", err)
	}
	if diagnosis.Healthy {
		t.Fatal("Healthy = true")
	}
	if diagnosis.SchemaVersion != 999 {
		t.Fatalf("SchemaVersion = %d, want 999 (the value actually stored)", diagnosis.SchemaVersion)
	}
	check := requireDiagnosticCheck(t, diagnosis, "schema")
	wantSubstring := fmt.Sprintf("schema_version=999 want=%d", CanonicalSchemaVersion)
	if check.Status != DiagnosticFail || !strings.Contains(check.Detail, wantSubstring) {
		t.Fatalf("schema check = %#v, want detail containing %q", check, wantSubstring)
	}
}

// TestDiagnoseStorageReportsCanonicalCardinalityViolation loads a clean
// canonical graph and deletes a Symbol's sole DEFINES edge with raw
// Cypher, leaving it with zero declaring Files instead of exactly one:
// acceptance (e)'s structural cardinality violation.
func TestDiagnoseStorageReportsCanonicalCardinalityViolation(t *testing.T) {
	path := buildCleanCanonicalGraph(t)
	injectRawCypher(t, path, fmt.Sprintf(
		`MATCH (:File)-[edge:DEFINES]->(:Symbol {stable_key: '%s'}) DELETE edge`,
		fixtureSymbolProcessKey,
	))

	diagnosis, err := DiagnoseStorage(context.Background(), path)
	if err != nil {
		t.Fatalf("DiagnoseStorage() error = %v", err)
	}
	if diagnosis.Healthy {
		t.Fatal("Healthy = true")
	}
	check := requireDiagnosticCheck(t, diagnosis, "integrity")
	if check.Status != DiagnosticFail || !strings.Contains(check.Detail, "symbol_definition_cardinality=1") {
		t.Fatalf("integrity check = %#v", check)
	}
	// The rest of the graph is untouched: schema still passes, isolating
	// the failure to the cardinality check alone.
	if schemaCheck := requireDiagnosticCheck(t, diagnosis, "schema"); schemaCheck.Status != DiagnosticPass {
		t.Fatalf("schema check = %#v, want PASS", schemaCheck)
	}
}

// newCanonicalSchemaDatabase builds a database carrying every canonical
// table (so DiagnoseStorage detects it as canonical) except the one named
// by omit, if non-empty, substituting the statement from overrides keyed
// by table name where present. It never inserts any rows, since the
// schema check only inspects table shapes.
func newCanonicalSchemaDatabase(t *testing.T, omit string, overrides map[string]string) string {
	t.Helper()
	var statements []string
	for _, table := range CanonicalNodeTables() {
		if table.Name == omit {
			continue
		}
		if statement, found := overrides[table.Name]; found {
			statements = append(statements, statement)
			continue
		}
		statements = append(statements, nodeStatement(table))
	}
	for _, table := range CanonicalRelationshipTables() {
		if table.Name == omit {
			continue
		}
		if statement, found := overrides[table.Name]; found {
			statements = append(statements, statement)
			continue
		}
		statements = append(statements, relationshipStatement(table))
	}
	path := filepath.Join(t.TempDir(), "graph.db")
	injectRawCypher(t, path, statements...)
	return path
}

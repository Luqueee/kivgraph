//go:build ladybug && cgo

package ladybug

import (
	"bufio"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
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
	path := os.Getenv("LUQUE_DOCTOR_LOCK_HELPER")
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
	command.Env = append(os.Environ(), "LUQUE_DOCTOR_LOCK_HELPER="+path)
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

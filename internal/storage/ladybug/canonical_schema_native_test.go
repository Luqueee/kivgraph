//go:build ladybug && cgo

package ladybug

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	lbug "github.com/LadybugDB/go-ladybug"
)

// TestCanonicalSchemaLoadsIntoEmptyDatabase proves the generated DDL is what
// LadybugDB really accepts, not just what the metadata claims.
func TestCanonicalSchemaLoadsIntoEmptyDatabase(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "schemas", "ladybug", "004-canonical.cypher"))
	if err != nil {
		t.Fatalf("read canonical schema: %v", err)
	}

	database, err := lbug.OpenDatabase(filepath.Join(t.TempDir(), "canonical.db"), lbug.DefaultSystemConfig())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	connection, err := lbug.OpenConnection(database)
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	defer connection.Close()

	for index, statement := range strings.Split(stripSchemaComments(string(contents)), ";") {
		statement = strings.TrimSpace(statement)
		if statement == "" {
			continue
		}
		result, err := connection.Query(statement)
		if err != nil {
			t.Fatalf("statement %d failed: %v\n%s", index+1, err, statement)
		}
		result.Close()
	}

	// Every declared table must exist after loading the schema.
	for _, name := range CanonicalTableNames() {
		result, err := connection.Query("CALL TABLE_INFO('" + name + "') RETURN *")
		if err != nil {
			t.Fatalf("table %q missing after schema load: %v", name, err)
		}
		result.Close()
	}

	// Reloading is idempotent: IF NOT EXISTS keeps a rebuild safe.
	for _, statement := range CanonicalSchemaStatements() {
		result, err := connection.Query(statement)
		if err != nil {
			t.Fatalf("re-applying %q failed: %v", statement, err)
		}
		result.Close()
	}
}

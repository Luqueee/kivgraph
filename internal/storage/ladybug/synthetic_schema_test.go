//go:build ladybug && cgo

package ladybug

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	lbug "github.com/LadybugDB/go-ladybug"
)

func TestSyntheticSchemaLoadsIntoEmptyDatabase(t *testing.T) {
	contents, err := os.ReadFile(filepath.Join("..", "..", "..", "schemas", "ladybug", "001-synthetic.cypher"))
	if err != nil {
		t.Fatalf("read synthetic schema: %v", err)
	}

	database, err := lbug.OpenDatabase(filepath.Join(t.TempDir(), "schema.db"), lbug.DefaultSystemConfig())
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	defer database.Close()
	connection, err := lbug.OpenConnection(database)
	if err != nil {
		t.Fatalf("open connection: %v", err)
	}
	defer connection.Close()

	statements := strings.Split(stripSchemaComments(string(contents)), ";")
	for index, statement := range statements {
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
}

func stripSchemaComments(contents string) string {
	lines := strings.Split(contents, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n")
}

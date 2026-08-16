//go:build ladybug && cgo

package ladybug

import (
	"context"
	"os"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
)

// A corrupt database is not a database with a wrong schema: that case is
// already covered by the doctor tests. This is the file itself being damaged --
// truncated, overwritten, replaced by something else -- which is what a disk
// failure or an interrupted copy actually produces.

// TestDiagnoseStorageDetectsADamagedDatabaseFile is the detection half of
// LUQUE-1205. The doctor must report the damage rather than crash on it, and it
// must not repair, rewrite or otherwise touch the file it is inspecting.
func TestDiagnoseStorageDetectsADamagedDatabaseFile(t *testing.T) {
	for name, damage := range map[string]func(*testing.T, string){
		"truncated":   truncateDatabase,
		"overwritten": overwriteDatabase,
	} {
		t.Run(name, func(t *testing.T) {
			path := buildCleanCanonicalGraph(t)
			damage(t, path)
			before := testFileHash(t, path)

			diagnosis, err := DiagnoseStorage(context.Background(), path)
			if err != nil {
				t.Fatalf("DiagnoseStorage() error = %v, want a reported diagnosis", err)
			}
			if diagnosis.Healthy {
				t.Fatalf("Healthy = true for a damaged database: %#v", diagnosis.Checks)
			}
			if !hasFailingCheck(diagnosis) {
				t.Fatalf("no check failed for a damaged database: %#v", diagnosis.Checks)
			}
			if after := testFileHash(t, path); after != before {
				t.Fatal("DiagnoseStorage modified the database it was inspecting")
			}
		})
	}
}

// TestCorruptDatabaseRefusesWrites is the blocking half: a damaged database must
// reject every write path with a classified error instead of appending to the
// damage.
func TestCorruptDatabaseRefusesWrites(t *testing.T) {
	set := canonicalFixtureSet(t)
	options := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "corruption-test"}

	t.Run("bulk load", func(t *testing.T) {
		path := buildCleanCanonicalGraph(t)
		overwriteDatabase(t, path)
		before := testFileHash(t, path)

		if _, err := LoadCanonical(context.Background(), path, set, options); err == nil {
			t.Fatal("LoadCanonical() error = nil, want a refused load")
		}
		if after := testFileHash(t, path); after != before {
			t.Fatal("a refused load still wrote to the damaged database")
		}
	})

	t.Run("delta", func(t *testing.T) {
		path := buildCleanCanonicalGraph(t)
		overwriteDatabase(t, path)
		before := testFileHash(t, path)

		delta := facts.Delta{ReplacedFiles: []string{"file:repository:acme/widgets:widgets.go"}}
		if _, err := ApplyCanonicalDelta(context.Background(), path, delta, options); err == nil {
			t.Fatal("ApplyCanonicalDelta() error = nil, want a refused mutation")
		}
		if after := testFileHash(t, path); after != before {
			t.Fatal("a refused delta still wrote to the damaged database")
		}
	})
}

// TestCorruptDatabaseRefusesReads keeps the read side honest too: a damaged
// database must not yield a half graph that downstream code would treat as
// complete.
func TestCorruptDatabaseRefusesReads(t *testing.T) {
	path := buildCleanCanonicalGraph(t)
	overwriteDatabase(t, path)

	graph, err := ScanCanonical(context.Background(), path)
	if err == nil {
		t.Fatalf("ScanCanonical() error = nil, want a refused scan (graph = %d symbols)", len(graph.Symbols))
	}
	if len(graph.Symbols) != 0 || len(graph.Edges) != 0 {
		t.Fatalf("ScanCanonical() returned partial content alongside its error: %#v", graph)
	}
}

// TestHealthyDatabasePassesTheSameChecks is the control: the assertions above
// fail for damage, not for everything.
func TestHealthyDatabasePassesTheSameChecks(t *testing.T) {
	path := buildCleanCanonicalGraph(t)
	diagnosis, err := DiagnoseStorage(context.Background(), path)
	if err != nil {
		t.Fatalf("DiagnoseStorage() error = %v", err)
	}
	if !diagnosis.Healthy || hasFailingCheck(diagnosis) {
		t.Fatalf("healthy database reported unhealthy: %#v", diagnosis.Checks)
	}
	if _, err := ScanCanonical(context.Background(), path); err != nil {
		t.Fatalf("ScanCanonical() on a healthy database error = %v", err)
	}
}

func hasFailingCheck(diagnosis StorageDiagnosis) bool {
	for _, check := range diagnosis.Checks {
		if check.Status == DiagnosticFail {
			return true
		}
	}
	return false
}

// truncateDatabase cuts the file in half: the header survives, the content does
// not, which is what an interrupted copy leaves behind.
func truncateDatabase(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat database: %v", err)
	}
	if err := os.Truncate(path, info.Size()/2); err != nil {
		t.Fatalf("truncate database: %v", err)
	}
}

// overwriteDatabase replaces the file with bytes that are not a database at
// all, the state a partial restore or a wrong file leaves.
func overwriteDatabase(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("this is not a ladybug database\n"), 0o600); err != nil {
		t.Fatalf("overwrite database: %v", err)
	}
}

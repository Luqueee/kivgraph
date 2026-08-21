//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"os"
	"testing"
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
// reject the write path with a classified error instead of appending to the
// damage.
//
// LoadCanonical is the only canonical write there is, and it writes a graph it
// creates itself: it refuses a path that already exists before opening it, so a
// damaged database is never opened for writing and never appended to. That
// existence refusal holds for an intact database too, so on its own it is no
// evidence about damage; the evidence is the engine level refusal, asserted on
// the open every writer goes through, against a control that opens the same
// database while it is still intact. Without that control a broken open would
// pass this test. The file hash proves neither refusal added damage to the
// damage.
func TestCorruptDatabaseRefusesWrites(t *testing.T) {
	set := canonicalFixtureSet(t)
	options := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "corruption-test"}

	path := buildCleanCanonicalGraph(t)
	intact, err := Open(context.Background(), path, DefaultConfig())
	if err != nil {
		t.Fatalf("Open() on an intact database error = %v, want it to open", err)
	}
	if err := intact.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	overwriteDatabase(t, path)
	before := testFileHash(t, path)

	if _, err := LoadCanonical(context.Background(), path, set, options); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("LoadCanonical() error = %v, want the load refused with ErrAlreadyExists", err)
	}
	if database, err := Open(context.Background(), path, DefaultConfig()); err == nil {
		_ = database.Close()
		t.Fatal("Open() for writing succeeded on a damaged database")
	}
	if after := testFileHash(t, path); after != before {
		t.Fatal("a refused write still touched the damaged database")
	}
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

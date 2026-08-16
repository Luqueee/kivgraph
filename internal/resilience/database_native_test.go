//go:build ladybug && cgo

package resilience

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
)

// TestCorruptDatabaseKeepsReadersServedAndIsReportedByDoctor is the LUQUE-1205
// requirement as one story on real storage: the definitive graph is destroyed
// under a running server, and the four things that must happen do.
//
//	detect          DiagnoseStorage reports the damage
//	block writes    the engine refuses to load or mutate the file
//	keep serving    the published snapshot still answers
//	report          the diagnosis names which checks failed
//
// The third is the one that needs a real database to mean anything: the served
// graph is in memory and derived, so destroying its source must not reach it.
func TestCorruptDatabaseKeepsReadersServedAndIsReportedByDoctor(t *testing.T) {
	ctx := context.Background()
	databasePath := filepath.Join(t.TempDir(), "graph.db")
	if _, err := ladybug.LoadCanonical(ctx, databasePath, rebuildFacts(), ladybug.CanonicalLoadOptions{
		SnapshotID: 1, ResolverVersion: "resilience-corruption",
	}); err != nil {
		t.Fatalf("load canonical graph: %v", err)
	}

	store := hotsnapshot.NewSnapshotStore(publishedSnapshot(t))
	session := connectServer(t, store)
	before := querySymbol(t, session)

	healthy, err := ladybug.DiagnoseStorage(ctx, databasePath)
	if err != nil {
		t.Fatalf("DiagnoseStorage() on a healthy database error = %v", err)
	}
	if !healthy.Healthy {
		t.Fatalf("fixture database is already unhealthy: %#v", healthy.Checks)
	}

	if err := os.WriteFile(databasePath, []byte("destroyed\n"), 0o600); err != nil {
		t.Fatalf("corrupt database: %v", err)
	}

	diagnosis, err := ladybug.DiagnoseStorage(ctx, databasePath)
	if err != nil {
		t.Fatalf("DiagnoseStorage() error = %v, want a reported diagnosis", err)
	}
	if diagnosis.Healthy {
		t.Fatalf("Healthy = true for a destroyed database: %#v", diagnosis.Checks)
	}
	failed := make([]string, 0, len(diagnosis.Checks))
	for _, check := range diagnosis.Checks {
		if check.Status == ladybug.DiagnosticFail {
			failed = append(failed, check.Name)
		}
	}
	if len(failed) == 0 {
		t.Fatalf("diagnosis names no failing check: %#v", diagnosis.Checks)
	}

	if _, err := ladybug.LoadCanonical(ctx, databasePath, rebuildFacts(), ladybug.CanonicalLoadOptions{
		SnapshotID: 2, ResolverVersion: "resilience-corruption",
	}); err == nil {
		t.Fatal("LoadCanonical() into a destroyed database succeeded")
	}

	if after := querySymbol(t, session); after != before {
		t.Fatalf("served graph changed when its source database was destroyed:\n%s\n%s", before, after)
	}
}

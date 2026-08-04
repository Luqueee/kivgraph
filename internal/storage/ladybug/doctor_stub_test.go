//go:build !ladybug || !cgo

package ladybug

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestDiagnoseStorageReportsNativeSupportUnavailable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "graph.db")
	if err := os.WriteFile(path, []byte("fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	diagnosis, err := DiagnoseStorage(context.Background(), path)
	if err != nil {
		t.Fatalf("DiagnoseStorage() error = %v", err)
	}
	if diagnosis.Healthy {
		t.Fatal("Healthy = true")
	}
	check, found := diagnosis.Check("open")
	if !found || check.Status != DiagnosticFail || check.Detail != ErrUnavailable.Error() {
		t.Fatalf("open check = %#v, found=%t", check, found)
	}
}

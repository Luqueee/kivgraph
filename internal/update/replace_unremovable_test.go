//go:build unix

package update

import (
	"os"
	"path/filepath"
	"testing"
)

// Once the staged bundle is under the installed name the update has happened.
// An old bundle that cannot be deleted afterwards used to be returned as an
// error: `update` printed a failure, exited 1 and skipped restarting the
// daemon, with the new version already live.
func TestReplaceBundleSucceedsWhenTheOldBundleCannotBeRemoved(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root removes a read-only directory, so there is nothing to refuse")
	}
	parent := t.TempDir()
	current := filepath.Join(parent, "kivgraph")
	staged := filepath.Join(parent, "staged", "kivgraph")
	locked := filepath.Join(current, "locked")
	for _, directory := range []string{locked, staged} {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create %q: %v", directory, err)
		}
	}
	if err := os.WriteFile(filepath.Join(locked, "held"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staged, "marker"), []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A directory that cannot be written cannot lose its entries.
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	backup := current + ".previous"
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(backup, "locked"), 0o700) })

	if err := replaceBundle(current, staged); err != nil {
		t.Fatalf("replaceBundle() error = %v, want success: the new bundle is installed", err)
	}
	if content, err := os.ReadFile(filepath.Join(current, "marker")); err != nil || string(content) != "new" {
		t.Fatalf("installed bundle marker = %q, %v, want the staged bundle in place", content, err)
	}
	// The leftover is what the next update clears before it starts.
	if _, err := os.Stat(filepath.Join(backup, "locked", "held")); err != nil {
		t.Fatalf("the fixture did not refuse removal, so the test proved nothing: %v", err)
	}
}

//go:build linux || darwin

package sourceobservation

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestTreeDigestSkipsNonRegularAnalyzedEntries(t *testing.T) {
	root := testsupport.TempDir(t)
	pipe := filepath.Join(root, "pipe.go")
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skipf("named pipes are unavailable: %v", err)
	}
	if err := os.Symlink(pipe, filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	digest, err := TreeDigest(context.Background(), root)
	if err != nil {
		t.Fatalf("TreeDigest() with named pipe error = %v", err)
	}
	empty, err := TreeDigest(context.Background(), testsupport.TempDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if digest != empty {
		t.Fatalf("TreeDigest() = %q for a named pipe, want skipped entry digest %q", digest, empty)
	}
	if _, err := os.Stat(pipe); err != nil {
		t.Fatalf("named pipe disappeared during digest: %v", err)
	}
}

func TestTreeDigestStillFollowsSymlinkedSources(t *testing.T) {
	root := testsupport.TempDir(t)
	target := filepath.Join(root, "real.go")
	if err := os.WriteFile(target, []byte("package a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := TreeDigest(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link.go")); err != nil {
		t.Fatal(err)
	}
	after, err := TreeDigest(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("TreeDigest() ignored a symlink to a regular source file")
	}
}

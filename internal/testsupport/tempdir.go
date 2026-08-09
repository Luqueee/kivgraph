// Package testsupport holds helpers shared by Ladygraph tests. It is not
// imported by production code.
package testsupport

import (
	"path/filepath"
	"testing"
)

// TempDir returns a per-test temporary directory whose path contains no
// symlink component.
//
// Repository path validation rejects a path with a symlinked ancestor, and
// that policy is deliberate. On macOS the directory returned by t.TempDir()
// lives under /var/folders and /var is a symlink to /private/var, so a test
// that feeds t.TempDir() to the workspace layer exercises the rejection
// instead of the behaviour under test. Resolving the realpath here keeps the
// production policy untouched.
func TempDir(t testing.TB) string {
	t.Helper()
	directory, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temporary directory: %v", err)
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		t.Fatalf("make temporary directory absolute: %v", err)
	}
	return filepath.Clean(absolute)
}

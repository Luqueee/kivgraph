//go:build !unix && !windows

package filelock

import (
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// TestAcquireRefusesWhereThereIsNoLock is the whole of this package on a
// platform with no primitive, and it asserts the error rather than the absence
// of one.
//
// Both answers Acquire can otherwise give are claims about other processes, and
// neither can be made here. Reporting "somebody else holds it" would be the
// worse lie -- it reads as a healthy machine already doing the work -- so it
// refuses, and each caller decides what that means for it.
func TestAcquireRefusesWhereThereIsNoLock(t *testing.T) {
	path := filepath.Join(testsupport.TempDir(t), "unsupported.lock")
	held, acquired, err := Acquire(path)
	if err == nil {
		t.Fatalf("Acquire(%q) = %v, %t, nil: this platform cannot make that claim", path, held, acquired)
	}
	if acquired {
		t.Fatalf("Acquire(%q) reported a lock it cannot take", path)
	}
	// Release stays safe on the value a refused Acquire returns, because the
	// caller defers it before it knows which it got.
	if err := held.Release(); err != nil {
		t.Fatalf("Release() after a refused Acquire = %v", err)
	}
}

package filelock

import (
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// TestAcquireIsExclusiveAndReleasable is the whole contract, and the middle
// assertion is the one that matters: a second caller is *refused* rather than
// made to wait, and refusal is not an error. Both callers read that answer as
// "somebody else is already doing it" and go and do something else -- rebuild
// nothing, or wait for the daemon the other process is installing.
func TestAcquireIsExclusiveAndReleasable(t *testing.T) {
	path := filepath.Join(testsupport.TempDir(t), "exclusive.lock")
	first, acquired, err := Acquire(path)
	if err != nil || !acquired {
		t.Fatalf("first Acquire(%q) = %t, %v", path, acquired, err)
	}
	if _, second, err := Acquire(path); err != nil || second {
		t.Fatalf("second Acquire(%q) = %t, %v, want refused without error", path, second, err)
	}
	if err := first.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	// And releasing gives it back rather than poisoning the path.
	third, acquired, err := Acquire(path)
	if err != nil || !acquired {
		t.Fatalf("Acquire(%q) after release = %t, %v", path, acquired, err)
	}
	if err := third.Release(); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
}

// A release on a lock that was never taken, or taken twice, must not panic:
// the caller defers it and cannot always know which it has.
func TestReleasingNothingIsSafe(t *testing.T) {
	var absent *Lock
	if err := absent.Release(); err != nil {
		t.Fatalf("Release() on a nil lock = %v", err)
	}
	path := filepath.Join(testsupport.TempDir(t), "twice.lock")
	held, acquired, err := Acquire(path)
	if err != nil || !acquired {
		t.Fatalf("Acquire(%q) = %t, %v", path, acquired, err)
	}
	if err := held.Release(); err != nil {
		t.Fatalf("first Release() = %v", err)
	}
	if err := held.Release(); err != nil {
		t.Fatalf("second Release() = %v", err)
	}
}

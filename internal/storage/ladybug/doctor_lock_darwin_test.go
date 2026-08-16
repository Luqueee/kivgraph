//go:build ladybug && cgo && darwin

package ladybug

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestExternalStorageLocksReportsAnIdleDatabase(t *testing.T) {
	path := newCanonicalDoctorDatabase(t)
	pids, supported, err := externalStorageLocks(path)
	if err != nil {
		t.Fatalf("externalStorageLocks() error = %v", err)
	}
	if !supported {
		t.Fatal("supported = false, want lock inspection on darwin")
	}
	if len(pids) != 0 {
		t.Fatalf("pids = %v, want none for a closed database", pids)
	}
}

func TestExternalStorageLocksFailsForAMissingDatabase(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent.db")
	if _, _, err := externalStorageLocks(missing); err == nil {
		t.Fatal("externalStorageLocks() error = nil, want a stat failure")
	}
}

// TestExternalStorageLocksIgnoresTheCallersOwnDatabase keeps the darwin
// implementation aligned with the Linux one: the reported pids are the other
// processes, never this one.
func TestExternalStorageLocksIgnoresTheCallersOwnDatabase(t *testing.T) {
	path := newCanonicalDoctorDatabase(t)
	database, err := Open(context.Background(), path, DefaultConfig())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()

	pids, supported, err := externalStorageLocks(path)
	if err != nil {
		t.Fatalf("externalStorageLocks() error = %v", err)
	}
	if !supported || len(pids) != 0 {
		t.Fatalf("externalStorageLocks() = %v, %t, want no external holder", pids, supported)
	}
}

// TestInspectingLocksDoesNotReleaseThem is the regression that decided the
// implementation. fcntl(F_GETLK) is the natural substitute for /proc/locks on
// macOS, and it is a trap: POSIX drops every record lock a process holds on a
// file when that process closes any descriptor for it, so probing a database
// this process owns unlocks the engine. Measured before the fix: an observer
// process saw F_WRLCK, and F_UNLCK right after a read-only probe.
func TestInspectingLocksDoesNotReleaseThem(t *testing.T) {
	path := newCanonicalDoctorDatabase(t)
	database, err := Open(context.Background(), path, DefaultConfig())
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer database.Close()

	if observed := observedLockType(t, path); observed != unix.F_WRLCK {
		t.Fatalf("lock type before inspection = %d, want F_WRLCK (%d)", observed, unix.F_WRLCK)
	}
	if _, _, err := externalStorageLocks(path); err != nil {
		t.Fatalf("externalStorageLocks() error = %v", err)
	}
	if observed := observedLockType(t, path); observed != unix.F_WRLCK {
		t.Fatalf("lock type after inspection = %d, want F_WRLCK (%d): inspection released the engine lock", observed, unix.F_WRLCK)
	}
}

func TestDiagnoseStorageDetectsExternalLock(t *testing.T) {
	path := newCanonicalDoctorDatabase(t)
	_, pid := startExternalDoctorLock(t, path)
	diagnosis, err := DiagnoseStorage(context.Background(), path)
	if err != nil {
		t.Fatalf("DiagnoseStorage() error = %v", err)
	}
	if diagnosis.Healthy {
		t.Fatal("Healthy = true with an external writer")
	}
	check := requireDiagnosticCheck(t, diagnosis, "lock")
	if check.Status != DiagnosticFail || !strings.Contains(check.Detail, fmt.Sprint(pid)) {
		t.Fatalf("lock check = %#v, want external pid %d", check, pid)
	}
}

const lockObserverEnv = "KIVGRAPH_LOCK_OBSERVER"

// TestLockObserverHelper runs in a second process, where fcntl(F_GETLK) is
// safe because that process holds no lock on the database.
func TestLockObserverHelper(t *testing.T) {
	path := os.Getenv(lockObserverEnv)
	if path == "" {
		return
	}
	file, err := os.Open(path)
	if err != nil {
		fmt.Printf("observed error %v\n", err)
		return
	}
	defer file.Close()
	lock := unix.Flock_t{Type: unix.F_WRLCK}
	if err := unix.FcntlFlock(file.Fd(), unix.F_GETLK, &lock); err != nil {
		fmt.Printf("observed error %v\n", err)
		return
	}
	fmt.Printf("observed type %d\n", lock.Type)
}

func observedLockType(t *testing.T, path string) int16 {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestLockObserverHelper$")
	command.Env = append(os.Environ(), lockObserverEnv+"="+path)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("lock observer: %v: %s", err, output)
	}
	for _, line := range strings.Split(string(output), "\n") {
		var observed int16
		if _, scanErr := fmt.Sscanf(line, "observed type %d", &observed); scanErr == nil {
			return observed
		}
	}
	t.Fatalf("lock observer produced no observation: %s", output)
	return 0
}

//go:build ladybug && cgo && linux

package ladybug

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const duplicateHolderEnv = "KIVGRAPH_DUPLICATE_HOLDER"

// TestDuplicateProcessHolderHelper is the second process. It opens the database
// and parks, so the test can attempt the same open from the process under test.
func TestDuplicateProcessHolderHelper(t *testing.T) {
	path := os.Getenv(duplicateHolderEnv)
	if path == "" {
		return
	}
	database, err := Open(context.Background(), path, DefaultConfig())
	if err != nil {
		os.Stdout.WriteString("HOLDER_FAILED " + err.Error() + "\n")
		os.Exit(1)
	}
	defer database.Close()
	os.Stdout.WriteString("HOLDER_READY\n")
	time.Sleep(30 * time.Second)
}

// TestSecondProcessIsRefusedWithALockedError is the LUQUE-1206 contract. Safe
// was already true: the engine's file lock stops the duplicate before it writes
// anything. Clear was not: the engine reports the same status for a locked
// database and a damaged one, so an operator reading "failed to open database"
// could not tell a second instance from corruption. The open path now consults
// the lock table and says which it is.
func TestSecondProcessIsRefusedWithALockedError(t *testing.T) {
	path := buildCleanCanonicalGraph(t)
	holder := startDuplicateHolder(t, path)

	_, err := Open(context.Background(), path, DefaultConfig())
	if err == nil {
		t.Fatal("Open() succeeded while another process held the database")
	}
	if !errors.Is(err, ErrDatabaseLocked) {
		t.Fatalf("Open() error = %v, want it classified as ErrDatabaseLocked", err)
	}
	if !strings.Contains(err.Error(), "pids") {
		t.Fatalf("Open() error = %v, want it to name the holding pids", err)
	}

	// Safety: the live write path must be refused too, and must not touch the
	// database the other process is using. LoadCanonical is that path, and it
	// only ever writes a graph it creates itself: it refuses an existing
	// database before opening it, so its refusal is ErrAlreadyExists and the
	// holder's file is never opened for writing at all. The lock classification
	// itself is the assertion above, on the open every writer goes through.
	if _, err := LoadCanonical(context.Background(), path, canonicalFixtureSet(t),
		CanonicalLoadOptions{SnapshotID: 2, ResolverVersion: "duplicate-test"}); !errors.Is(err, ErrAlreadyExists) {
		t.Fatalf("LoadCanonical() error = %v, want ErrAlreadyExists", err)
	}

	stopDuplicateHolder(t, holder)

	// Once the first process is gone the database is usable again: the lock is
	// the engine's, not a stale file Kivgraph left behind.
	database, err := Open(context.Background(), path, DefaultConfig())
	if err != nil {
		t.Fatalf("Open() after the holder exited error = %v", err)
	}
	if err := database.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// TestDamagedDatabaseIsNotReportedAsLocked is the control that makes the
// distinction meaningful: corruption must keep its own error.
func TestDamagedDatabaseIsNotReportedAsLocked(t *testing.T) {
	path := buildCleanCanonicalGraph(t)
	overwriteDatabase(t, path)

	_, err := Open(context.Background(), path, DefaultConfig())
	if err == nil {
		t.Fatal("Open() succeeded on a damaged database")
	}
	if errors.Is(err, ErrDatabaseLocked) {
		t.Fatalf("Open() error = %v, want a damaged database not to be reported as locked", err)
	}
}

func startDuplicateHolder(t *testing.T, path string) *exec.Cmd {
	t.Helper()
	command := exec.Command(os.Args[0], "-test.run=^TestDuplicateProcessHolderHelper$", "-test.v")
	command.Env = append(os.Environ(), duplicateHolderEnv+"="+path)
	output, err := command.StdoutPipe()
	if err != nil {
		t.Fatalf("holder stdout: %v", err)
	}
	if err := command.Start(); err != nil {
		t.Fatalf("start holder: %v", err)
	}
	t.Cleanup(func() {
		_ = command.Process.Kill()
		_ = command.Wait()
	})

	ready := make(chan string, 1)
	go func() {
		buffer := make([]byte, 4096)
		collected := ""
		for {
			read, err := output.Read(buffer)
			collected += string(buffer[:read])
			if strings.Contains(collected, "HOLDER_READY") || strings.Contains(collected, "HOLDER_FAILED") {
				ready <- collected
				return
			}
			if err != nil {
				ready <- collected
				return
			}
		}
	}()

	select {
	case line := <-ready:
		if !strings.Contains(line, "HOLDER_READY") {
			t.Fatalf("holder did not take the database: %s", line)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("holder never reported readiness")
	}
	return command
}

func stopDuplicateHolder(t *testing.T, command *exec.Cmd) {
	t.Helper()
	if err := command.Process.Kill(); err != nil {
		t.Fatalf("kill holder: %v", err)
	}
	_, _ = command.Process.Wait()
	// The kernel releases the lock when the process dies, but the release is
	// not instantaneous from this process's point of view.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		pids, supported, err := externalStorageLocks(commandDatabasePath(command))
		if err != nil || !supported || len(pids) == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the holder's lock was never released")
}

func commandDatabasePath(command *exec.Cmd) string {
	for _, entry := range command.Env {
		if value, found := strings.CutPrefix(entry, duplicateHolderEnv+"="); found {
			return value
		}
	}
	return ""
}

package generation

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestNewEnforcesProductionSpacePolicy(t *testing.T) {
	config := DefaultConfig()
	config.ReserveBytes = MinimumReserveBytes - 1
	if _, err := New(t.TempDir(), config); err == nil {
		t.Fatal("New() accepted a reserve below 512 MiB")
	}
	config = DefaultConfig()
	config.MarginBytes = MinimumMarginBytes - 1
	if _, err := New(t.TempDir(), config); err == nil {
		t.Fatal("New() accepted a margin below 1 GiB")
	}
	config = DefaultConfig()
	config.FreePermille = MinimumFreePermille - 1
	if _, err := New(t.TempDir(), config); err == nil {
		t.Fatal("New() accepted less than 15% free space")
	}
}

func TestRequiredCandidateBytesUsesLargerPolicy(t *testing.T) {
	got, err := requiredCandidateBytes(100, 20, 30, 1_000, 150)
	if err != nil {
		t.Fatal(err)
	}
	if got != 250 {
		t.Fatalf("requiredCandidateBytes() = %d, want 250", got)
	}
	got, err = requiredCandidateBytes(10, 10, 10, 10_000, 150)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1_500 {
		t.Fatalf("requiredCandidateBytes() = %d, want 1500", got)
	}
	if _, err := requiredCandidateBytes(math.MaxUint64, 0, 0, 0, 0); err == nil {
		t.Fatal("requiredCandidateBytes() accepted overflow")
	}
}

func TestPublishAndRestoreGenerations(t *testing.T) {
	store := newTestStore(t)
	first := publishTestGeneration(t, store, "000001", "first")
	if first.PreviousID != "" {
		t.Fatalf("PreviousID = %q, want empty", first.PreviousID)
	}
	assertCurrentGeneration(t, store, "000001", "first")
	assertMode(t, store.root, 0o700)
	assertMode(t, store.generations, 0o700)
	assertMode(t, store.reserve, 0o600)

	second := publishTestGeneration(t, store, "000002", "second")
	if second.PreviousID != "000001" {
		t.Fatalf("PreviousID = %q, want 000001", second.PreviousID)
	}
	assertCurrentGeneration(t, store, "000002", "second")
	if _, err := os.Stat(filepath.Join(store.generations, "000001")); err != nil {
		t.Fatalf("previous generation was not retained: %v", err)
	}
	if err := store.Restore(context.Background(), "000001", validateTestGeneration("first")); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	assertCurrentGeneration(t, store, "000001", "first")
}

func TestFailedBuildPreservesCurrentAndReleasesReserve(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "000001", "stable")
	before := testHash(t, filepath.Join(store.generations, "000001", "graph.db"))
	_, err := store.Publish(context.Background(), PublishRequest{
		ID: "000002",
		Build: func(_ context.Context, path string) error {
			if err := writeTestGeneration(path, "corrupt"); err != nil {
				return err
			}
			return syscall.ENOSPC
		},
		Validate: validateTestGeneration("corrupt"),
	})
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("Publish() error = %v, want ENOSPC", err)
	}
	assertCurrentGeneration(t, store, "000001", "stable")
	if after := testHash(t, filepath.Join(store.generations, "000001", "graph.db")); after != before {
		t.Fatalf("active database changed: before=%x after=%x", before, after)
	}
	assertAbsent(t, filepath.Join(store.generations, "000002.tmp"))
	assertAbsent(t, filepath.Join(store.generations, "000002"))
	assertAbsent(t, store.reserve)
	data, readErr := os.ReadFile(store.failure)
	if readErr != nil || !strings.Contains(string(data), "no space left on device") {
		t.Fatalf("failure record = %q, error = %v", data, readErr)
	}
}

func TestPublicationFaultsRollbackCurrent(t *testing.T) {
	tests := []struct {
		name      string
		operation Operation
		match     func(*Store, string) bool
	}{
		{name: "rename generation", operation: OperationRenameGeneration, match: func(_ *Store, _ string) bool { return true }},
		{name: "write current", operation: OperationWriteCurrent, match: func(_ *Store, _ string) bool { return true }},
		{name: "sync current", operation: OperationSyncFile, match: func(store *Store, path string) bool { return path == store.current+".next" }},
		{name: "rename current", operation: OperationRenameCurrent, match: func(_ *Store, _ string) bool { return true }},
		{name: "sync state after current", operation: OperationSyncDirectory, match: func(store *Store, path string) bool { return path == store.root }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store := newTestStore(t)
			publishTestGeneration(t, store, "000001", "stable")
			injected := false
			store.config.FaultInjector = func(operation Operation, path string) error {
				if !injected && operation == test.operation && test.match(store, path) {
					injected = true
					return syscall.ENOSPC
				}
				return nil
			}
			_, err := store.Publish(context.Background(), PublishRequest{
				ID:       "000002",
				Build:    func(_ context.Context, path string) error { return writeTestGeneration(path, "candidate") },
				Validate: validateTestGeneration("candidate"),
			})
			if !errors.Is(err, syscall.ENOSPC) {
				t.Fatalf("Publish() error = %v, want ENOSPC", err)
			}
			if !injected {
				t.Fatal("fault was not injected")
			}
			assertCurrentGeneration(t, store, "000001", "stable")
			assertAbsent(t, filepath.Join(store.generations, "000002.tmp"))
			assertAbsent(t, filepath.Join(store.generations, "000002"))
			assertAbsent(t, store.reserve)
		})
	}
}

func TestValidationFailureNeverPublishesCandidate(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "000001", "stable")
	validationErr := errors.New("golden probe failed")
	_, err := store.Publish(context.Background(), PublishRequest{
		ID:       "000002",
		Build:    func(_ context.Context, path string) error { return writeTestGeneration(path, "candidate") },
		Validate: func(context.Context, Generation) error { return validationErr },
	})
	if !errors.Is(err, validationErr) {
		t.Fatalf("Publish() error = %v", err)
	}
	assertCurrentGeneration(t, store, "000001", "stable")
	assertAbsent(t, filepath.Join(store.generations, "000002.tmp"))
}

func TestPublishRejectsInvalidGenerationAndSymlink(t *testing.T) {
	store := newTestStore(t)
	for _, id := range []string{"", "000000", "1", "../001", "abcdef"} {
		_, err := store.Publish(context.Background(), PublishRequest{ID: id, Build: func(context.Context, string) error { return nil }, Validate: func(context.Context, Generation) error { return nil }})
		if !errors.Is(err, ErrInvalidID) {
			t.Fatalf("Publish(%q) error = %v, want ErrInvalidID", id, err)
		}
	}
	_, err := store.Publish(context.Background(), PublishRequest{
		ID: "000001",
		Build: func(_ context.Context, path string) error {
			if err := os.WriteFile(filepath.Join(path, "graph.db"), []byte("data"), 0o600); err != nil {
				return err
			}
			return os.Symlink("graph.db", filepath.Join(path, "alias"))
		},
		Validate: func(context.Context, Generation) error { return nil },
	})
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("Publish() error = %v, want symlink rejection", err)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()
	config := Config{ReserveBytes: 4 << 10, DatabaseFile: "graph.db"}
	store, err := newStore(t.TempDir(), config, false)
	if err != nil {
		t.Fatal(err)
	}
	return store
}

func publishTestGeneration(t *testing.T, store *Store, id, content string) Publication {
	t.Helper()
	publication, err := store.Publish(context.Background(), PublishRequest{
		ID:       id,
		Build:    func(_ context.Context, path string) error { return writeTestGeneration(path, content) },
		Validate: validateTestGeneration(content),
	})
	if err != nil {
		t.Fatalf("Publish(%s) error = %v", id, err)
	}
	return publication
}

func writeTestGeneration(path, content string) error {
	if err := os.WriteFile(filepath.Join(path, "graph.db"), []byte(content), 0o600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(path, "snapshot.valid"), []byte("valid\n"), 0o600)
}

func validateTestGeneration(content string) ValidateFunc {
	return func(_ context.Context, generation Generation) error {
		data, err := os.ReadFile(generation.DatabasePath)
		if err != nil {
			return err
		}
		if string(data) != content {
			return errors.New("database content mismatch")
		}
		marker, err := os.ReadFile(filepath.Join(generation.Path, "snapshot.valid"))
		if err != nil {
			return err
		}
		if string(marker) != "valid\n" {
			return errors.New("snapshot marker mismatch")
		}
		return nil
	}
}

func assertCurrentGeneration(t *testing.T, store *Store, id, content string) {
	t.Helper()
	generation, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if generation.ID != id {
		t.Fatalf("Current().ID = %q, want %q", generation.ID, id)
	}
	if err := validateTestGeneration(content)(context.Background(), generation); err != nil {
		t.Fatalf("current generation validation: %v", err)
	}
}

func assertAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("path %s exists or stat failed: %v", path, err)
	}
}

func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Fatalf("mode %s = %o, want %o", path, got, want)
	}
}

func testHash(t *testing.T, path string) [sha256.Size]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(data)
}

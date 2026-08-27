package generation

import (
	"context"
	"crypto/sha256"
	"errors"
	"math"
	"os"
	"path/filepath"
	"runtime"
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

func TestBackupTracksPreviousActive(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "000001", "first")
	if _, err := store.Backup(context.Background()); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("Backup() error = %v, want ErrNoBackup after first publish", err)
	}
	publishTestGeneration(t, store, "000002", "second")
	backup, err := store.Backup(context.Background())
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if backup.ID != "000001" {
		t.Fatalf("Backup().ID = %q, want 000001", backup.ID)
	}
}

// Discard leaves a store that reads as empty and rebuilds cleanly. The
// pointers go first, so an interruption can leave a directory nobody points
// at -- recoverable -- and never a pointer naming a directory that is gone.
func TestDiscardRemovesEveryGenerationAndBothPointers(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "000001", "first")
	publishTestGeneration(t, store, "000002", "second")
	publishTestGeneration(t, store, "000003", "third")

	removed, err := store.Discard(context.Background())
	if err != nil {
		t.Fatalf("Discard() error = %v", err)
	}
	if strings.Join(removed, ",") != "000001,000002,000003" {
		t.Fatalf("Discard() = %v, want every generation in order", removed)
	}
	if _, err := store.Current(context.Background()); !errors.Is(err, ErrNoCurrent) {
		t.Fatalf("Current() error = %v, want ErrNoCurrent", err)
	}
	if _, err := store.Backup(context.Background()); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("Backup() error = %v, want ErrNoBackup", err)
	}
	remaining, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(remaining) != 0 {
		t.Fatalf("List() = %#v, want no generation", remaining)
	}
	if _, err := os.Stat(store.reserve); !os.IsNotExist(err) {
		t.Fatalf("space reserve error = %v, want it released", err)
	}

	// The store must be usable immediately: an empty store publishes the
	// first id again.
	nextID, err := store.NextID(context.Background())
	if err != nil {
		t.Fatalf("NextID() error = %v", err)
	}
	if nextID != "000001" {
		t.Fatalf("NextID() = %q, want 000001", nextID)
	}
	publishTestGeneration(t, store, nextID, "again")
	assertCurrentGeneration(t, store, "000001", "again")
}

func TestDiscardAcceptsAStoreThatNeverPublished(t *testing.T) {
	store := newTestStore(t)
	removed, err := store.Discard(context.Background())
	if err != nil {
		t.Fatalf("Discard() error = %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("Discard() = %v, want nothing removed", removed)
	}
}

// Keeping the published generation means keeping exactly one: the BACKUP
// pointer names a generation that is being removed, and a pointer that
// survives its generation is the one state this store refuses to keep.
func TestDiscardExceptKeepsOnlyTheNamedGeneration(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "000001", "first")
	publishTestGeneration(t, store, "000002", "second")
	publishTestGeneration(t, store, "000003", "third")
	if _, err := store.Backup(context.Background()); err != nil {
		t.Fatalf("Backup() error = %v, want a backup before discarding", err)
	}

	removed, err := store.DiscardExcept(context.Background(), "000003")
	if err != nil {
		t.Fatalf("DiscardExcept() error = %v", err)
	}
	if strings.Join(removed, ",") != "000001,000002" {
		t.Fatalf("DiscardExcept() = %v, want every other generation", removed)
	}
	assertCurrentGeneration(t, store, "000003", "third")
	if _, err := store.Backup(context.Background()); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("Backup() error = %v, want ErrNoBackup", err)
	}
	remaining, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(remaining) != 1 || remaining[0].ID != "000003" {
		t.Fatalf("List() = %#v, want only the kept generation", remaining)
	}
}

func TestDiscardExceptRefusesAGenerationItCannotKeep(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "000001", "first")
	if _, err := store.DiscardExcept(context.Background(), "000002"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("DiscardExcept() error = %v, want os.ErrNotExist", err)
	}
	assertCurrentGeneration(t, store, "000001", "first")
	if _, err := store.DiscardExcept(context.Background(), "nope"); err == nil {
		t.Fatal("DiscardExcept() accepted an invalid generation id")
	}
}

func TestRestoreInvertsActiveAndBackupRoles(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "000001", "first")
	publishTestGeneration(t, store, "000002", "second")
	if err := store.Restore(context.Background(), "000001", validateTestGeneration("first")); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}
	assertCurrentGeneration(t, store, "000001", "first")
	backup, err := store.Backup(context.Background())
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if backup.ID != "000002" {
		t.Fatalf("Backup().ID = %q, want 000002 (the generation that was active)", backup.ID)
	}
}

func TestPublishFaultAfterBackupWrittenRevertsBothPointers(t *testing.T) {
	for _, operation := range []Operation{OperationWriteCurrent, OperationRenameCurrent} {
		t.Run(string(operation), func(t *testing.T) {
			store := newTestStore(t)
			publishTestGeneration(t, store, "000001", "first")
			publishTestGeneration(t, store, "000002", "second")
			injected := false
			store.config.FaultInjector = func(op Operation, _ string) error {
				if !injected && op == operation {
					injected = true
					return syscall.ENOSPC
				}
				return nil
			}
			_, err := store.Publish(context.Background(), PublishRequest{
				ID:       "000003",
				Build:    func(_ context.Context, path string) error { return writeTestGeneration(path, "third") },
				Validate: validateTestGeneration("third"),
			})
			if !errors.Is(err, syscall.ENOSPC) {
				t.Fatalf("Publish() error = %v, want ENOSPC", err)
			}
			if !injected {
				t.Fatal("fault was not injected")
			}
			assertCurrentGeneration(t, store, "000002", "second")
			backup, err := store.Backup(context.Background())
			if err != nil {
				t.Fatalf("Backup() error = %v", err)
			}
			if backup.ID != "000001" {
				t.Fatalf("Backup().ID = %q, want 000001 unchanged", backup.ID)
			}
		})
	}
}

func TestRestoreFaultAfterBackupWrittenRevertsBothPointers(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "000001", "first")
	publishTestGeneration(t, store, "000002", "second")
	injected := false
	store.config.FaultInjector = func(operation Operation, _ string) error {
		if !injected && operation == OperationRenameCurrent {
			injected = true
			return syscall.ENOSPC
		}
		return nil
	}
	err := store.Restore(context.Background(), "000001", validateTestGeneration("first"))
	if !errors.Is(err, syscall.ENOSPC) {
		t.Fatalf("Restore() error = %v, want ENOSPC", err)
	}
	if !injected {
		t.Fatal("fault was not injected")
	}
	assertCurrentGeneration(t, store, "000002", "second")
	backup, err := store.Backup(context.Background())
	if err != nil {
		t.Fatalf("Backup() error = %v", err)
	}
	if backup.ID != "000001" {
		t.Fatalf("Backup().ID = %q, want 000001 unchanged", backup.ID)
	}
}

func TestBackupInterpretsInconsistentPointerAsAbsent(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "000001", "first")
	publishTestGeneration(t, store, "000002", "second")
	if err := os.WriteFile(store.backup, []byte("000002\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Backup(context.Background()); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("Backup() error = %v, want ErrNoBackup when BACKUP == CURRENT", err)
	}
	if err := os.WriteFile(store.backup, []byte("000009\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Backup(context.Background()); !errors.Is(err, ErrNoBackup) {
		t.Fatalf("Backup() error = %v, want ErrNoBackup when BACKUP targets a missing generation", err)
	}
}

func TestListOrdersAndIgnoresJunk(t *testing.T) {
	store := newTestStore(t)
	empty, err := store.List(context.Background())
	if err != nil || len(empty) != 0 {
		t.Fatalf("List() = %v, %v, want empty, nil", empty, err)
	}
	publishTestGeneration(t, store, "000003", "c")
	publishTestGeneration(t, store, "000001", "a")
	publishTestGeneration(t, store, "000002", "b")
	if err := os.Mkdir(filepath.Join(store.generations, "000099.tmp"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(store.generations, "not-an-id"), 0o700); err != nil {
		t.Fatal(err)
	}
	generations, err := store.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	want := []string{"000001", "000002", "000003"}
	if len(generations) != len(want) {
		t.Fatalf("List() = %d generations, want %d: %v", len(generations), len(want), generations)
	}
	for i, generation := range generations {
		if generation.ID != want[i] {
			t.Fatalf("List()[%d].ID = %q, want %q", i, generation.ID, want[i])
		}
	}
}

func TestPruneKeepsActiveAndBackup(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "000001", "a")
	publishTestGeneration(t, store, "000002", "b")
	publishTestGeneration(t, store, "000003", "c")
	candidatePath := filepath.Join(store.generations, "000099.tmp")
	if err := os.Mkdir(candidatePath, 0o700); err != nil {
		t.Fatal(err)
	}
	removed, err := store.Prune(context.Background())
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if len(removed) != 1 || removed[0] != "000001" {
		t.Fatalf("Prune() removed = %v, want [000001]", removed)
	}
	assertAbsent(t, filepath.Join(store.generations, "000001"))
	if _, err := os.Stat(filepath.Join(store.generations, "000002")); err != nil {
		t.Fatalf("backup generation was pruned: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.generations, "000003")); err != nil {
		t.Fatalf("active generation was pruned: %v", err)
	}
	if _, err := os.Stat(candidatePath); err != nil {
		t.Fatalf("in-progress candidate was pruned: %v", err)
	}
}

func TestPruneWithoutCurrentDeletesNothing(t *testing.T) {
	store := newTestStore(t)
	if err := os.MkdirAll(filepath.Join(store.generations, "000001"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Prune(context.Background()); !errors.Is(err, ErrNoCurrent) {
		t.Fatalf("Prune() error = %v, want ErrNoCurrent", err)
	}
	if _, err := os.Stat(filepath.Join(store.generations, "000001")); err != nil {
		t.Fatalf("Prune() deleted a generation despite no CURRENT: %v", err)
	}
}

func TestNextIDAdvancesAndSkipsOccupied(t *testing.T) {
	store := newTestStore(t)
	id, err := store.NextID(context.Background())
	if err != nil {
		t.Fatalf("NextID() error = %v", err)
	}
	if id != "000001" {
		t.Fatalf("NextID() = %q, want 000001 with no CURRENT", id)
	}
	publishTestGeneration(t, store, "000001", "a")
	id, err = store.NextID(context.Background())
	if err != nil {
		t.Fatalf("NextID() error = %v", err)
	}
	if id != "000002" {
		t.Fatalf("NextID() = %q, want 000002", id)
	}
	leftover := filepath.Join(store.generations, "000002")
	if err := os.MkdirAll(leftover, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := writeTestGeneration(leftover, "leftover"); err != nil {
		t.Fatal(err)
	}
	id, err = store.NextID(context.Background())
	if err != nil {
		t.Fatalf("NextID() error = %v", err)
	}
	if id != "000003" {
		t.Fatalf("NextID() = %q, want 000003 since 000002 is occupied", id)
	}
}

func TestNextIDWrapsPastReservedID(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "999999", "last")
	id, err := store.NextID(context.Background())
	if err != nil {
		t.Fatalf("NextID() error = %v", err)
	}
	if id != "000001" {
		t.Fatalf("NextID() = %q, want 000001 after wrapping past 999999", id)
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
	// One claim among several here, so a platform that keeps no mode bits
	// drops this one and keeps the rest rather than skipping the whole test:
	// publishing and restoring a generation is worth checking everywhere, and
	// Go reports 0777 for every directory on Windows regardless of its ACL, so
	// asserting 0700 there asserts what Go says about every directory.
	//
	// Spelled out rather than calling testsupport.ModeBitsHonoured, which says
	// exactly this: internal/testsupport imports this package, so a test here
	// that imported it back would be an import cycle.
	if runtime.GOOS == "windows" {
		return
	}
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

// TestPublishReclaimsACandidateLeftByADeadWriter is the guard against bricking
// the store. A rebuild killed between creating its candidate and renaming it
// -- an OOM, a closed terminal, a lost pipe -- leaves `<id>.tmp` behind, and
// every later attempt derives the same id from the same CURRENT pointer. When
// that collision was an error the store never accepted another generation
// again, and the only way out was deleting the directory by hand.
func TestPublishReclaimsACandidateLeftByADeadWriter(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "000001", "first")

	// What a killed process leaves: the candidate of the generation the next
	// attempt will build, with a partial payload inside it.
	abandoned := filepath.Join(store.generations, "000002.tmp")
	if err := os.MkdirAll(abandoned, 0o700); err != nil {
		t.Fatalf("seed abandoned candidate: %v", err)
	}
	if err := os.WriteFile(filepath.Join(abandoned, "half-written"), []byte("debris"), 0o600); err != nil {
		t.Fatalf("seed debris: %v", err)
	}

	second := publishTestGeneration(t, store, "000002", "second")
	if second.PreviousID != "000001" {
		t.Fatalf("PreviousID = %q, want 000001", second.PreviousID)
	}
	assertCurrentGeneration(t, store, "000002", "second")
	if _, err := os.Stat(filepath.Join(store.generations, "000002.tmp")); !os.IsNotExist(err) {
		t.Errorf("the candidate directory survived publication: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store.generations, "000002", "half-written")); !os.IsNotExist(err) {
		t.Error("the debris of the dead writer was published")
	}
}

// TestPublishRefusesWhileAnotherProcessHoldsTheLock keeps the reclaim above
// from being a licence to overwrite a live rebuild. The store's mutex orders
// only this process, and one state directory is shared by an `index --full`,
// an `index_project` from a client, and a running server's resynchroniser.
func TestPublishRefusesWhileAnotherProcessHoldsTheLock(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "000001", "first")

	held, err := acquirePublishLock(filepath.Join(store.root, "publish.lock"))
	if err != nil {
		t.Fatalf("acquirePublishLock() error = %v", err)
	}

	built := false
	_, err = store.Publish(context.Background(), PublishRequest{
		ID: "000002",
		Build: func(_ context.Context, path string) error {
			built = true
			return writeTestGeneration(path, "second")
		},
		Validate: validateTestGeneration("second"),
	})
	if !errors.Is(err, ErrPublishInProgress) {
		t.Fatalf("Publish() error = %v, want ErrPublishInProgress", err)
	}
	if built {
		t.Error("the candidate was built while another writer held the lock")
	}
	assertCurrentGeneration(t, store, "000001", "first")

	// Releasing it lets the next publication through, so the refusal is the
	// lock and not a latch the store never reopens.
	if err := held.release(); err != nil {
		t.Fatalf("release() error = %v", err)
	}
	publishTestGeneration(t, store, "000002", "second")
	assertCurrentGeneration(t, store, "000002", "second")
}

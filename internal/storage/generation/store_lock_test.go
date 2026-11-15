package generation

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Publish reads CURRENT, builds for minutes and then writes both pointers. An
// operation that moved a pointer or removed a generation in between was undone
// or left a pointer dangling, and reported success. Each of them now loses to
// the lock a publication holds, and changes nothing when it loses.
func TestPointerAndRemovalOperationsRefuseWhileAPublicationHoldsTheLock(t *testing.T) {
	operations := []struct {
		name string
		run  func(store *Store) error
	}{
		{name: "Restore", run: func(store *Store) error {
			return store.Restore(context.Background(), "000001", validateTestGeneration("first"))
		}},
		{name: "Prune", run: func(store *Store) error {
			_, err := store.Prune(context.Background())
			return err
		}},
		{name: "Discard", run: func(store *Store) error {
			_, err := store.Discard(context.Background())
			return err
		}},
		{name: "DiscardExcept", run: func(store *Store) error {
			_, err := store.DiscardExcept(context.Background(), "000003")
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			store := newTestStore(t)
			publishTestGeneration(t, store, "000001", "first")
			publishTestGeneration(t, store, "000002", "second")
			publishTestGeneration(t, store, "000003", "third")

			held, err := acquirePublishLock(filepath.Join(store.root, "publish.lock"))
			if err != nil {
				t.Fatalf("acquirePublishLock() error = %v", err)
			}
			if err := operation.run(store); !errors.Is(err, ErrPublishInProgress) {
				t.Fatalf("%s() error = %v, want ErrPublishInProgress", operation.name, err)
			}
			assertCurrentGeneration(t, store, "000003", "third")
			if got := generationIDs(t, store); !slices.Equal(got, []string{"000001", "000002", "000003"}) {
				t.Fatalf("generations after a refused %s = %v, want all three untouched", operation.name, got)
			}
			backup, err := store.Backup(context.Background())
			if err != nil || backup.ID != "000002" {
				t.Fatalf("backup after a refused %s = %q, %v, want 000002", operation.name, backup.ID, err)
			}

			// The refusal is the lock, not a latch: released, the same call works.
			if err := held.release(); err != nil {
				t.Fatalf("release() error = %v", err)
			}
			if err := operation.run(store); err != nil {
				t.Fatalf("%s() after release error = %v", operation.name, err)
			}
		})
	}
}

// `clean --keep-active` reads the active id and then asks for everything else
// to go. A publication that landed in between must not lose its generation
// with CURRENT still naming it.
func TestDiscardExceptRefusesAnIDThatIsNoLongerActive(t *testing.T) {
	store := newTestStore(t)
	publishTestGeneration(t, store, "000001", "first")
	publishTestGeneration(t, store, "000002", "second")

	_, err := store.DiscardExcept(context.Background(), "000001")
	if err == nil || !strings.Contains(err.Error(), "000001 is no longer active: 000002 is") {
		t.Fatalf("DiscardExcept(stale id) error = %v, want it to name both generations", err)
	}
	assertCurrentGeneration(t, store, "000002", "second")
	if got := generationIDs(t, store); !slices.Equal(got, []string{"000001", "000002"}) {
		t.Fatalf("generations after the refusal = %v, want both untouched", got)
	}
}

func generationIDs(t *testing.T, store *Store) []string {
	t.Helper()
	generations, err := store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(generations))
	for _, generation := range generations {
		ids = append(ids, generation.ID)
	}
	return ids
}

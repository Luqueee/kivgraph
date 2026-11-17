package generation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// A rebuild that dies leaves "<id>.tmp" behind. Publish reclaims the one whose
// id it reuses, but after a clean the numbering starts over and the rest were
// never reused: a partial database stayed on disk while clean said it had
// removed everything.
func TestRemovalOperationsReclaimAbandonedCandidates(t *testing.T) {
	operations := []struct {
		name string
		run  func(store *Store) error
	}{
		{name: "Prune", run: func(store *Store) error {
			_, err := store.Prune(context.Background())
			return err
		}},
		{name: "Discard", run: func(store *Store) error {
			_, err := store.Discard(context.Background())
			return err
		}},
		{name: "DiscardExcept", run: func(store *Store) error {
			_, err := store.DiscardExcept(context.Background(), "000002")
			return err
		}},
	}
	for _, operation := range operations {
		t.Run(operation.name, func(t *testing.T) {
			store := newTestStore(t)
			publishTestGeneration(t, store, "000001", "first")
			publishTestGeneration(t, store, "000002", "second")

			abandoned := filepath.Join(store.generations, "000009.tmp")
			// Named like a candidate and not one: neither is this store's to remove.
			notADirectory := filepath.Join(store.generations, "000008.tmp")
			notAnID := filepath.Join(store.generations, "notes.tmp")
			for _, directory := range []string{abandoned, notAnID} {
				if err := os.MkdirAll(directory, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			for _, file := range []string{filepath.Join(abandoned, "graph.db"), notADirectory} {
				if err := os.WriteFile(file, []byte("partial"), 0o600); err != nil {
					t.Fatal(err)
				}
			}

			// While a publication holds the lock the candidate may be the one
			// it is building, and nothing may touch it.
			held, err := acquirePublishLock(filepath.Join(store.root, "publish.lock"))
			if err != nil {
				t.Fatal(err)
			}
			if err := operation.run(store); !errors.Is(err, ErrPublishInProgress) {
				t.Fatalf("%s() under a held lock error = %v, want ErrPublishInProgress", operation.name, err)
			}
			if _, err := os.Stat(filepath.Join(abandoned, "graph.db")); err != nil {
				t.Fatalf("a candidate was removed while a publication held the lock: %v", err)
			}
			if err := held.release(); err != nil {
				t.Fatal(err)
			}

			if err := operation.run(store); err != nil {
				t.Fatalf("%s() error = %v", operation.name, err)
			}
			if _, err := os.Stat(abandoned); !os.IsNotExist(err) {
				t.Fatalf("the abandoned candidate survived %s: %v", operation.name, err)
			}
			for _, bystander := range []string{notADirectory, notAnID} {
				if _, err := os.Stat(bystander); err != nil {
					t.Fatalf("%s removed %s, which is not a candidate: %v", operation.name, filepath.Base(bystander), err)
				}
			}
		})
	}
}

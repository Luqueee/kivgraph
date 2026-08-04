package hotsnapshot

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestSnapshotStorePublishesAtomicallyAndKeepsOldReadersValid(t *testing.T) {
	initial := publishedSnapshot(t, 1)
	next := publishedSnapshot(t, 2)
	store := NewSnapshotStore(initial)
	old := store.Load()
	if old != initial {
		t.Fatal("Load() did not return initial snapshot")
	}
	if err := store.Publish(next); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if store.Load() != next {
		t.Fatal("Load() did not return published snapshot")
	}
	oldID, found := old.SymbolByStableKey("s-a")
	if old.Metadata().ID != 1 || !found || oldID != 0 {
		t.Fatal("old reader snapshot was invalidated")
	}
}

func TestSnapshotStoreRejectsInvalidGenerations(t *testing.T) {
	store := NewSnapshotStore(nil)
	if store.Load() != nil {
		t.Fatal("empty store returned a snapshot")
	}
	if err := store.Publish(nil); !errors.Is(err, ErrNilSnapshot) {
		t.Fatalf("Publish(nil) error = %v", err)
	}
	first := publishedSnapshot(t, 4)
	if err := store.Publish(first); err != nil {
		t.Fatal(err)
	}
	for _, candidate := range []*GraphSnapshot{publishedSnapshot(t, 4), publishedSnapshot(t, 3)} {
		if err := store.Publish(candidate); !errors.Is(err, ErrSnapshotGeneration) {
			t.Fatalf("Publish(%d) error = %v, want ErrSnapshotGeneration", candidate.Metadata().ID, err)
		}
	}
}

func TestSnapshotStoreClose(t *testing.T) {
	store := NewSnapshotStore(publishedSnapshot(t, 1))
	old := store.Load()
	store.Close()
	store.Close()
	if store.Load() != nil {
		t.Fatal("Load() returned a snapshot after Close")
	}
	if err := store.Publish(publishedSnapshot(t, 2)); !errors.Is(err, ErrSnapshotStoreClosed) {
		t.Fatalf("Publish() after Close error = %v", err)
	}
	if _, found := old.SymbolByStableKey("s-a"); !found {
		t.Fatal("reader holding old snapshot lost access after Close")
	}
}

func TestSnapshotStoreConcurrentReadersAndPublishers(t *testing.T) {
	store := NewSnapshotStore(publishedSnapshot(t, 0))
	const publishers = 32
	const readers = 16
	var writers sync.WaitGroup
	for index := 1; index <= publishers; index++ {
		index := index
		writers.Add(1)
		go func() {
			defer writers.Done()
			candidate := publishedSnapshot(t, uint64(index))
			_ = store.Publish(candidate)
		}()
	}
	var readersGroup sync.WaitGroup
	for range readers {
		readersGroup.Add(1)
		go func() {
			defer readersGroup.Done()
			for range 1_000 {
				snapshot := store.Load()
				if snapshot == nil {
					t.Error("concurrent Load() returned nil")
					return
				}
				if _, found := snapshot.SymbolByStableKey("s-a"); !found {
					t.Error("concurrent reader observed incomplete snapshot")
					return
				}
			}
		}()
	}
	writers.Wait()
	readersGroup.Wait()
	if got := store.Load().Metadata().ID; got != publishers {
		t.Fatalf("final generation = %d, want %d", got, publishers)
	}
}

func publishedSnapshot(t *testing.T, id uint64) *GraphSnapshot {
	t.Helper()
	snapshot, err := BuildGraphSnapshot(builderRows(), id, time.Unix(int64(id+1), 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot(%d) error = %v", id, err)
	}
	return snapshot
}

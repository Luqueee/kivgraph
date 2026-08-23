package hotsnapshot

import (
	"errors"
	"sync"
	"sync/atomic"
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

// TestADeferredStoreDoesNoWorkUntilSomethingNeedsTheGraph is the whole point of
// deferring: a server that is started and never asked anything must not pay for
// the indexes. Availability, the generation's name and the handshake all have to
// be answerable without running the loader.
func TestADeferredStoreDoesNoWorkUntilSomethingNeedsTheGraph(t *testing.T) {
	snapshot := publishedSnapshot(t, 7)
	calls := 0
	store := NewDeferredSnapshotStore(7, func() (*GraphSnapshot, error) {
		calls++
		return snapshot, nil
	})
	if !store.Available() {
		t.Fatal("a deferred store reported no graph: pending work is not missing work")
	}
	if store.GenerationID() != "7" {
		t.Fatalf("GenerationID() = %q, want \"7\"", store.GenerationID())
	}
	if store.LoadFailure() != nil {
		t.Fatalf("LoadFailure() = %v before any load", store.LoadFailure())
	}
	if calls != 0 {
		t.Fatalf("the loader ran %d times before anything asked for the graph", calls)
	}
	if store.Load() != snapshot {
		t.Fatal("Load() did not materialise the deferred snapshot")
	}
	if store.Load() != snapshot || calls != 1 {
		t.Fatalf("the loader ran %d times, want exactly 1", calls)
	}
}

// TestConcurrentReadersMaterialiseOneSetOfIndexes defends the mutex. Without it
// eight sessions arriving together would each build their own copy of the very
// thing this store exists to build once.
func TestConcurrentReadersMaterialiseOneSetOfIndexes(t *testing.T) {
	snapshot := publishedSnapshot(t, 3)
	var calls int64
	release := make(chan struct{})
	store := NewDeferredSnapshotStore(3, func() (*GraphSnapshot, error) {
		atomic.AddInt64(&calls, 1)
		<-release
		return snapshot, nil
	})
	var waiting sync.WaitGroup
	seen := make([]*GraphSnapshot, 8)
	for index := range seen {
		waiting.Add(1)
		go func(at int) {
			defer waiting.Done()
			seen[at] = store.Load()
		}(index)
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	waiting.Wait()
	if got := atomic.LoadInt64(&calls); got != 1 {
		t.Fatalf("the loader ran %d times for 8 concurrent readers, want 1", got)
	}
	for at, got := range seen {
		if got != snapshot {
			t.Fatalf("reader %d saw %v, want the materialised snapshot", at, got)
		}
	}
}

// TestARefusedLoadIsNameableAndNotRetried is the honest half of deferring. The
// failure used to happen at startup, where it killed the process loudly; now it
// happens under a query, so it has to be answerable -- and re-mapping a broken
// generation on every call would turn one fault into a permanent cost.
func TestARefusedLoadIsNameableAndNotRetried(t *testing.T) {
	refusal := errors.New("the generation carries no readable graph")
	calls := 0
	store := NewDeferredSnapshotStore(9, func() (*GraphSnapshot, error) {
		calls++
		return nil, refusal
	})
	if store.Load() != nil {
		t.Fatal("Load() answered a snapshot the loader refused to build")
	}
	if !errors.Is(store.LoadFailure(), refusal) {
		t.Fatalf("LoadFailure() = %v, want the refusal", store.LoadFailure())
	}
	if store.Available() {
		t.Fatal("a store whose load was refused still reported a graph")
	}
	if store.GenerationID() != "" {
		t.Fatalf("GenerationID() = %q after a refusal, want empty", store.GenerationID())
	}
	if store.Load() != nil || calls != 1 {
		t.Fatalf("the loader ran %d times after refusing, want exactly 1", calls)
	}

	// A rebuilt generation is a different question, so it clears the refusal
	// rather than needing a restart.
	recovered := publishedSnapshot(t, 10)
	if err := store.Publish(recovered); err != nil {
		t.Fatalf("Publish() after a refusal error = %v", err)
	}
	if store.LoadFailure() != nil || !store.Available() || store.Load() != recovered {
		t.Fatalf("a published generation did not retire the refusal: failure=%v", store.LoadFailure())
	}
}

// TestClosingADeferredStoreCancelsTheWork covers the shutdown race: a store
// closed before anyone asked must not go and build a graph nobody can read.
func TestClosingADeferredStoreCancelsTheWork(t *testing.T) {
	calls := 0
	store := NewDeferredSnapshotStore(4, func() (*GraphSnapshot, error) {
		calls++
		return publishedSnapshot(t, 4), nil
	})
	store.Close()
	if store.Load() != nil {
		t.Fatal("Load() answered after Close")
	}
	if store.Available() || store.GenerationID() != "" {
		t.Fatal("a closed store still advertised a graph")
	}
	if calls != 0 {
		t.Fatalf("the loader ran %d times on a closed store", calls)
	}
}

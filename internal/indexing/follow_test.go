package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestFollowRejectsAnIncompleteRequest(t *testing.T) {
	store := hotsnapshot.NewSnapshotStore(nil)
	defer store.Close()
	if err := Follow(context.Background(), nil, FollowOptions{Root: testsupport.TempDir(t)}); err == nil {
		t.Fatal("Follow() without a snapshot store must fail")
	}
	if err := Follow(context.Background(), store, FollowOptions{}); err == nil {
		t.Fatal("Follow() without a root must fail")
	}
}

// A store that has never published is not a failure: a server started before
// the first index keeps answering that the index is not ready.
func TestFollowOnceAcceptsAStoreWithNoPublication(t *testing.T) {
	store := hotsnapshot.NewSnapshotStore(nil)
	defer store.Close()
	generations, err := generation.New(testsupport.TempDir(t), generation.DefaultConfig())
	if err != nil {
		t.Fatalf("generation.New() error = %v", err)
	}
	result, err := followOnce(context.Background(), store, generations)
	if err != nil {
		t.Fatalf("followOnce() error = %v", err)
	}
	if result != (followResult{}) {
		t.Fatalf("followOnce() = %+v, want nothing to do", result)
	}
	if store.Load() != nil {
		t.Fatal("followOnce() published a snapshot that does not exist")
	}
}

// The follower asks the store what it is serving, never a counter of its own:
// another publisher installs generations through the same store, and a
// remembered answer would rebuild what is already published.
func TestNeedsPublicationComparesAgainstTheServedGeneration(t *testing.T) {
	for name, test := range map[string]struct {
		servedID uint64
		serving  bool
		activeID uint64
		want     bool
	}{
		"nothing served yet":      {serving: false, activeID: 1, want: true},
		"active is newer":         {servedID: 4, serving: true, activeID: 5, want: true},
		"active is what we serve": {servedID: 5, serving: true, activeID: 5, want: false},
		"another publisher won":   {servedID: 6, serving: true, activeID: 5, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := needsPublicationID(test.servedID, test.serving, test.activeID); got != test.want {
				t.Fatalf("needsPublicationID() = %v, want %v", got, test.want)
			}
		})
	}
}

// TestAReconcileTickDoesNotMapADeferredGeneration is why the comparison takes an
// identifier. This runs every reconciliation interval, so a tick that asked the
// store for the graph would load every idle server after one interval of
// answering nothing -- which is the cost ADR 0067 exists to avoid.
func TestAReconcileTickDoesNotMapADeferredGeneration(t *testing.T) {
	ctx := context.Background()
	generations, err := generation.New(testsupport.TempDir(t), generation.DefaultConfig())
	if err != nil {
		t.Fatalf("generation.New() error = %v", err)
	}
	id, err := generations.NextID(ctx)
	if err != nil {
		t.Fatalf("NextID() error = %v", err)
	}
	if _, err := generations.Publish(ctx, generation.PublishRequest{
		ID:       id,
		Build:    func(_ context.Context, directory string) error { return writeGraphFile(directory) },
		Validate: func(context.Context, generation.Generation) error { return nil },
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	published, err := parseSnapshotID(id)
	if err != nil {
		t.Fatalf("parseSnapshotID(%q) error = %v", id, err)
	}

	loads := 0
	store := hotsnapshot.NewDeferredSnapshotStore(published, func() (*hotsnapshot.GraphSnapshot, error) {
		loads++
		return hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{}, published, time.Unix(1_700_000_000, 0).UTC(), 1)
	})
	defer store.Close()

	result, err := followOnce(ctx, store, generations)
	if err != nil {
		t.Fatalf("followOnce() error = %v", err)
	}
	if result != (followResult{}) {
		t.Fatalf("followOnce() = %+v, want nothing to do: the store already answers from that generation", result)
	}
	if loads != 0 {
		t.Fatalf("a reconcile tick mapped the graph %d times", loads)
	}
}

// writeGraphFile puts the one file a published generation must carry where the
// store expects it, so the fixture is a real publication rather than a
// hand-written directory.
func writeGraphFile(directory string) error {
	return os.WriteFile(filepath.Join(directory, generation.DefaultConfig().DatabaseFile), []byte("graph"), 0o644)
}

package indexing

import (
	"context"
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
func TestNeedsPublicationComparesAgainstTheServedSnapshot(t *testing.T) {
	served := func(t *testing.T, id uint64) *hotsnapshot.GraphSnapshot {
		t.Helper()
		snapshot, err := hotsnapshot.NewGraphSnapshot(hotsnapshot.GraphSnapshotInput{
			ID: id, Version: 1, CreatedAt: time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("NewGraphSnapshot() error = %v", err)
		}
		return snapshot
	}
	for name, test := range map[string]struct {
		served   *hotsnapshot.GraphSnapshot
		activeID uint64
		want     bool
	}{
		"nothing served yet":      {served: nil, activeID: 1, want: true},
		"active is newer":         {served: served(t, 4), activeID: 5, want: true},
		"active is what we serve": {served: served(t, 5), activeID: 5, want: false},
		"another publisher won":   {served: served(t, 6), activeID: 5, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := needsPublication(test.served, test.activeID); got != test.want {
				t.Fatalf("needsPublication() = %v, want %v", got, test.want)
			}
		})
	}
}

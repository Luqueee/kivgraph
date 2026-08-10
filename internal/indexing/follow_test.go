package indexing

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/testsupport"
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
	published, err := followOnce(context.Background(), store, generations)
	if err != nil {
		t.Fatalf("followOnce() error = %v", err)
	}
	if published != 0 {
		t.Fatalf("followOnce() = %d, want no publication", published)
	}
	if store.Load() != nil {
		t.Fatal("followOnce() published a snapshot that does not exist")
	}
}

// The follower compares against the snapshot being served, not against its own
// memory, so a generation another publisher already installed costs no rebuild.
// Reaching the builder here would need a database that does not exist, so a
// rebuild would fail the test rather than pass it silently.
func TestFollowOnceLeavesAnAlreadyServedGenerationAlone(t *testing.T) {
	root := testsupport.TempDir(t)
	generations, err := generation.New(root, generation.DefaultConfig())
	if err != nil {
		t.Fatalf("generation.New() error = %v", err)
	}
	nextID, err := generations.NextID(context.Background())
	if err != nil {
		t.Fatalf("NextID() error = %v", err)
	}
	published, err := generations.Publish(context.Background(), generation.PublishRequest{
		ID: nextID,
		Build: func(_ context.Context, directory string) error {
			// The follower never opens this file; it only has to exist
			// for the store to accept the candidate as complete.
			return os.WriteFile(filepath.Join(directory, "graph.db"), []byte("fixture"), 0o600)
		},
		Validate: func(context.Context, generation.Generation) error { return nil },
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	activeID, err := parseSnapshotID(published.Generation.ID)
	if err != nil {
		t.Fatalf("parseSnapshotID() error = %v", err)
	}

	snapshot, err := hotsnapshot.NewGraphSnapshot(hotsnapshot.GraphSnapshotInput{
		ID: activeID, Version: 1, CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("NewGraphSnapshot() error = %v", err)
	}
	store := hotsnapshot.NewSnapshotStore(snapshot)
	defer store.Close()

	installed, err := followOnce(context.Background(), store, generations)
	if err != nil {
		t.Fatalf("followOnce() error = %v", err)
	}
	if installed != 0 {
		t.Fatalf("followOnce() = %d, want no publication", installed)
	}
	if store.Load() != snapshot {
		t.Fatal("followOnce() replaced the snapshot it was already serving")
	}
}

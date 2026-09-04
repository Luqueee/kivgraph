package indexing

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/freshness"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestContentFreshnessUsesCachedGenerationWithoutFilesystemScan(t *testing.T) {
	repositoryRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(repositoryRoot, "main.go"), []byte("package main\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	repository := workspace.Repository{Name: "test", Path: repositoryRoot, Languages: []string{"go"}}
	storageRoot := t.TempDir()
	digest, err := freshness.Capture(t.Context(), []workspace.Repository{repository})
	if err != nil {
		t.Fatal(err)
	}
	if err := freshness.Save(t.Context(), storageRoot, 42, digest); err != nil {
		t.Fatal(err)
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(
		hotsnapshot.LadybugSnapshotRows{}, 42, time.Unix(1, 0).UTC(), 1,
	)
	if err != nil {
		t.Fatal(err)
	}
	service := NewService(config.Loaded{}, hotsnapshot.NewSnapshotStore(snapshot), "resolver-v1", "")
	service.freshnessCache.Store(freshness.Status{Generation: 42, State: "fresh"})
	if err := os.WriteFile(filepath.Join(repositoryRoot, "main.go"), []byte("package changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := service.ContentFreshness(t.Context())
	if got.Generation != 42 || got.State != "fresh" {
		t.Fatalf("content freshness = %+v, want the cached generation 42", got)
	}
}

func TestContentFreshnessDoesNotMaterializeDeferredSnapshot(t *testing.T) {
	loaded := false
	store := hotsnapshot.NewDeferredSnapshotStore(42, func() (*hotsnapshot.GraphSnapshot, error) {
		loaded = true
		return nil, nil
	})
	service := NewService(config.Loaded{}, store, "resolver-v1", "")
	service.freshnessCache.Store(freshness.Status{Generation: 42, State: "fresh"})

	got := service.ContentFreshness(t.Context())
	if loaded {
		t.Fatal("content freshness materialized the deferred snapshot")
	}
	if got.Generation != 42 || got.State != "fresh" {
		t.Fatalf("content freshness = %+v, want the cached deferred generation 42", got)
	}
}
